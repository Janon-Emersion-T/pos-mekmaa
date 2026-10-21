package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type CashHandover struct {
	ID              int64      `json:"id"`
	CashierID       int64      `json:"cashierId"`
	CashierEmail    string     `json:"cashierEmail"`
	Currency        string     `json:"currency"`
	RequestedAmount int64      `json:"requestedAmount"`
	ApprovedAmount  *int64     `json:"approvedAmount"`
	Status          string     `json:"status"`
	Remarks         string     `json:"remarks"`
	ReviewerEmail   string     `json:"reviewerEmail"`
	DecisionRemarks string     `json:"decisionRemarks"`
	SessionID       *int64     `json:"sessionId"`
	CreatedAt       time.Time  `json:"createdAt"`
	ReviewedAt      *time.Time `json:"reviewedAt"`
}

func scanCashHandover(row interface{ Scan(...any) error }, x *CashHandover) error {
	return row.Scan(&x.ID, &x.CashierID, &x.CashierEmail, &x.Currency,
		&x.RequestedAmount, &x.ApprovedAmount, &x.Status, &x.Remarks,
		&x.ReviewerEmail, &x.DecisionRemarks, &x.SessionID,
		&x.CreatedAt, &x.ReviewedAt)
}

const cashHandoverSelect = `SELECT t.id,t.requested_by,u.email,sa.currency,
	t.requested_amount,t.approved_amount,t.status,t.remarks,
	COALESCE(rv.email,''),t.decision_remarks,t.session_id,t.created_at,t.reviewed_at
	FROM financial_transfers t
	JOIN users u ON u.id=t.requested_by
	JOIN financial_accounts sa ON sa.id=t.source_account_id
	LEFT JOIN users rv ON rv.id=t.reviewed_by `

func (s *Server) cashHandovers(w http.ResponseWriter, r *http.Request) {
	actor := principal(r)
	if r.Method == http.MethodGet {
		rows, err := s.db.QueryContext(r.Context(), cashHandoverSelect+`
			WHERE t.requested_by=$1
              AND EXISTS (
                SELECT 1 FROM financial_accounts src
                WHERE src.id=t.source_account_id
                  AND src.account_type='staff_cash'
              )
              ORDER BY t.created_at DESC LIMIT 200`, actor.ID)
		if err != nil {
			problem(w, 500, "Could not load cash handovers")
			return
		}
		defer rows.Close()
		out := []CashHandover{}
		for rows.Next() {
			var x CashHandover
			if scanCashHandover(rows, &x) != nil {
				problem(w, 500, "Could not read cash handovers")
				return
			}
			out = append(out, x)
		}
		if rows.Err() != nil {
			problem(w, 500, "Could not read cash handovers")
			return
		}

		var balance int64
		var currency string
		err = s.db.QueryRowContext(r.Context(), `
			SELECT a.balance,a.currency FROM financial_accounts a
			JOIN store_settings ss ON ss.currency=a.currency
			WHERE a.account_type='staff_cash' AND a.user_id=$1`, actor.ID).Scan(&balance, &currency)
		if errors.Is(err, sql.ErrNoRows) {
			currency = ""
			balance = 0
		} else if err != nil {
			problem(w, 500, "Could not load accountable cash")
			return
		}
		respond(w, 200, map[string]any{"balance": balance, "currency": currency, "transfers": out})
		return
	}

	var req struct {
		RequestID string `json:"requestId"`
		Amount    int64  `json:"amount"`
		Remarks   string `json:"remarks"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Remarks = strings.TrimSpace(req.Remarks)
	if req.Amount <= 0 || req.Amount > 1000000000 || len(req.Remarks) > 500 || !requestIDPattern.MatchString(req.RequestID) {
		problem(w, 400, "Enter a valid positive amount, optional remarks, and request ID")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not request cash handover")
		return
	}
	defer tx.Rollback()
	replay, hash, err := requestReplay(r.Context(), tx, req.RequestID, "cash-handover", actor.ID, req)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	if replay != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(replay)
		return
	}
	if err = auditActor(r.Context(), tx, actor); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}

	var currency string
	if err = tx.QueryRowContext(r.Context(), `SELECT currency FROM store_settings WHERE id=1 FOR SHARE`).Scan(&currency); err != nil {
		problem(w, 500, "Could not load currency")
		return
	}
	source, err := getOrCreateFinancialAccount(r.Context(), tx, "staff_cash", &actor.ID, currency)
	if err != nil {
		problem(w, 500, "Could not load cashier cash account")
		return
	}
	// Lock the source so concurrent requests cannot over-commit the same cash.
	if err = tx.QueryRowContext(r.Context(), `SELECT balance FROM financial_accounts WHERE id=$1 FOR UPDATE`, source.ID).Scan(&source.Balance); err != nil {
		problem(w, 500, "Could not lock cashier cash account")
		return
	}
	var pending int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(requested_amount),0) FROM financial_transfers WHERE source_account_id=$1 AND status='pending'`, source.ID).Scan(&pending); err != nil {
		problem(w, 500, "Could not validate pending handovers")
		return
	}
	if req.Amount > source.Balance-pending {
		problem(w, 409, "Amount exceeds accountable cash not already requested")
		return
	}

	var superadminID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM users WHERE role='superadmin' AND active=true ORDER BY id LIMIT 1 FOR SHARE`).Scan(&superadminID); err != nil {
		problem(w, 409, "No active superadmin is available to receive cash")
		return
	}
	destination, err := getOrCreateFinancialAccount(r.Context(), tx, "superadmin_cash", &superadminID, currency)
	if err != nil {
		problem(w, 500, "Could not load superadmin cash account")
		return
	}
	var sessionID *int64
	var openSessionID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM register_sessions WHERE opened_by=$1 AND status='open' ORDER BY id DESC LIMIT 1 FOR SHARE`, actor.ID).Scan(&openSessionID); err == nil {
		sessionID = &openSessionID
	} else if !errors.Is(err, sql.ErrNoRows) {
		problem(w, 500, "Could not check register session")
		return
	}
	var id int64
	if err = tx.QueryRowContext(r.Context(), `
		INSERT INTO financial_transfers(source_account_id,destination_account_id,requested_amount,status,remarks,requested_by,session_id,cash_session_id,request_id)
		VALUES($1,$2,$3,'pending',$4,$5,$6,$6,$7) RETURNING id`, source.ID, destination.ID, req.Amount, req.Remarks, actor.ID, sessionID, req.RequestID).Scan(&id); err != nil {
		problem(w, 409, "Cash handover request already exists")
		return
	}
	result := map[string]any{"id": id, "status": "pending"}
	if err = saveRequest(r.Context(), tx, req.RequestID, "cash-handover", hash, actor.ID, result); err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not save cash handover")
		return
	}
	respond(w, http.StatusCreated, result)
}

func (s *Server) pendingCashHandovers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), cashHandoverSelect+`
		WHERE EXISTS (
                SELECT 1 FROM financial_accounts src
                WHERE src.id=t.source_account_id
                  AND src.account_type='staff_cash'
              )
              ORDER BY CASE WHEN t.status='pending' THEN 0 ELSE 1 END,t.created_at DESC LIMIT 300`)
	if err != nil {
		problem(w, 500, "Could not load cash handover requests")
		return
	}
	defer rows.Close()
	out := []CashHandover{}
	for rows.Next() {
		var x CashHandover
		if scanCashHandover(rows, &x) != nil {
			problem(w, 500, "Could not read cash handover requests")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read cash handover requests")
		return
	}
	respond(w, 200, out)
}

func (s *Server) reviewCashHandover(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		Action         string `json:"action"`
		ApprovedAmount int64  `json:"approvedAmount"`
		Remarks        string `json:"remarks"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.Remarks = strings.TrimSpace(req.Remarks)
	if (req.Action != "approve" && req.Action != "reject") || req.ApprovedAmount < 0 || len(req.Remarks) > 500 {
		problem(w, 400, "Choose approve or reject with a valid amount and remarks")
		return
	}
	approve := req.Action == "approve"
	if err := reviewFinancialTransfer(r.Context(), s.db, id, principal(r).ID, req.ApprovedAmount, approve, req.Remarks); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "only active superadmins") {
			status = http.StatusForbidden
		} else if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		problem(w, status, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) cashHandoverRoutes(m *http.ServeMux) {
	m.Handle("GET /api/cash-handovers", requireRoles(s.cashHandovers, "cashier", "admin"))
	m.Handle("POST /api/cash-handovers", requireRoles(s.cashHandovers, "cashier", "admin"))
	m.Handle("GET /api/cash-handovers/review", requireRoles(s.pendingCashHandovers, "superadmin"))
	m.Handle("POST /api/cash-handovers/{id}/review", requireRoles(s.reviewCashHandover, "superadmin"))
}
