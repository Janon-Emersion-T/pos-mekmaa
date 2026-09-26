package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func fmtID(id int64) string { return strconv.FormatInt(id, 10) }

type ReceiptLine struct {
	ID        int64  `json:"id"`
	ProductID *int64 `json:"productId"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice int    `json:"unitPrice"`
	Refunded  int    `json:"refunded"`
}

const saleColumns = `SELECT s.id,s.created_at,s.total,s.payment,COALESCE((SELECT string_agg(quantity||' × '||product_name,', ' ORDER BY id) FROM sale_items WHERE sale_id=s.id),''),s.session_id,COALESCE(s.created_by,0),COALESCE(s.cashier_email,'Not recorded'),s.currency,s.currency_inferred,s.cash_received,s.change_due,COALESCE((SELECT SUM(total) FROM refunds WHERE sale_id=s.id),0),s.deleted_at,COALESCE(s.deletion_reason,''),s.customer_id,s.customer_name,CASE WHEN s.payment='credit' THEN COALESCE((SELECT paid_amount FROM credit_sale_balances WHERE id=s.id),0) ELSE s.total-COALESCE((SELECT SUM(total) FROM refunds WHERE sale_id=s.id),0) END,COALESCE((SELECT outstanding FROM credit_sale_balances WHERE id=s.id),0) FROM sales s `

func scanSale(row interface{ Scan(...any) error }, x *Sale) error {
	return row.Scan(&x.ID, &x.Created, &x.Total, &x.Payment, &x.Items, &x.SessionID, &x.CreatedBy, &x.CashierEmail, &x.Currency, &x.CurrencyInferred, &x.CashReceived, &x.ChangeDue, &x.Refunded, &x.DeletedAt, &x.DeletionReason, &x.CustomerID, &x.CustomerName, &x.PaidAmount, &x.Outstanding)
}
func businessLocation() *time.Location {
	name := os.Getenv("BUSINESS_TIMEZONE")
	if name == "" {
		name = "Asia/Colombo"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
func dateBounds(from, to string) (time.Time, time.Time, error) {
	var start, end time.Time
	var err error
	if from != "" {
		start, err = time.ParseInLocation("2006-01-02", from, businessLocation())
		if err != nil {
			return start, end, err
		}
	}
	if to != "" {
		end, err = time.ParseInLocation("2006-01-02", to, businessLocation())
		if err != nil {
			return start, end, err
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return start, end, errors.New("invalid date range")
	}
	return start, end, nil
}
func optionalTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func (s *Server) sales(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var sid int64
	var err error
	if q.Get("sessionId") != "" {
		sid, err = strconv.ParseInt(q.Get("sessionId"), 10, 64)
		if err != nil || sid < 1 {
			problem(w, 400, "Invalid session")
			return
		}
	}
	limit, offset := 100, 0
	if q.Get("limit") != "" {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			problem(w, 400, "Invalid limit")
			return
		}
	}
	if q.Get("offset") != "" {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil || offset < 0 {
			problem(w, 400, "Invalid offset")
			return
		}
	}
	from, to, err := dateBounds(q.Get("from"), q.Get("to"))
	if err != nil {
		problem(w, 400, "Enter a valid date range")
		return
	}
	payment, status := q.Get("payment"), q.Get("status")
	if payment != "" && payment != "cash" && payment != "card" && payment != "credit" {
		problem(w, 400, "Invalid payment")
		return
	}
	if status != "" && status != "completed" && status != "refunded" && status != "deleted" {
		problem(w, 400, "Invalid status")
		return
	}
	if status == "deleted" && principal(r).Role != "superadmin" {
		problem(w, 403, "Only superadmins can view deleted sales")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), saleColumns+`WHERE ($1::bigint=0 OR s.session_id=$1)
 AND ($2::timestamptz IS NULL OR s.created_at >= $2) AND ($3::timestamptz IS NULL OR s.created_at < $3)
 AND ($4='' OR s.payment=$4) AND ($5='' OR position(lower($5) in lower(COALESCE(s.cashier_email,'')))>0)
 AND (($6='deleted' AND s.deleted_at IS NOT NULL) OR ($6<>'deleted' AND s.deleted_at IS NULL))
 AND ($6 NOT IN ('completed','refunded') OR ($6='refunded' AND EXISTS(SELECT 1 FROM refunds WHERE sale_id=s.id)) OR ($6='completed' AND NOT EXISTS(SELECT 1 FROM refunds WHERE sale_id=s.id)))
 AND ($7='' OR s.id::text=$7 OR EXISTS(SELECT 1 FROM sale_items WHERE sale_id=s.id AND position(lower($7) in lower(product_name))>0))
 ORDER BY s.id DESC LIMIT $8 OFFSET $9`, sid, optionalTime(from), optionalTime(to), payment, q.Get("cashier"), status, strings.TrimSpace(q.Get("q")), limit, offset)
	if err != nil {
		problem(w, 500, "Could not load sales")
		return
	}
	defer rows.Close()
	out := []Sale{}
	for rows.Next() {
		var x Sale
		if scanSale(rows, &x) != nil {
			problem(w, 500, "Could not read sales")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read sales")
		return
	}
	respond(w, 200, out)
}
func (s *Server) receiptData(ctx context.Context, id int64) (Sale, error) {
	var sale Sale
	err := scanSale(s.db.QueryRowContext(ctx, saleColumns+"WHERE s.id=$1", id), &sale)
	if err != nil {
		return sale, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,i.product_id,i.product_name,i.quantity,i.unit_price,COALESCE((SELECT SUM(quantity) FROM refund_items WHERE sale_item_id=i.id),0) FROM sale_items i WHERE sale_id=$1 ORDER BY i.id`, id)
	if err != nil {
		return sale, err
	}
	defer rows.Close()
	sale.Lines = []ReceiptLine{}
	for rows.Next() {
		var x ReceiptLine
		if err = rows.Scan(&x.ID, &x.ProductID, &x.Name, &x.Quantity, &x.UnitPrice, &x.Refunded); err != nil {
			return sale, err
		}
		sale.Lines = append(sale.Lines, x)
	}
	return sale, rows.Err()
}
func (s *Server) receipt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid sale")
		return
	}
	sale, err := s.receiptData(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Sale not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load receipt")
		return
	}
	if sale.DeletedAt != nil && principal(r).Role != "superadmin" {
		problem(w, 403, "Only superadmins can view deleted sales")
		return
	}
	respond(w, 200, sale)
}
