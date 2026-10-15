package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type RegisterCashAllocation struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"userId"`
	UserEmail        string     `json:"userEmail"`
	Amount           int        `json:"amount"`
	Currency         string     `json:"currency"`
	Status           string     `json:"status"`
	AssignedBy       int64      `json:"assignedBy"`
	AssignedByEmail  string     `json:"assignedByEmail"`
	UsedSessionID    *int64     `json:"usedSessionId"`
	Note             string     `json:"note"`
	CreatedAt        time.Time  `json:"createdAt"`
	UsedAt           *time.Time `json:"usedAt"`
	FundingType      string     `json:"fundingType"`
	FundingAccountID *int64     `json:"fundingAccountId"`
	ReconciliationID *int64     `json:"reconciliationId"`
}

func scanRegisterCashAllocation(row interface{ Scan(...any) error }, x *RegisterCashAllocation) error {
	return row.Scan(&x.ID, &x.UserID, &x.UserEmail, &x.Amount, &x.Currency, &x.Status, &x.AssignedBy, &x.AssignedByEmail, &x.UsedSessionID, &x.Note, &x.CreatedAt, &x.UsedAt, &x.FundingType, &x.FundingAccountID, &x.ReconciliationID)
}

func (s *Server) registerCashAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		where := `WHERE a.status='pending' AND a.user_id=$1`
		args := []any{principal(r).ID}
		if principal(r).Role == "superadmin" {
			where = `WHERE a.status='pending'`
			args = nil
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT a.id,a.user_id,u.email,a.amount,a.currency,a.status,a.assigned_by,a.assigned_by_email,a.used_session_id,a.note,a.created_at,a.used_at,a.funding_type,a.funding_account_id,a.reconciliation_id FROM register_cash_allocations a JOIN users u ON u.id=a.user_id `+where+` ORDER BY a.id DESC LIMIT 100`, args...)
		if err != nil {
			problem(w, 500, "Could not load opening cash allocations")
			return
		}
		defer rows.Close()
		out := []RegisterCashAllocation{}
		for rows.Next() {
			var x RegisterCashAllocation
			if scanRegisterCashAllocation(rows, &x) != nil {
				problem(w, 500, "Could not read opening cash allocations")
				return
			}
			out = append(out, x)
		}
		if rows.Err() != nil {
			problem(w, 500, "Could not read opening cash allocations")
			return
		}
		respond(w, 200, out)
		return
	}
	var req struct {
		UserID           int64  `json:"userId"`
		Amount           int    `json:"amount"`
		Note             string `json:"note"`
		FundingType      string `json:"fundingType"`
		FundingAccountID *int64 `json:"fundingAccountId"`
		ReconciliationID *int64 `json:"reconciliationId"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	req.FundingType = strings.TrimSpace(req.FundingType)

	if req.FundingType != "new_funds" &&
		req.FundingType != "carry_forward" {
		problem(w, 400, "Choose new funds or carry-forward")
		return
	}
	if req.FundingType == "new_funds" &&
		(req.Amount <= 0 ||
			req.FundingAccountID == nil ||
			*req.FundingAccountID <= 0) {
		problem(w, 400, "New funding requires a positive amount and source account")
		return
	}
	if req.FundingType == "carry_forward" &&
		(req.Amount <= 0 ||
			req.FundingAccountID != nil ||
			req.ReconciliationID == nil ||
			*req.ReconciliationID <= 0) {
		problem(w, 400, "Carry-forward requires a verified reconciliation and positive amount")
		return
	}

	if req.UserID < 1 || req.Amount < 0 || req.Amount > 10000000 || len(req.Note) > 500 {
		problem(w, 400, "Choose a staff member and a valid opening cash amount")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not assign opening cash")
		return
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	var active bool
	var currency string
	if err = tx.QueryRowContext(r.Context(), `SELECT active FROM users WHERE id=$1 FOR SHARE`, req.UserID).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Staff user not found")
		return
	}
	if err != nil || !active {
		problem(w, 409, "Choose an active staff user")
		return
	}
	if tx.QueryRowContext(r.Context(), `SELECT currency FROM store_settings WHERE id=1 FOR SHARE`).Scan(&currency) != nil {
		problem(w, 500, "Could not load store currency")
		return
	}
	var open bool
	if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM register_sessions WHERE opened_by=$1 AND status='open')`, req.UserID).Scan(&open) != nil {
		problem(w, 500, "Could not check register session")
		return
	}
	if open {
		problem(w, 409, "Close this staff member's open register before assigning new opening cash")
		return
	}
	if req.FundingType == "new_funds" {
		var sourceType, sourceCurrency string
		var sourceOwner sql.NullInt64
		var sourceBalance int64

		err = tx.QueryRowContext(r.Context(), `
			SELECT account_type,user_id,currency,balance
			FROM financial_accounts
			WHERE id=$1
			FOR SHARE
		`, *req.FundingAccountID).Scan(
			&sourceType, &sourceOwner,
			&sourceCurrency, &sourceBalance,
		)
		if err != nil {
			problem(w, 400, "Funding source account not found")
			return
		}
		if sourceCurrency != currency ||
			(sourceType != "central_cash" &&
				sourceType != "investment_cash" &&
				sourceType != "bank") ||
			sourceOwner.Valid {
			problem(w, 400, "Invalid opening funding source")
			return
		}
		if sourceBalance < int64(req.Amount) {
			problem(w, 409, "Insufficient source account balance")
			return
		}
	} else {
		var recUserID int64
		var recCurrency, recStatus, sessionStatus string
		err = tx.QueryRowContext(r.Context(), `
			SELECT r.user_id,r.currency,r.status,s.status
			FROM register_cash_reconciliations r
			JOIN register_sessions s ON s.id=r.session_id
			WHERE r.id=$1
			FOR UPDATE OF r
		`, *req.ReconciliationID).Scan(
			&recUserID, &recCurrency, &recStatus, &sessionStatus,
		)
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Verified reconciliation not found")
			return
		}
		if err != nil {
			problem(w, 500, "Could not read reconciliation")
			return
		}
		if recStatus != "verified" ||
			sessionStatus != "closed" ||
			recUserID != req.UserID ||
			recCurrency != currency {
			problem(w, 409, "Reconciliation cannot fund this opening cash")
			return
		}
		available, err := currentCarryForwardAvailable(
			r.Context(), tx, *req.ReconciliationID, nil,
		)
		if err != nil {
			problem(w, 409, "Could not calculate carry-forward cash")
			return
		}
		if available < int64(req.Amount) {
			problem(w, 409, "Carry-forward amount exceeds verified available cash")
			return
		}
	}

	var id int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,
			assigned_by,assigned_by_email,note,
			funding_type,funding_account_id
			,reconciliation_id
		)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id
	`, req.UserID, req.Amount, currency,
		principal(r).ID, principal(r).Email,
		req.Note, req.FundingType,
		req.FundingAccountID, req.ReconciliationID,
	).Scan(&id)
	if err != nil {
		problem(w, 409, "This staff member already has pending opening cash")
		return
	}
	if tx.Commit() != nil {
		problem(w, 500, "Could not assign opening cash")
		return
	}
	respond(w, 201, map[string]any{"id": id})
}

func (s *Server) resetDatabase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Confirmation != "RESET DATABASE" {
		problem(w, 400, "Type RESET DATABASE to confirm")
		return
	}
	var hash string
	if s.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1 AND role='superadmin' AND active`, principal(r).ID).Scan(&hash) != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		problem(w, http.StatusUnauthorized, "Password verification failed")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not reset database")
		return
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `TRUNCATE TABLE
    audit_events,
    cash_shortage_resolutions,
    cash_shortages,
    categories,
    customer_balance_adjustments,
    customer_opening_balances,
    customer_payment_reversals,
    customer_payments,
    customers,
    financial_accounts,
    financial_ledger_entries,
    financial_transfers,
    guest_refreshments,
    inventory_movements,
    login_limits,
    petty_cash_entries,
    product_images,
    products,
    purchase_items,
    purchases,
    refund_items,
    refunds,
    register_cash_allocations,
    register_cash_reconciliations,
    register_sessions,
    request_results,
    sale_items,
    sales
RESTART IDENTITY`); err != nil {
		problem(w, 500, "Could not reset database")
		return
	}
	// Restore the special Owner customer after clearing business data.
	// Its new record has no opening balance, sales, payments or settlements.
	if _, err = tx.ExecContext(r.Context(), `
		INSERT INTO customers (name, notes, is_owner)
		VALUES ('Owner', 'Owner account: cost-price credit checkout', true)
	`); err != nil {
		problem(w, 500, "Could not restore Owner account")
		return
	}

	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id<>$1`, principal(r).ID); err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not reset database")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) ownerControlRoutes(m *http.ServeMux) {
	m.Handle(
		"GET /api/financial/funding-sources",
		requireRoles(s.financialFundingSources, "superadmin"),
	)
	m.Handle(
		"GET /api/register-cash-reconciliations",
		requireRoles(s.registerCashReconciliations, "superadmin"),
	)
	m.Handle(
		"POST /api/register-cash-reconciliations/{id}/review",
		requireRoles(s.reviewRegisterCashReconciliation, "superadmin"),
	)
	m.Handle(
		"GET /api/register-cash-carry-forward-options",
		requireRoles(s.carryForwardOptions, "superadmin"),
	)
	m.Handle("GET /api/register-cash-allocations", requireRoles(s.registerCashAllocations, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/register-cash-allocations", requireRoles(s.registerCashAllocations, "superadmin"))
	m.Handle("POST /api/system/reset", requireRoles(s.resetDatabase, "superadmin"))
}
