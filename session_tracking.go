package main

import (
	"context"
	"database/sql"
	"net/http"
)

const sessionColumns = `SELECT r.id,r.opened_at,r.closed_at,r.opening_cash,r.closing_cash,r.status,
r.opening_cash+COALESCE((SELECT SUM(total) FROM sales WHERE session_id=r.id AND payment='cash' AND deleted_at IS NULL),0)+COALESCE((SELECT SUM(CASE WHEN direction='in' THEN amount ELSE -amount END) FROM petty_cash_entries WHERE session_id=r.id),0)+COALESCE((SELECT SUM(amount) FROM customer_payments WHERE session_id=r.id AND payment='cash'),0)-COALESCE((SELECT SUM(CASE WHEN payment='cash' THEN total ELSE cash_returned END) FROM refunds WHERE session_id=r.id),0),
COALESCE(r.opened_by,0),COALESCE(r.opened_by_email,'Not recorded'),COALESCE(r.closed_by,0),COALESCE(r.closed_by_email,'Not recorded'),r.closing_expected_cash,
(SELECT count(*) FROM sales WHERE session_id=r.id AND deleted_at IS NULL),
COALESCE((SELECT SUM(total) FROM sales WHERE session_id=r.id AND deleted_at IS NULL),0),r.currency,COALESCE((SELECT SUM(total) FROM refunds WHERE session_id=r.id),0)
FROM register_sessions r `

func scanSession(row interface{ Scan(...any) error }, x *Session) error {
	return row.Scan(&x.ID, &x.OpenedAt, &x.ClosedAt, &x.OpeningCash, &x.ClosingCash, &x.Status, &x.ExpectedCash,
		&x.OpenedBy, &x.OpenedByEmail, &x.ClosedBy, &x.ClosedByEmail, &x.ClosingExpectedCash, &x.SaleCount, &x.SalesTotal, &x.Currency, &x.RefundTotal)
}

// Hold the register row until commit so checkout, cash movements, and closing
// serialize. Read aggregates in a new statement after acquiring the lock.
func (s *Server) lockedSession(ctx context.Context, tx *sql.Tx) (Session, error) {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM register_sessions WHERE status='open' ORDER BY id DESC LIMIT 1 FOR UPDATE`).Scan(&id); err != nil {
		return Session{}, err
	}
	return s.activeSession(ctx, tx)
}

func (s *Server) sessionHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), sessionColumns+"ORDER BY r.id DESC LIMIT 100")
	if err != nil {
		problem(w, 500, "Could not load session history")
		return
	}
	defer rows.Close()
	result := []Session{}
	for rows.Next() {
		var x Session
		if err = scanSession(rows, &x); err != nil {
			problem(w, 500, "Could not read session history")
			return
		}
		result = append(result, x)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read session history")
		return
	}
	respond(w, 200, result)
}
