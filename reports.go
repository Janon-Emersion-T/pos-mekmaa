package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type DailyRow struct {
	CreditSales       int    `json:"creditSales"`
	CashCollections   int    `json:"cashCollections"`
	CardCollections   int    `json:"cardCollections"`
	CreditReturns     int    `json:"creditReturns"`
	Currency          string `json:"currency"`
	SaleCount         int    `json:"saleCount"`
	CashSales         int    `json:"cashSales"`
	CardSales         int    `json:"cardSales"`
	CashRefunds       int    `json:"cashRefunds"`
	CardRefunds       int    `json:"cardRefunds"`
	PettyIn           int    `json:"pettyIn"`
	PettyOut          int    `json:"pettyOut"`
	ClosingDifference int    `json:"closingDifference"`
	ClosedSessions    int    `json:"closedSessions"`
}

func (s *Server) dailyReport(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	if day == "" {
		day = time.Now().In(businessLocation()).Format("2006-01-02")
	}
	from, to, err := dateBounds(day, day)
	if err != nil {
		problem(w, 400, "Invalid report date")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `WITH events AS(
 SELECT currency,'sale_'||payment AS kind,total AS amount FROM sales WHERE created_at>=$1 AND created_at<$2 AND deleted_at IS NULL
 UNION ALL SELECT currency,'refund_'||payment,CASE WHEN payment='credit' THEN total-cash_returned-card_returned ELSE total END FROM refunds WHERE created_at>=$1 AND created_at<$2
 UNION ALL SELECT currency,'refund_cash',cash_returned FROM refunds WHERE payment='credit' AND created_at>=$1 AND created_at<$2
 UNION ALL SELECT currency,'refund_card',card_returned FROM refunds WHERE payment='credit' AND created_at>=$1 AND created_at<$2
 UNION ALL SELECT currency,'collection_'||payment,amount FROM customer_payments WHERE created_at>=$1 AND created_at<$2
 UNION ALL SELECT currency,'petty_'||direction,amount FROM petty_cash_entries WHERE created_at>=$1 AND created_at<$2
 UNION ALL SELECT currency,'close',closing_cash-closing_expected_cash FROM register_sessions WHERE closed_at>=$1 AND closed_at<$2 AND closing_expected_cash IS NOT NULL)
 SELECT currency,count(*) FILTER(WHERE kind LIKE 'sale_%'),COALESCE(sum(amount) FILTER(WHERE kind='sale_cash'),0),COALESCE(sum(amount) FILTER(WHERE kind='sale_card'),0),COALESCE(sum(amount) FILTER(WHERE kind='refund_cash'),0),COALESCE(sum(amount) FILTER(WHERE kind='refund_card'),0),COALESCE(sum(amount) FILTER(WHERE kind='petty_in'),0),COALESCE(sum(amount) FILTER(WHERE kind='petty_out'),0),COALESCE(sum(amount) FILTER(WHERE kind='close'),0),count(*) FILTER(WHERE kind='close'),COALESCE(sum(amount) FILTER(WHERE kind='sale_credit'),0),COALESCE(sum(amount) FILTER(WHERE kind='collection_cash'),0),COALESCE(sum(amount) FILTER(WHERE kind='collection_card'),0),COALESCE(sum(amount) FILTER(WHERE kind='refund_credit'),0) FROM events GROUP BY currency ORDER BY currency`, from, to)
	if err != nil {
		problem(w, 500, "Could not load report")
		return
	}
	defer rows.Close()
	out := []DailyRow{}
	for rows.Next() {
		var x DailyRow
		if rows.Scan(&x.Currency, &x.SaleCount, &x.CashSales, &x.CardSales, &x.CashRefunds, &x.CardRefunds, &x.PettyIn, &x.PettyOut, &x.ClosingDifference, &x.ClosedSessions, &x.CreditSales, &x.CashCollections, &x.CardCollections, &x.CreditReturns) != nil {
			problem(w, 500, "Could not read report")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read report")
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="closing-`+day+`.csv"`)
		c := csv.NewWriter(w)
		defer c.Flush()
		c.Write([]string{"date", "timezone", "currency", "sales", "cash_sales", "card_sales", "cash_refunds", "card_refunds", "net_sales", "petty_in", "petty_out", "net_cash_movement", "closed_registers", "closing_difference", "credit_sales", "cash_collections", "card_collections", "debt_cancelled"})
		for _, x := range out {
			c.Write([]string{day, businessLocation().String(), x.Currency, strconv.Itoa(x.SaleCount), decimalAmount(x.CashSales), decimalAmount(x.CardSales), decimalAmount(x.CashRefunds), decimalAmount(x.CardRefunds), decimalAmount(x.CashSales + x.CardSales + x.CreditSales - x.CashRefunds - x.CardRefunds - x.CreditReturns), decimalAmount(x.PettyIn), decimalAmount(x.PettyOut), decimalAmount(x.CashSales + x.CashCollections - x.CashRefunds + x.PettyIn - x.PettyOut), strconv.Itoa(x.ClosedSessions), decimalAmount(x.ClosingDifference), decimalAmount(x.CreditSales), decimalAmount(x.CashCollections), decimalAmount(x.CardCollections), decimalAmount(x.CreditReturns)})
		}
		return
	}
	respond(w, 200, map[string]any{"date": day, "timezone": businessLocation().String(), "rows": out})
}
func decimalAmount(n int) string { return fmt.Sprintf("%.2f", float64(n)/100) }
func (s *Server) auditHistory(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if r.URL.Query().Get("offset") == "" {
		offset = 0
		err = nil
	}
	if err != nil || offset < 0 {
		problem(w, 400, "Invalid offset")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,created_at,COALESCE(actor_id,0),COALESCE(actor_email,'Not recorded'),entity,entity_id,action,before_data,after_data FROM audit_events WHERE ($1='' OR entity=$1) AND ($2='' OR position(lower($2) in lower(COALESCE(actor_email,'')))>0) ORDER BY id DESC LIMIT 50 OFFSET $3`, r.URL.Query().Get("entity"), r.URL.Query().Get("actor"), offset)
	if err != nil {
		problem(w, 500, "Could not load audit history")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, actor, eid int64
		var at time.Time
		var email, entity, action string
		// INSERT/DELETE events have a NULL before/after snapshot. Scan into
		// []byte so database/sql can represent SQL NULL before encoding JSON.
		var before, after []byte
		if rows.Scan(&id, &at, &actor, &email, &entity, &eid, &action, &before, &after) != nil {
			problem(w, 500, "Could not read audit")
			return
		}
		out = append(out, map[string]any{"id": id, "created": at, "actorId": actor, "actorEmail": email, "entity": entity, "entityId": eid, "action": action, "before": json.RawMessage(before), "after": json.RawMessage(after)})
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read audit")
		return
	}
	respond(w, 200, out)
}
func (s *Server) operationRoutes(m *http.ServeMux) {
	m.Handle("GET /api/sales/{id}", requireRoles(s.receipt, "superadmin", "admin", "cashier"))
	m.Handle("GET /api/sales/{id}/refunds", requireRoles(s.refunds, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/sales/{id}/refunds", requireRoles(s.refundSale, "superadmin", "admin"))
	m.Handle("GET /api/reports/daily", requireRoles(s.dailyReport, "superadmin", "admin"))
	m.Handle("GET /api/audit", requireRoles(s.auditHistory, "superadmin"))
	m.Handle("POST /api/auth/password", requireRoles(s.changePassword, "superadmin", "admin", "cashier"))
	m.Handle("GET /api/products/export", requireRoles(s.exportProducts, "superadmin", "admin"))
	m.Handle("POST /api/products/import", requireRoles(s.importProducts, "superadmin", "admin"))
	m.Handle("POST /api/product-images", requireRoles(s.uploadImage, "superadmin", "admin"))
	m.Handle("GET /api/product-images/{id}", requireRoles(s.productImage, "superadmin", "admin", "cashier"))
}
