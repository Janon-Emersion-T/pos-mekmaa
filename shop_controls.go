package main

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func beginOperation(w http.ResponseWriter, r *http.Request, s *Server, id, kind string, body any) (*sql.Tx, string) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start operation")
		return nil, ""
	}
	replay, hash, err := requestReplay(r.Context(), tx, id, kind, principal(r).ID, body)
	if err != nil {
		tx.Rollback()
		problem(w, 409, err.Error())
		return nil, ""
	}
	if replay != nil {
		tx.Rollback()
		respond(w, 200, replay)
		return nil, ""
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		tx.Rollback()
		problem(w, 500, "Could not record actor")
		return nil, ""
	}
	return tx, hash
}
func finishOperation(w http.ResponseWriter, r *http.Request, tx *sql.Tx, id, kind, hash string, result any) {
	if saveRequest(r.Context(), tx, id, kind, hash, principal(r).ID, result) != nil || tx.Commit() != nil {
		problem(w, 500, "Could not complete operation. Retry to recover the result")
		return
	}
	respond(w, 201, result)
}
func validReason(reason string) bool { return strings.TrimSpace(reason) != "" && len(reason) <= 500 }
func routeID(w http.ResponseWriter, r *http.Request) int64 {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		problem(w, 400, "Invalid record")
		return 0
	}
	return id
}
func (s *Server) correctOpeningBalance(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		RequestID      string  `json:"requestId"`
		Amount         int     `json:"amount"`
		ExpectedAmount int     `json:"expectedAmount"`
		Reason         string  `json:"reason"`
		DueDate        *string `json:"dueDate"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Amount < 0 || req.Amount > 10000000 || !validReason(req.Reason) {
		problem(w, 400, "Enter the corrected amount and a reason")
		return
	}
	if req.DueDate != nil && *req.DueDate != "" {
		if _, err := time.Parse("2006-01-02", *req.DueDate); err != nil {
			problem(w, 400, "Invalid due date")
			return
		}
	}
	kind := "opening-correction:" + fmtID(id)
	tx, hash := beginOperation(w, r, s, req.RequestID, kind, req)
	if tx == nil {
		return
	}
	defer tx.Rollback()
	var cid, bid int64
	if tx.QueryRowContext(r.Context(), `SELECT id FROM customers WHERE id=$1 FOR UPDATE`, id).Scan(&cid) != nil {
		problem(w, 404, "Customer not found")
		return
	}
	if tx.QueryRowContext(r.Context(), `SELECT id FROM customer_opening_balances WHERE customer_id=$1 FOR UPDATE`, id).Scan(&bid) != nil {
		problem(w, 404, "Opening balance not found")
		return
	}
	var amount, paid int
	if tx.QueryRowContext(r.Context(), `SELECT amount+adjustment_total,paid_amount FROM customer_opening_balance_totals WHERE id=$1`, bid).Scan(&amount, &paid) != nil {
		problem(w, 500, "Could not read balance")
		return
	}
	if amount != req.ExpectedAmount {
		problem(w, 409, "Opening balance changed. Refresh and review it")
		return
	}
	if req.Amount < paid {
		problem(w, 409, "Corrected debt cannot be below payments already recorded. Reverse incorrect payments first")
		return
	}
	if req.Amount != amount {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO customer_balance_adjustments(opening_balance_id,amount,reason,created_by,cashier_email) VALUES($1,$2,$3,$4,$5)`, bid, req.Amount-amount, req.Reason, principal(r).ID, principal(r).Email); err != nil {
			problem(w, 500, "Could not save correction")
			return
		}
	}
	if req.DueDate != nil {
		if _, err := tx.ExecContext(r.Context(), `UPDATE customer_opening_balances SET due_date=NULLIF($1,'')::date WHERE id=$2`, *req.DueDate, bid); err != nil {
			problem(w, 500, "Could not update due date")
			return
		}
	}
	finishOperation(w, r, tx, req.RequestID, kind, hash, map[string]any{"amount": req.Amount, "outstanding": req.Amount - paid})
}
func (s *Server) reverseCustomerPayment(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Reason    string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if !validReason(req.Reason) {
		problem(w, 400, "Enter a reason for reversing the payment")
		return
	}
	kind := "payment-reversal:" + fmtID(id)
	tx, hash := beginOperation(w, r, s, req.RequestID, kind, req)
	if tx == nil {
		return
	}
	defer tx.Rollback()
	var sid, cid, saleID, openingID, originalCashierID int64
	var amount int
	var paymentMethod, paymentCurrency string
	err := tx.QueryRowContext(r.Context(), `
		SELECT p.session_id,
		       COALESCE(s.customer_id,b.customer_id),
		       COALESCE(p.sale_id,0),
		       COALESCE(p.opening_balance_id,0),
		       p.amount,
		       p.payment,
		       p.currency,
		       rs.opened_by
		FROM customer_payments p
		JOIN register_sessions rs ON rs.id=p.session_id
		LEFT JOIN sales s ON s.id=p.sale_id
		LEFT JOIN customer_opening_balances b ON b.id=p.opening_balance_id
		WHERE p.id=$1
	`, id).Scan(
		&sid, &cid, &saleID, &openingID, &amount,
		&paymentMethod, &paymentCurrency, &originalCashierID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Payment not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not read payment")
		return
	}
	var locked int64
	if tx.QueryRowContext(r.Context(), `SELECT id FROM register_sessions WHERE id=$1 FOR UPDATE`, sid).Scan(&locked) != nil || tx.QueryRowContext(r.Context(), `SELECT id FROM customers WHERE id=$1 FOR UPDATE`, cid).Scan(&locked) != nil {
		problem(w, 500, "Could not lock payment")
		return
	}
	var paid int
	if saleID > 0 {
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM sales WHERE id=$1 FOR UPDATE`, saleID).Scan(&locked)
		if err == nil {
			err = tx.QueryRowContext(r.Context(), `SELECT paid_amount FROM credit_sale_balances WHERE id=$1`, saleID).Scan(&paid)
		}
	} else {
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM customer_opening_balances WHERE id=$1 FOR UPDATE`, openingID).Scan(&locked)
		if err == nil {
			err = tx.QueryRowContext(r.Context(), `SELECT paid_amount FROM customer_opening_balance_totals WHERE id=$1`, openingID).Scan(&paid)
		}
	}
	if err != nil {
		problem(w, 500, "Could not read balance")
		return
	}
	var reversed bool
	if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM customer_payment_reversals WHERE payment_id=$1)`, id).Scan(&reversed) != nil {
		problem(w, 500, "Could not read reversal")
		return
	}
	if reversed {
		problem(w, 409, "This payment has already been reversed")
		return
	}
	if paid < amount {
		problem(w, 409, "This payment funded money already refunded. It cannot be reversed")
		return
	}
	var reversalID int64
	err = tx.QueryRowContext(
		r.Context(),
		`INSERT INTO customer_payment_reversals(payment_id,reason,created_by,cashier_email)
		 VALUES($1,$2,$3,$4) RETURNING id`,
		id, req.Reason, principal(r).ID, principal(r).Email,
	).Scan(&reversalID)
	if err != nil {
		problem(w, 500, "Could not reverse payment")
		return
	}

	if paymentMethod == "cash" {
		var originalLedgerCount int
		if err = tx.QueryRowContext(
			r.Context(),
			`SELECT COUNT(*)
			 FROM financial_ledger_entries
			 WHERE reference_type='customer_payment'
			   AND reference_id=$1
			   AND entry_type='cash_collection'`,
			id,
		).Scan(&originalLedgerCount); err != nil {
			problem(w, 500, "Could not verify original cash posting")
			return
		}

		// Historical payments without an original financial posting
		// must not create an unmatched negative cash entry.
		if originalLedgerCount == 0 {
			finishOperation(w, r, tx, req.RequestID, kind, hash,
				map[string]any{"id": id, "reversed": true})
			return
		}

		var originalAccountID int64
		var originalAmount int64
		err = tx.QueryRowContext(r.Context(), `
			SELECT l.account_id, l.amount
			FROM financial_ledger_entries l
			JOIN financial_accounts a ON a.id=l.account_id
			WHERE l.reference_type='customer_payment'
			  AND l.reference_id=$1
			  AND l.entry_type='cash_collection'
			  AND a.account_type='staff_cash'
			  AND a.user_id=$2
			  AND a.currency=$3
		`, id, originalCashierID, paymentCurrency).
			Scan(&originalAccountID, &originalAmount)
		if err != nil || originalAmount != int64(amount) {
			problem(w, 409, "Original cash posting does not match this payment")
			return
		}

		if err = postFinancialCashMovement(
			r.Context(), tx,
			originalAccountID, -int64(amount),
			"cash_collection_reversal",
			"customer_payment_reversal", reversalID,
			principal(r).ID, req.Reason,
		); err != nil {
			problem(w, 500, "Could not post payment reversal")
			return
		}
	}
	finishOperation(w, r, tx, req.RequestID, kind, hash, map[string]any{"id": id, "reversed": true})
}
func (s *Server) customerCreditPolicy(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		CreditLimit      *int   `json:"creditLimit"`
		Currency         string `json:"currency"`
		PaymentTermsDays int    `json:"paymentTermsDays"`
	}
	if !decode(w, r, &req) {
		return
	}
	valid := false
	for _, c := range supportedCurrencies {
		if c.Code == req.Currency {
			valid = true
		}
	}
	if !valid || req.PaymentTermsDays < 0 || req.PaymentTermsDays > 365 || (req.CreditLimit != nil && (*req.CreditLimit < 0 || *req.CreditLimit > 10000000)) {
		problem(w, 400, "Choose a supported currency, nonnegative credit limit, and terms of 0–365 days")
		return
	}
	result, err := s.auditedExec(r, `UPDATE customers SET credit_limit=$1,credit_currency=$2,payment_terms_days=$3 WHERE id=$4`, req.CreditLimit, req.Currency, req.PaymentTermsDays, id)
	if err != nil {
		problem(w, 500, "Could not save credit policy")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		problem(w, 404, "Customer not found")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func finishSessionClose(w http.ResponseWriter, r *http.Request, tx *sql.Tx, x Session, cash int, expected *int, reason string, admin bool) bool {
	reason = strings.TrimSpace(reason)
	if cash < 0 || cash > 2147483647 || len(reason) > 500 {
		problem(w, 400, "Enter valid counted cash and a reason of at most 500 characters")
		return false
	}
	if expected != nil && *expected != x.ExpectedCash {
		problem(w, 409, "Register totals changed. Refresh before closing")
		return false
	}
	if (admin || cash != x.ExpectedCash) && !validReason(reason) {
		problem(w, 400, "Explain the cash difference or administrative closure")
		return false
	}
	if cash < x.ExpectedCash {
		if _, err := recordCashShortage(
			r.Context(),
			tx,
			x.OpenedBy,
			x.ID,
			int64(x.ExpectedCash),
			int64(cash),
			x.Currency,
			reason,
		); err != nil {
			problem(w, 500, "Could not record cash shortage")
			return false
		}
	}

	// Record the physical closing count for later superadmin verification.
	// This does not change the persistent financial account balance.
	_, err := tx.ExecContext(r.Context(), `
		INSERT INTO register_cash_reconciliations(
			session_id,user_id,currency,
			expected_cash,counted_cash,
			cash_difference,shortage_cash,
			closed_ledger_entry_id,remarks
		)
		VALUES(
			$1,$2,$3,$4,$5,
			$5::bigint - $4::bigint,
			GREATEST($4::bigint - $5::bigint, 0),
			COALESCE((
				SELECT MAX(l.id)
				FROM financial_ledger_entries l
				JOIN financial_accounts fa ON fa.id=l.account_id
				WHERE fa.account_type='staff_cash'
				  AND fa.user_id=$2
				  AND fa.currency=$3
			),0),
			$6
		)
	`, x.ID, x.OpenedBy, x.Currency,
		int64(x.ExpectedCash), int64(cash), reason)
	if err != nil {
		problem(w, 500, "Could not record register reconciliation")
		return false
	}

	_, err = tx.ExecContext(r.Context(), `UPDATE register_sessions SET status='closed',closed_at=now(),closing_cash=$1,closed_by=$2,closed_by_email=$3,closing_expected_cash=$4,closing_reason=$5 WHERE id=$6`, cash, principal(r).ID, principal(r).Email, x.ExpectedCash, reason, x.ID)
	if err != nil {
		problem(w, 500, "Could not close session")
		return false
	}
	if err = tx.Commit(); err != nil {
		problem(w, 500, "Could not close session")
		return false
	}
	respond(w, 200, map[string]any{"expectedCash": x.ExpectedCash, "closingCash": cash, "difference": cash - x.ExpectedCash})
	return true
}
func (s *Server) adminCloseSession(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var req struct {
		ClosingCash  int    `json:"closingCash"`
		ExpectedCash *int   `json:"expectedCash"`
		Reason       string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ExpectedCash == nil {
		problem(w, 400, "Review the register totals before closing")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start closure")
		return
	}
	defer tx.Rollback()
	var locked int64
	if tx.QueryRowContext(r.Context(), `SELECT id FROM register_sessions WHERE id=$1 AND status='open' FOR UPDATE`, id).Scan(&locked) != nil {
		problem(w, 409, "This register is no longer open")
		return
	}
	var x Session
	if scanSession(tx.QueryRowContext(r.Context(), sessionColumns+`WHERE r.id=$1`, id), &x) != nil || auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not read session")
		return
	}
	finishSessionClose(w, r, tx, x, req.ClosingCash, req.ExpectedCash, req.Reason, true)
}

type StatementEntry struct {
	Created   time.Time `json:"created"`
	Kind      string    `json:"kind"`
	Reference int64     `json:"reference"`
	Currency  string    `json:"currency"`
	Change    int64     `json:"change"`
	Balance   int64     `json:"balance"`
	Note      string    `json:"note"`
}

func (s *Server) customerStatement(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var c Customer
	if err := scanCustomer(s.db.QueryRowContext(r.Context(), customerColumns+`WHERE id=$1`, id), &c); err != nil {
		problem(w, 404, "Customer not found")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT created,kind,reference,currency,amount,note FROM (
 SELECT created_at AS created,'Opening balance' AS kind,id AS reference,currency,amount::bigint AS amount,'Previous purchases' AS note FROM customer_opening_balances WHERE customer_id=$1
 UNION ALL SELECT a.created_at,'Balance correction',a.id,b.currency,a.amount,a.reason FROM customer_balance_adjustments a JOIN customer_opening_balances b ON b.id=a.opening_balance_id WHERE b.customer_id=$1
 UNION ALL SELECT created_at,'Credit sale',id,currency,total,items FROM (SELECT s.*,COALESCE((SELECT string_agg(product_name,', ') FROM sale_items WHERE sale_id=s.id),'') AS items FROM sales s) s WHERE customer_id=$1 AND payment='credit' AND deleted_at IS NULL
 UNION ALL SELECT p.created_at,'Payment',p.id,p.currency,-p.amount::bigint,p.note FROM customer_payments p LEFT JOIN sales s ON s.id=p.sale_id LEFT JOIN customer_opening_balances b ON b.id=p.opening_balance_id WHERE s.customer_id=$1 OR b.customer_id=$1
 UNION ALL SELECT v.created_at,'Payment reversal',v.id,p.currency,p.amount,v.reason FROM customer_payment_reversals v JOIN customer_payments p ON p.id=v.payment_id LEFT JOIN sales s ON s.id=p.sale_id LEFT JOIN customer_opening_balances b ON b.id=p.opening_balance_id WHERE s.customer_id=$1 OR b.customer_id=$1
 UNION ALL SELECT f.created_at,'Return: debt cancelled',f.id,f.currency,-(f.total-f.cash_returned-f.card_returned)::bigint,f.reason FROM refunds f JOIN sales s ON s.id=f.sale_id WHERE s.customer_id=$1 AND s.payment='credit'
 ) entries ORDER BY created,reference,kind`, id)
	if err != nil {
		problem(w, 500, "Could not load statement")
		return
	}
	defer rows.Close()
	entries := []StatementEntry{}
	balances := map[string]int64{}
	for rows.Next() {
		var e StatementEntry
		if rows.Scan(&e.Created, &e.Kind, &e.Reference, &e.Currency, &e.Change, &e.Note) != nil {
			problem(w, 500, "Could not read statement")
			return
		}
		balances[e.Currency] += e.Change
		e.Balance = balances[e.Currency]
		entries = append(entries, e)
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read statement")
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="customer-%d-statement.csv"`, id))
		writer := csv.NewWriter(w)
		defer writer.Flush()
		writer.Write([]string{"date", "type", "reference", "currency", "debt_change", "balance", "note"})
		for _, e := range entries {
			note := e.Note
			if strings.ContainsAny(strings.TrimLeft(note, " \t\r\n")[:min(1, len(strings.TrimLeft(note, " \t\r\n")))], "=+-@") {
				note = "'" + note
			}
			writer.Write([]string{e.Created.In(businessLocation()).Format(time.RFC3339), e.Kind, fmtID(e.Reference), e.Currency, decimalAmount(int(e.Change)), decimalAmount(int(e.Balance)), note})
		}
		return
	}
	respond(w, 200, map[string]any{"customer": c, "entries": entries, "balances": balances})
}
func (s *Server) customerPaymentReceipt(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var p CustomerPayment
	var cid int64
	var name string
	err := s.db.QueryRowContext(r.Context(), `SELECT p.id,COALESCE(p.sale_id,0),p.opening_balance_id,p.session_id,p.amount,p.payment,p.currency,p.note,p.cashier_email,p.created_at,c.id,c.name,EXISTS(SELECT 1 FROM customer_payment_reversals WHERE payment_id=p.id),COALESCE((SELECT reason FROM customer_payment_reversals WHERE payment_id=p.id),'') FROM customer_payments p LEFT JOIN sales s ON s.id=p.sale_id LEFT JOIN customer_opening_balances b ON b.id=p.opening_balance_id JOIN customers c ON c.id=COALESCE(s.customer_id,b.customer_id) WHERE p.id=$1`, id).Scan(&p.ID, &p.SaleID, &p.OpeningBalanceID, &p.SessionID, &p.Amount, &p.Payment, &p.Currency, &p.Note, &p.CashierEmail, &p.Created, &cid, &name, &p.Reversed, &p.ReversalReason)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Payment not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not read receipt")
		return
	}
	respond(w, 200, map[string]any{"payment": p, "customerId": cid, "customerName": name})
}
func (s *Server) controlRoutes(m *http.ServeMux) {
	m.Handle("POST /api/customers/{id}/opening-corrections", requireRoles(s.correctOpeningBalance, "superadmin", "admin"))
	m.Handle("PUT /api/customers/{id}/credit-policy", requireRoles(s.customerCreditPolicy, "superadmin", "admin"))
	m.Handle("GET /api/customers/{id}/statement", requireRoles(s.customerStatement, "superadmin", "admin", "cashier"))
	m.Handle("GET /api/customer-payments/{id}", requireRoles(s.customerPaymentReceipt, "superadmin", "admin", "cashier"))
	m.Handle("POST /api/customer-payments/{id}/reverse", requireRoles(s.reverseCustomerPayment, "superadmin", "admin"))
	m.Handle("POST /api/sessions/{id}/close", requireRoles(s.adminCloseSession, "superadmin", "admin"))
}
