package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) deleteSale(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid sale")
		return
	}
	var req struct {
		Confirmation string `json:"confirmation"`
		Reason       string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Confirmation != "DELETE "+strconv.FormatInt(id, 10) || req.Reason == "" || len(req.Reason) > 500 {
		problem(w, 400, "Confirm the sale and provide a reason of up to 500 characters")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not delete sale")
		return
	}
	defer tx.Rollback()
	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	var sessionID int64
	if err = tx.QueryRowContext(r.Context(), `SELECT session_id FROM sales WHERE id=$1`, id).Scan(&sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Sale not found")
		} else {
			problem(w, 500, "Could not load sale")
		}
		return
	}
	if err = tx.QueryRowContext(r.Context(), `SELECT id FROM register_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&sessionID); err != nil {
		problem(w, 500, "Could not load session")
		return
	}
	var deleted sql.NullTime
	err = tx.QueryRowContext(r.Context(), `SELECT deleted_at FROM sales WHERE id=$1 FOR UPDATE`, id).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Sale not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load sale")
		return
	}
	var hasRefunds bool
	if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM refunds WHERE sale_id=$1)`, id).Scan(&hasRefunds) != nil {
		problem(w, 500, "Could not check refunds")
		return
	}
	if hasRefunds {
		problem(w, 409, "Refunded sales cannot be deleted; keep their refund trail")
		return
	}
	var hasPayments bool
	if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM customer_payments WHERE sale_id=$1)`, id).Scan(&hasPayments) != nil {
		problem(w, 500, "Could not check customer payments")
		return
	}
	if hasPayments {
		problem(w, 409, "Sales with customer payments cannot be deleted. Record a return to preserve the payment history")
		return
	}
	if deleted.Valid {
		problem(w, 409, "This sale has already been deleted")
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT product_id,SUM(quantity) FROM sale_items WHERE sale_id=$1 AND product_id IS NOT NULL GROUP BY product_id ORDER BY product_id`, id)
	if err != nil {
		problem(w, 500, "Could not restore stock")
		return
	}
	type item struct{ id, quantity int64 }
	var items []item
	for rows.Next() {
		var x item
		if err = rows.Scan(&x.id, &x.quantity); err != nil {
			rows.Close()
			problem(w, 500, "Could not read sale items")
			return
		}
		items = append(items, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		problem(w, 500, "Could not read sale items")
		return
	}
	for _, x := range items {
		_, err = tx.ExecContext(r.Context(), `UPDATE products SET stock=stock+$1 WHERE id=$2`, x.quantity, x.id)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO inventory_movements(product_id,kind,quantity,reference_type,reference_id,note,created_by) VALUES($1,'adjustment',$2,'sale_deletion',$3,$4,$5)`, x.id, x.quantity, id, "Sale deleted: "+req.Reason, principal(r).ID)
		}
		if err != nil {
			problem(w, 500, "Could not restore stock")
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE sales SET deleted_at=now(),deleted_by=$1,deletion_reason=$2 WHERE id=$3`, principal(r).ID, req.Reason, id)
	if err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not delete sale")
		return
	}
	respond(w, 200, map[string]any{"id": id, "deleted": true})
}
