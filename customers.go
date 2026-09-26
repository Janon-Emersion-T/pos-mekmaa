package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type CustomerBalance struct {
	Currency    string `json:"currency"`
	Outstanding int64  `json:"outstanding"`
}
type Customer struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Phone    string            `json:"phone"`
	Email    string            `json:"email"`
	Address  string            `json:"address"`
	Notes    string            `json:"notes"`
	Created  time.Time         `json:"created"`
	Balances []CustomerBalance `json:"balances"`
}

const customerColumns = `SELECT id,name,phone,email,address,notes,created_at FROM customers `

func scanCustomer(row interface{ Scan(...any) error }, c *Customer) error {
	c.Balances = []CustomerBalance{}
	return row.Scan(&c.ID, &c.Name, &c.Phone, &c.Email, &c.Address, &c.Notes, &c.Created)
}
func (s *Server) customers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), customerColumns+`ORDER BY lower(name),id`)
	if err != nil {
		problem(w, 500, "Could not load customers")
		return
	}
	out := []Customer{}
	indices := map[int64]int{}
	for rows.Next() {
		var c Customer
		if err = scanCustomer(rows, &c); err != nil {
			break
		}
		indices[c.ID] = len(out)
		out = append(out, c)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		problem(w, 500, "Could not read customers")
		return
	}
	rows, err = s.db.QueryContext(r.Context(), `SELECT customer_id,currency,SUM(outstanding) FROM (SELECT customer_id,currency,outstanding FROM credit_sale_balances WHERE deleted_at IS NULL UNION ALL SELECT customer_id,currency,outstanding FROM customer_opening_balance_totals) balances GROUP BY customer_id,currency HAVING SUM(outstanding)<>0 ORDER BY currency`)
	if err != nil {
		problem(w, 500, "Could not load customer balances")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var b CustomerBalance
		if err = rows.Scan(&id, &b.Currency, &b.Outstanding); err != nil {
			problem(w, 500, "Could not read customer balances")
			return
		}
		if i, ok := indices[id]; ok {
			out[i].Balances = append(out[i].Balances, b)
		}
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read customer balances")
		return
	}
	respond(w, 200, out)
}
func (s *Server) saveCustomer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		Phone           string `json:"phone"`
		Email           string `json:"email"`
		Address         string `json:"address"`
		Notes           string `json:"notes"`
		OpeningBalance  *int   `json:"openingBalance"`
		OpeningCurrency string `json:"openingCurrency"`
		RequestID       string `json:"requestId,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.OpeningBalance != nil && (*req.OpeningBalance < 0 || *req.OpeningBalance > 10000000) {
		problem(w, 400, "Opening balance must be between 0 and 100,000")
		return
	}
	if r.Method != http.MethodPost && (req.OpeningBalance != nil || req.OpeningCurrency != "") {
		problem(w, 400, "Opening debt cannot be changed when editing contact details")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Email = strings.TrimSpace(req.Email)
	req.Address = strings.TrimSpace(req.Address)
	req.Notes = strings.TrimSpace(req.Notes)
	if req.Name == "" || utf8.RuneCountInString(req.Name) > 200 || len(req.Phone) > 50 || len(req.Email) > 254 || utf8.RuneCountInString(req.Address) > 500 || utf8.RuneCountInString(req.Notes) > 1000 {
		problem(w, 400, "Enter a customer name and keep contact details within the field limits")
		return
	}
	if req.Email != "" {
		address, err := mail.ParseAddress(req.Email)
		if err != nil || address.Address != req.Email {
			problem(w, 400, "Enter a valid customer email or leave it blank")
			return
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not save customer")
		return
	}
	defer tx.Rollback()
	var requestHash string
	if r.Method == http.MethodPost && req.RequestID != "" {
		var replay json.RawMessage
		replay, requestHash, err = requestReplay(r.Context(), tx, req.RequestID, "customer-create", principal(r).ID, req)
		if err != nil {
			problem(w, 409, err.Error())
			return
		}
		if replay != nil {
			respond(w, 200, replay)
			return
		}
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	var id int64
	status := 201
	if r.Method == http.MethodPost {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO customers(name,phone,email,address,notes) VALUES($1,$2,$3,$4,$5) RETURNING id`, req.Name, req.Phone, req.Email, req.Address, req.Notes).Scan(&id)
	} else {
		status = 200
		id, err = strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			problem(w, 400, "Invalid customer")
			return
		}
		err = tx.QueryRowContext(r.Context(), `UPDATE customers SET name=$1,phone=$2,email=$3,address=$4,notes=$5 WHERE id=$6 RETURNING id`, req.Name, req.Phone, req.Email, req.Address, req.Notes, id).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Customer not found")
		return
	}
	if err == nil && r.Method == http.MethodPost && req.OpeningBalance != nil && *req.OpeningBalance > 0 {
		var currency string
		err = tx.QueryRowContext(r.Context(), `SELECT currency FROM store_settings WHERE id=1 FOR SHARE`).Scan(&currency)
		if err == nil && req.OpeningCurrency != "" && req.OpeningCurrency != currency {
			problem(w, 409, "Store currency changed. Refresh before entering opening debt")
			return
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO customer_opening_balances(customer_id,amount,currency,created_by,cashier_email) VALUES($1,$2,$3,$4,$5)`, id, *req.OpeningBalance, currency, principal(r).ID, principal(r).Email)
		}
	}
	if err == nil && r.Method == http.MethodPost && req.RequestID != "" {
		err = saveRequest(r.Context(), tx, req.RequestID, "customer-create", requestHash, principal(r).ID, map[string]any{"id": id})
	}
	if err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not save customer")
		return
	}
	respond(w, status, map[string]any{"id": id})
}

type CustomerOpeningBalance struct {
	ID          int64     `json:"id"`
	Amount      int       `json:"amount"`
	Currency    string    `json:"currency"`
	PaidAmount  int       `json:"paidAmount"`
	Outstanding int       `json:"outstanding"`
	Created     time.Time `json:"created"`
}

type CustomerPayment struct {
	OpeningBalanceID *int64    `json:"openingBalanceId"`
	ID               int64     `json:"id"`
	SaleID           int64     `json:"saleId"`
	SessionID        int64     `json:"sessionId"`
	Amount           int       `json:"amount"`
	Payment          string    `json:"payment"`
	Currency         string    `json:"currency"`
	Note             string    `json:"note"`
	CashierEmail     string    `json:"cashierEmail"`
	Created          time.Time `json:"created"`
}

func (s *Server) customerDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid customer")
		return
	}
	var c Customer
	err = scanCustomer(s.db.QueryRowContext(r.Context(), customerColumns+`WHERE id=$1`, id), &c)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Customer not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load customer")
		return
	}
	// Keep complete history so older unpaid receipts remain available for collection.
	rows, err := s.db.QueryContext(r.Context(), saleColumns+`WHERE s.customer_id=$1 AND s.deleted_at IS NULL ORDER BY s.id DESC`, id)
	if err != nil {
		problem(w, 500, "Could not load customer sales")
		return
	}
	sales := []Sale{}
	for rows.Next() {
		var sale Sale
		if err = scanSale(rows, &sale); err != nil {
			break
		}
		sales = append(sales, sale)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		problem(w, 500, "Could not read customer sales")
		return
	}
	var opening *CustomerOpeningBalance
	var b CustomerOpeningBalance
	err = s.db.QueryRowContext(r.Context(), `SELECT id,amount,currency,paid_amount,outstanding,created_at FROM customer_opening_balance_totals WHERE customer_id=$1`, id).Scan(&b.ID, &b.Amount, &b.Currency, &b.PaidAmount, &b.Outstanding, &b.Created)
	if err == nil {
		opening = &b
	} else if !errors.Is(err, sql.ErrNoRows) {
		problem(w, 500, "Could not read opening balance")
		return
	}
	rows, err = s.db.QueryContext(r.Context(), `SELECT p.id,COALESCE(p.sale_id,0),p.session_id,p.amount,p.payment,p.currency,p.note,p.cashier_email,p.created_at,p.opening_balance_id FROM customer_payments p LEFT JOIN sales s ON s.id=p.sale_id LEFT JOIN customer_opening_balances b ON b.id=p.opening_balance_id WHERE s.customer_id=$1 OR b.customer_id=$1 ORDER BY p.id DESC`, id)
	if err != nil {
		problem(w, 500, "Could not load customer payments")
		return
	}
	defer rows.Close()
	payments := []CustomerPayment{}
	for rows.Next() {
		var p CustomerPayment
		if rows.Scan(&p.ID, &p.SaleID, &p.SessionID, &p.Amount, &p.Payment, &p.Currency, &p.Note, &p.CashierEmail, &p.Created, &p.OpeningBalanceID) != nil {
			problem(w, 500, "Could not read customer payments")
			return
		}
		payments = append(payments, p)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read customer payments")
		return
	}
	respond(w, 200, map[string]any{"customer": c, "sales": sales, "payments": payments, "openingBalance": opening})
}
func (s *Server) collectCustomerPayment(w http.ResponseWriter, r *http.Request) {
	customerID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || customerID < 1 {
		problem(w, 400, "Invalid customer")
		return
	}
	var req struct {
		RequestID        string `json:"requestId"`
		SaleID           int64  `json:"saleId"`
		OpeningBalanceID int64  `json:"openingBalanceId,omitempty"`
		SessionID        int64  `json:"sessionId"`
		Amount           int    `json:"amount"`
		Payment          string `json:"payment"`
		Note             string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	if req.SaleID < 0 || req.OpeningBalanceID < 0 || (req.SaleID == 0) == (req.OpeningBalanceID == 0) || req.Amount <= 0 || req.Amount > 10000000 || (req.Payment != "cash" && req.Payment != "card") || len(req.Note) > 500 {
		problem(w, 400, "Choose a receipt or opening balance and enter a positive payment amount and payment method")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not record payment")
		return
	}
	defer tx.Rollback()
	kind := "customer-payment:" + fmtID(customerID)
	replay, hash, err := requestReplay(r.Context(), tx, req.RequestID, kind, principal(r).ID, req)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	if replay != nil {
		respond(w, 200, replay)
		return
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	session, err := s.lockedSession(r.Context(), tx, principal(r).ID)
	if err != nil {
		problem(w, 409, "Open your own register session before collecting a payment")
		return
	}
	if req.SessionID != session.ID {
		problem(w, 409, "The register session has changed. Review it before collecting payment")
		return
	}
	var currency string
	if req.OpeningBalanceID > 0 {
		err = tx.QueryRowContext(r.Context(), `SELECT currency FROM customer_opening_balances WHERE id=$1 AND customer_id=$2 FOR UPDATE`, req.OpeningBalanceID, customerID).Scan(&currency)
	} else {
		err = tx.QueryRowContext(r.Context(), `SELECT currency FROM sales WHERE id=$1 AND customer_id=$2 AND payment='credit' AND deleted_at IS NULL FOR UPDATE`, req.SaleID, customerID).Scan(&currency)
	}
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Customer receipt or opening balance not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load customer sale")
		return
	}
	if currency != session.Currency {
		problem(w, 409, "The register currency must match the customer's receipt")
		return
	}
	var outstanding int
	if req.OpeningBalanceID > 0 {
		err = tx.QueryRowContext(r.Context(), `SELECT outstanding FROM customer_opening_balance_totals WHERE id=$1`, req.OpeningBalanceID).Scan(&outstanding)
	} else {
		err = tx.QueryRowContext(r.Context(), `SELECT outstanding FROM credit_sale_balances WHERE id=$1`, req.SaleID).Scan(&outstanding)
	}
	if err != nil {
		problem(w, 500, "Could not read balance")
		return
	}
	if req.Amount > outstanding {
		problem(w, 409, "Payment exceeds the remaining balance. Refresh the customer account")
		return
	}
	var id int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO customer_payments(sale_id,session_id,amount,payment,currency,note,created_by,cashier_email,opening_balance_id) VALUES(NULLIF($1,0),$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0)) RETURNING id`, req.SaleID, session.ID, req.Amount, req.Payment, currency, req.Note, principal(r).ID, principal(r).Email, req.OpeningBalanceID).Scan(&id)
	if err != nil {
		problem(w, 500, "Could not save customer payment")
		return
	}
	result := map[string]any{"id": id, "saleId": req.SaleID, "openingBalanceId": req.OpeningBalanceID, "amount": req.Amount, "currency": currency, "outstanding": outstanding - req.Amount}
	if saveRequest(r.Context(), tx, req.RequestID, kind, hash, principal(r).ID, result) != nil || tx.Commit() != nil {
		problem(w, 500, "Could not complete customer payment")
		return
	}
	respond(w, 201, result)
}
func (s *Server) customerRoutes(m *http.ServeMux) {
	m.Handle("GET /api/customers", requireRoles(s.customers, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/customers", requireRoles(s.saveCustomer, "superadmin", "admin", "cashier"))
	m.Handle("GET /api/customers/{id}", requireRoles(s.customerDetail, "superadmin", "admin", "cashier"))
	m.Handle("PUT /api/customers/{id}", requireRoles(s.saveCustomer, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/customers/{id}/payments", requireRoles(s.collectCustomerPayment, "superadmin", "admin", "cashier"))
}
