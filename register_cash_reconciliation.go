package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type RegisterCashReconciliation struct {
	ID                    int64      `json:"id"`
	SessionID             int64      `json:"sessionId"`
	UserID                int64      `json:"userId"`
	UserEmail             string     `json:"userEmail"`
	Currency              string     `json:"currency"`
	ExpectedCash          int64      `json:"expectedCash"`
	CountedCash           int64      `json:"countedCash"`
	CashDifference        int64      `json:"cashDifference"`
	ShortageCash          int64      `json:"shortageCash"`
	VerifiedAvailableCash *int64     `json:"verifiedAvailableCash"`
	CurrentAvailableCash  int64      `json:"currentAvailableCash"`
	Status                string     `json:"status"`
	VerifiedBy            *int64     `json:"verifiedBy"`
	VerifiedByEmail       string     `json:"verifiedByEmail"`
	VerifiedAt            *time.Time `json:"verifiedAt"`
	Remarks               string     `json:"remarks"`
	CreatedAt             time.Time  `json:"createdAt"`
	ClosingReason         string     `json:"closingReason"`
}

func scanRegisterCashReconciliation(row interface{ Scan(...any) error }, x *RegisterCashReconciliation) error {
	return row.Scan(
		&x.ID, &x.SessionID, &x.UserID, &x.UserEmail,
		&x.Currency, &x.ExpectedCash, &x.CountedCash,
		&x.CashDifference, &x.ShortageCash,
		&x.VerifiedAvailableCash, &x.CurrentAvailableCash,
		&x.Status, &x.VerifiedBy, &x.VerifiedByEmail,
		&x.VerifiedAt, &x.Remarks, &x.CreatedAt,
		&x.ClosingReason,
	)
}

const reconciliationCurrentAvailableExpr = `GREATEST(
	r.verified_available_cash
	+ COALESCE((
		SELECT SUM(l.amount)
		FROM financial_ledger_entries l
		JOIN financial_accounts fa ON fa.id=l.account_id
		WHERE fa.account_type='staff_cash'
		  AND fa.user_id=r.user_id
		  AND fa.currency=r.currency
		  AND l.id > r.verified_ledger_entry_id
	),0)
	- COALESCE((
		SELECT SUM(a.amount)
		FROM register_cash_allocations a
		WHERE a.reconciliation_id=r.id
		  AND a.status IN ('pending','used')
	),0),
	0
)`

func reconciliationCurrentAvailableSQL(excludeAllocation bool) string {
	allocationFilter := ""
	if excludeAllocation {
		allocationFilter = "AND a.id<>$2"
	}
	return `
		SELECT GREATEST(
			r.verified_available_cash
			+ COALESCE((
				SELECT SUM(l.amount)
				FROM financial_ledger_entries l
				JOIN financial_accounts fa ON fa.id=l.account_id
				WHERE fa.account_type='staff_cash'
				  AND fa.user_id=r.user_id
				  AND fa.currency=r.currency
				  AND l.id > r.verified_ledger_entry_id
			),0)
			- COALESCE((
				SELECT SUM(a.amount)
				FROM register_cash_allocations a
				WHERE a.reconciliation_id=r.id
				  AND a.status IN ('pending','used')
				  ` + allocationFilter + `
			),0),
			0
		)
		FROM register_cash_reconciliations r
		WHERE r.id=$1
		  AND r.status='verified'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM register_cash_reconciliations newer
		      WHERE newer.user_id=r.user_id
		        AND newer.currency=r.currency
		        AND newer.session_id>r.session_id
		  )
	`
}

func currentCarryForwardAvailable(ctx context.Context, tx *sql.Tx, reconciliationID int64, excludeAllocationID *int64) (int64, error) {
	if reconciliationID <= 0 {
		return 0, errors.New("invalid reconciliation")
	}
	var available int64
	if excludeAllocationID != nil {
		err := tx.QueryRowContext(ctx, reconciliationCurrentAvailableSQL(true), reconciliationID, *excludeAllocationID).Scan(&available)
		return available, err
	}
	err := tx.QueryRowContext(ctx, reconciliationCurrentAvailableSQL(false), reconciliationID).Scan(&available)
	return available, err
}

func verifiedPhysicalAvailable(ctx context.Context, tx *sql.Tx, reconciliationID int64) (int64, int64, error) {
	var available, ledgerID int64
	err := tx.QueryRowContext(ctx, `
		SELECT GREATEST(
			r.counted_cash + COALESCE((
				SELECT SUM(l.amount)
				FROM financial_ledger_entries l
				JOIN financial_accounts fa ON fa.id=l.account_id
				WHERE fa.account_type='staff_cash'
				  AND fa.user_id=r.user_id
				  AND fa.currency=r.currency
				  AND l.id > r.closed_ledger_entry_id
			),0),
			0
		),
		COALESCE((
			SELECT MAX(l.id)
			FROM financial_ledger_entries l
			JOIN financial_accounts fa ON fa.id=l.account_id
			WHERE fa.account_type='staff_cash'
			  AND fa.user_id=r.user_id
			  AND fa.currency=r.currency
		),0)
		FROM register_cash_reconciliations r
		WHERE r.id=$1
		  AND r.status='pending'
	`, reconciliationID).Scan(&available, &ledgerID)
	return available, ledgerID, err
}

func (s *Server) registerCashReconciliations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT r.id,r.session_id,r.user_id,u.email,
		       r.currency,r.expected_cash,r.counted_cash,
		       r.cash_difference,r.shortage_cash,
		       r.verified_available_cash,
		       CASE WHEN r.status='verified' THEN `+reconciliationCurrentAvailableExpr+` ELSE 0 END,
		       r.status,r.verified_by,COALESCE(v.email,''),
		       r.verified_at,r.remarks,r.created_at,
		       COALESCE(s.closing_reason,'')
		FROM register_cash_reconciliations r
		JOIN users u ON u.id=r.user_id
		JOIN register_sessions s ON s.id=r.session_id
		LEFT JOIN users v ON v.id=r.verified_by
		ORDER BY r.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		problem(w, 500, "Could not load reconciliations")
		return
	}
	defer rows.Close()
	out := []RegisterCashReconciliation{}
	for rows.Next() {
		var x RegisterCashReconciliation
		if err := scanRegisterCashReconciliation(rows, &x); err != nil {
			problem(w, 500, "Could not read reconciliations")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read reconciliations")
		return
	}
	respond(w, 200, out)
}

func (s *Server) reviewRegisterCashReconciliation(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Remarks string `json:"remarks"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.Remarks = strings.TrimSpace(req.Remarks)
	if (req.Action != "verify" && req.Action != "reject") || len(req.Remarks) > 500 {
		problem(w, 400, "Choose verify or reject with remarks of at most 500 characters")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not review reconciliation")
		return
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	var role string
	var active bool
	if err = tx.QueryRowContext(r.Context(), `SELECT role,active FROM users WHERE id=$1 FOR UPDATE`, principal(r).ID).Scan(&role, &active); err != nil || role != "superadmin" || !active {
		problem(w, http.StatusForbidden, "Only an active superadmin can review reconciliations")
		return
	}
	var sessionID, userID int64
	var currency, status, sessionStatus string
	var expected, counted int64
	err = tx.QueryRowContext(r.Context(), `
		SELECT r.session_id,r.user_id,r.currency,r.expected_cash,r.counted_cash,r.status,s.status
		FROM register_cash_reconciliations r
		JOIN register_sessions s ON s.id=r.session_id
		WHERE r.id=$1
		FOR UPDATE OF r
	`, id).Scan(&sessionID, &userID, &currency, &expected, &counted, &status, &sessionStatus)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Reconciliation not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not read reconciliation")
		return
	}
	if status != "pending" {
		problem(w, 409, "Reconciliation has already been reviewed")
		return
	}
	if sessionStatus != "closed" || expected < 0 || counted < 0 {
		problem(w, 409, "Reconciliation is not linked to a closed register")
		return
	}
	var accountID int64
	if err = tx.QueryRowContext(r.Context(), `
		SELECT id
		FROM financial_accounts
		WHERE account_type='staff_cash'
		  AND user_id=$1
		  AND currency=$2
		FOR SHARE
	`, userID, currency).Scan(&accountID); err != nil {
		problem(w, 409, "Cashier financial account is unavailable")
		return
	}
	if req.Action == "reject" {
		_, err = tx.ExecContext(r.Context(), `
			UPDATE register_cash_reconciliations
			SET status='rejected',
			    verified_by=$1,
			    verified_at=now(),
			    remarks=$2
			WHERE id=$3 AND status='pending'
		`, principal(r).ID, req.Remarks, id)
		if err != nil {
			problem(w, 500, "Could not reject reconciliation")
			return
		}
		if tx.Commit() != nil {
			problem(w, 500, "Could not reject reconciliation")
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
		return
	}
	available, ledgerID, err := verifiedPhysicalAvailable(r.Context(), tx, id)
	if err != nil {
		problem(w, 409, "Could not calculate verified cash availability")
		return
	}
	_, err = tx.ExecContext(r.Context(), `
		UPDATE register_cash_reconciliations
		SET status='verified',
		    verified_available_cash=$1,
		    verified_ledger_entry_id=$2,
		    verified_by=$3,
		    verified_at=now(),
		    remarks=$4
		WHERE id=$5 AND status='pending'
	`, available, ledgerID, principal(r).ID, req.Remarks, id)
	if err != nil {
		problem(w, 500, "Could not verify reconciliation")
		return
	}
	if tx.Commit() != nil {
		problem(w, 500, "Could not verify reconciliation")
		return
	}
	respond(w, 200, map[string]any{"verifiedAvailableCash": available})
}

func (s *Server) carryForwardOptions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT r.id,r.session_id,r.user_id,u.email,
		       r.currency,r.expected_cash,r.counted_cash,
		       r.cash_difference,r.shortage_cash,
		       r.verified_available_cash,
		       `+reconciliationCurrentAvailableExpr+`,
		       r.status,r.verified_by,COALESCE(v.email,''),
		       r.verified_at,r.remarks,r.created_at,
		       COALESCE(s.closing_reason,'')
		FROM register_cash_reconciliations r
		JOIN users u ON u.id=r.user_id
		JOIN register_sessions s ON s.id=r.session_id
		LEFT JOIN users v ON v.id=r.verified_by
		WHERE r.status='verified'
		ORDER BY r.verified_at DESC
		LIMIT 200
	`)
	if err != nil {
		problem(w, 500, "Could not load carry-forward options")
		return
	}
	defer rows.Close()
	out := []RegisterCashReconciliation{}
	for rows.Next() {
		var x RegisterCashReconciliation
		if err := scanRegisterCashReconciliation(rows, &x); err != nil {
			problem(w, 500, "Could not read carry-forward options")
			return
		}
		if x.CurrentAvailableCash > 0 {
			out = append(out, x)
		}
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read carry-forward options")
		return
	}
	respond(w, 200, out)
}
