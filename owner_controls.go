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
	ID              int64      `json:"id"`
	UserID          int64      `json:"userId"`
	UserEmail       string     `json:"userEmail"`
	Amount          int        `json:"amount"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	AssignedBy      int64      `json:"assignedBy"`
	AssignedByEmail string     `json:"assignedByEmail"`
	UsedSessionID   *int64     `json:"usedSessionId"`
	Note            string     `json:"note"`
	CreatedAt       time.Time  `json:"createdAt"`
	UsedAt          *time.Time `json:"usedAt"`
}

func scanRegisterCashAllocation(row interface{ Scan(...any) error }, x *RegisterCashAllocation) error {
	return row.Scan(&x.ID, &x.UserID, &x.UserEmail, &x.Amount, &x.Currency, &x.Status, &x.AssignedBy, &x.AssignedByEmail, &x.UsedSessionID, &x.Note, &x.CreatedAt, &x.UsedAt)
}

func (s *Server) registerCashAllocations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		where := `WHERE a.status='pending' AND a.user_id=$1`
		args := []any{principal(r).ID}
		if principal(r).Role == "superadmin" {
			where = `WHERE a.status='pending'`
			args = nil
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT a.id,a.user_id,u.email,a.amount,a.currency,a.status,a.assigned_by,a.assigned_by_email,a.used_session_id,a.note,a.created_at,a.used_at FROM register_cash_allocations a JOIN users u ON u.id=a.user_id `+where+` ORDER BY a.id DESC LIMIT 100`, args...)
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
		UserID int64  `json:"userId"`
		Amount int    `json:"amount"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Note = strings.TrimSpace(req.Note)
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
	var id int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO register_cash_allocations(user_id,amount,currency,assigned_by,assigned_by_email,note) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, req.UserID, req.Amount, currency, principal(r).ID, principal(r).Email, req.Note).Scan(&id)
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
	if _, err = tx.ExecContext(r.Context(), `TRUNCATE TABLE register_cash_allocations,customer_payment_reversals,customer_balance_adjustments,customer_payments,customer_opening_balances,refund_items,refunds,sale_items,sales,petty_cash_entries,purchase_items,purchases,inventory_movements,products,categories,product_images,customers,request_results,login_limits,audit_events RESTART IDENTITY`); err != nil {
		problem(w, 500, "Could not reset database")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM auth_sessions WHERE user_id<>$1`, principal(r).ID); err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not reset database")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) ownerControlRoutes(m *http.ServeMux) {
	m.Handle("GET /api/register-cash-allocations", requireRoles(s.registerCashAllocations, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/register-cash-allocations", requireRoles(s.registerCashAllocations, "superadmin"))
	m.Handle("POST /api/system/reset", requireRoles(s.resetDatabase, "superadmin"))
}
