package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func normalizeInvoiceText(value string) (string, string) {
	display := strings.Join(strings.Fields(value), " ")
	return display, strings.ToLower(display)
}

// resolveSupplierInvoice reuses an exact identity first. A normalized match is
// reused only when it is unambiguous, which avoids silently merging historical
// suppliers that differ only by case or whitespace.
func resolveSupplierInvoice(ctx context.Context, tx *sql.Tx, supplier, number, currency string) (int64, error) {
	supplier, supplierKey := normalizeInvoiceText(supplier)
	number, numberKey := normalizeInvoiceText(number)
	if supplier == "" || number == "" || currency == "" {
		return 0, errors.New("supplier and invoice number are required")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%d:%s%d:%s%d:%s", len(supplierKey), supplierKey, len(numberKey), numberKey, len(currency), currency)); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM supplier_invoices WHERE supplier=$1 AND invoice_number=$2 AND currency=$3 AND legacy_purchase_id IS NULL FOR UPDATE`, supplier, number, currency).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM supplier_invoices WHERE supplier_key=$1 AND invoice_key=$2 AND currency=$3 AND legacy_purchase_id IS NULL ORDER BY id FOR UPDATE`, supplierKey, numberKey, currency)
	if err != nil {
		return 0, err
	}
	var matches []int64
	for rows.Next() {
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		matches = append(matches, id)
	}
	rows.Close()
	if len(matches) == 1 {
		return matches[0], nil
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO supplier_invoices(supplier,invoice_number,supplier_key,invoice_key,currency) VALUES($1,$2,$3,$4,$5) RETURNING id`, supplier, number, supplierKey, numberKey, currency).Scan(&id)
	return id, err
}

func (s *Server) invoiceRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/invoices", requireRoles(s.listInvoices, "superadmin"))
	mux.Handle("GET /api/invoices/{id}", requireRoles(s.getInvoice, "superadmin"))
	mux.Handle("POST /api/invoices/{id}/payments", requireRoles(s.createInvoiceTransaction, "superadmin"))
	mux.Handle("POST /api/invoices/{id}/refunds", requireRoles(s.createInvoiceTransaction, "superadmin"))
}

func (s *Server) listInvoices(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "unpaid" && status != "partially_paid" && status != "paid" {
		problem(w, 400, "Invalid invoice status")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
		WITH totals AS (
		 SELECT i.id,i.supplier,i.invoice_number,i.currency,MIN(p.created_at) invoice_date,
		        COUNT(p.id) purchase_count,SUM(p.total) total,
		        COALESCE((SELECT SUM(pp.amount) FROM purchase_payments pp JOIN purchases px ON px.id=pp.purchase_id WHERE px.invoice_id=i.id),0) paid,
		        COALESCE((SELECT SUM(t.amount) FROM supplier_invoice_transactions t WHERE t.invoice_id=i.id AND t.transaction_type='refund'),0) refunded
		 FROM supplier_invoices i JOIN purchases p ON p.invoice_id=i.id
		 GROUP BY i.id
		)
		SELECT id,supplier,invoice_number,currency,invoice_date,purchase_count,total,paid,refunded,
		       GREATEST(total-paid,0),CASE WHEN paid=0 THEN 'unpaid' WHEN paid>=total THEN 'paid' ELSE 'partially_paid' END
		FROM totals
		WHERE ($1='' OR supplier ILIKE '%%'||$1||'%%' OR invoice_number ILIKE '%%'||$1||'%%')
		  AND ($2='' OR CASE WHEN paid=0 THEN 'unpaid' WHEN paid>=total THEN 'paid' ELSE 'partially_paid' END=$2)
		ORDER BY invoice_date DESC,id DESC LIMIT 300`, search, status)
	if err != nil {
		problem(w, 500, "Could not load invoices")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, count, total, paid, refunded, outstanding int64
		var supplier, number, currency, invoiceStatus string
		var date any
		if err := rows.Scan(&id, &supplier, &number, &currency, &date, &count, &total, &paid, &refunded, &outstanding, &invoiceStatus); err != nil {
			problem(w, 500, "Could not read invoices")
			return
		}
		out = append(out, map[string]any{"id": id, "supplier": supplier, "invoiceNumber": number, "currency": currency, "date": date, "purchaseCount": count, "total": total, "paid": paid, "refunded": refunded, "outstanding": outstanding, "status": invoiceStatus})
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read invoices")
		return
	}
	respond(w, 200, out)
}

func (s *Server) getInvoice(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	var supplier, number, currency string
	var date any
	var total, paid, refunded int64
	err := s.db.QueryRowContext(r.Context(), `SELECT i.supplier,i.invoice_number,i.currency,MIN(p.created_at),SUM(p.total),COALESCE((SELECT SUM(pp.amount) FROM purchase_payments pp JOIN purchases px ON px.id=pp.purchase_id WHERE px.invoice_id=i.id),0),COALESCE((SELECT SUM(t.amount) FROM supplier_invoice_transactions t WHERE t.invoice_id=i.id AND t.transaction_type='refund'),0) FROM supplier_invoices i JOIN purchases p ON p.invoice_id=i.id WHERE i.id=$1 GROUP BY i.id`, id).Scan(&supplier, &number, &currency, &date, &total, &paid, &refunded)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Invoice not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load invoice")
		return
	}
	purchaseRows, err := s.db.QueryContext(r.Context(), `SELECT p.id,p.created_at,p.total,COALESCE(string_agg(pi.quantity||' × '||pr.name||' @ '||pi.unit_cost,', ' ORDER BY pi.id),'') FROM purchases p LEFT JOIN purchase_items pi ON pi.purchase_id=p.id LEFT JOIN products pr ON pr.id=pi.product_id WHERE p.invoice_id=$1 GROUP BY p.id ORDER BY p.id`, id)
	if err != nil {
		problem(w, 500, "Could not load linked purchases")
		return
	}
	purchases := []map[string]any{}
	for purchaseRows.Next() {
		var pid, ptotal int64
		var created any
		var items string
		if purchaseRows.Scan(&pid, &created, &ptotal, &items) != nil {
			purchaseRows.Close()
			problem(w, 500, "Could not read linked purchases")
			return
		}
		purchases = append(purchases, map[string]any{"id": pid, "createdAt": created, "total": ptotal, "items": items})
	}
	purchaseRows.Close()
	txRows, err := s.db.QueryContext(r.Context(), `SELECT t.id,t.transaction_type,t.amount,t.currency,t.financial_account_id,a.account_type,t.reference,t.notes,t.reason,t.created_at,u.email FROM supplier_invoice_transactions t JOIN financial_accounts a ON a.id=t.financial_account_id JOIN users u ON u.id=t.created_by WHERE t.invoice_id=$1 UNION ALL SELECT pp.id,'payment',pp.amount,pp.currency,pp.financial_account_id,a.account_type,pp.reference,pp.notes,'',pp.created_at,u.email FROM purchase_payments pp JOIN purchases p ON p.id=pp.purchase_id JOIN financial_accounts a ON a.id=pp.financial_account_id JOIN users u ON u.id=pp.created_by WHERE p.invoice_id=$1 AND pp.invoice_transaction_id IS NULL ORDER BY 10 DESC,1 DESC`, id)
	if err != nil {
		problem(w, 500, "Could not load invoice transactions")
		return
	}
	transactions := []map[string]any{}
	for txRows.Next() {
		var tid, amount, accountID int64
		var kind, curr, accountType, reference, notes, reason, email string
		var created any
		if txRows.Scan(&tid, &kind, &amount, &curr, &accountID, &accountType, &reference, &notes, &reason, &created, &email) != nil {
			txRows.Close()
			problem(w, 500, "Could not read invoice transactions")
			return
		}
		transactions = append(transactions, map[string]any{"id": tid, "type": kind, "amount": amount, "currency": curr, "financialAccountId": accountID, "accountType": accountType, "reference": reference, "notes": notes, "reason": reason, "createdAt": created, "actorEmail": email})
	}
	txRows.Close()
	respond(w, 200, map[string]any{"id": id, "supplier": supplier, "invoiceNumber": number, "currency": currency, "date": date, "total": total, "paid": paid, "refunded": refunded, "outstanding": max(total-paid, 0), "status": func() string {
		if paid == 0 {
			return "unpaid"
		}
		if paid >= total {
			return "paid"
		}
		return "partially_paid"
	}(), "purchases": purchases, "transactions": transactions})
}

type invoiceTransactionRequest struct {
	RequestID          string `json:"requestId"`
	FinancialAccountID int64  `json:"financialAccountId"`
	Amount             int64  `json:"amount"`
	FundingType        string `json:"fundingType"`
	Reference          string `json:"reference"`
	Notes              string `json:"notes"`
	Reason             string `json:"reason"`
}

func (s *Server) createInvoiceTransaction(w http.ResponseWriter, r *http.Request) {
	id := routeID(w, r)
	if id == 0 {
		return
	}
	kind := "payment"
	if strings.HasSuffix(r.URL.Path, "/refunds") {
		kind = "refund"
	}
	var req invoiceTransactionRequest
	if !decode(w, r, &req) {
		return
	}
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.Reference = strings.TrimSpace(req.Reference)
	req.Notes = strings.TrimSpace(req.Notes)
	req.Reason = strings.TrimSpace(req.Reason)
	req.FundingType = strings.TrimSpace(req.FundingType)
	if !requestIDPattern.MatchString(req.RequestID) || req.FinancialAccountID <= 0 || req.Amount <= 0 || req.Reference == "" || len(req.Reference) > 200 || len(req.Notes) > 2000 || len(req.Reason) > 500 {
		problem(w, 400, "Valid request ID, account, amount and reference are required")
		return
	}
	if kind == "payment" {
		if req.FundingType != "superadmin_capital" && req.FundingType != "reinvested_revenue" && req.FundingType != "other" {
			problem(w, 400, "Invalid funding type")
			return
		}
		if req.Reason != "" {
			problem(w, 400, "Payment cannot include a refund reason")
			return
		}
	} else {
		req.FundingType = ""
		if req.Reason == "" {
			problem(w, 400, "Refund reason is required")
			return
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not begin invoice transaction")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, req.RequestID); err != nil {
		problem(w, 500, "Could not lock invoice request")
		return
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	var currency string
	if err = tx.QueryRowContext(r.Context(), `SELECT currency FROM supplier_invoices WHERE id=$1 FOR UPDATE`, id).Scan(&currency); errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Invoice not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not lock invoice")
		return
	}
	var existingID int64
	var oldKind string
	var oldInvoice, oldAccount, oldAmount, oldActor int64
	var oldRef, oldNotes, oldReason string
	err = tx.QueryRowContext(r.Context(), `SELECT id,invoice_id,transaction_type,financial_account_id,amount,reference,notes,reason,created_by FROM supplier_invoice_transactions WHERE request_id=$1`, req.RequestID).Scan(&existingID, &oldInvoice, &oldKind, &oldAccount, &oldAmount, &oldRef, &oldNotes, &oldReason, &oldActor)
	if err == nil {
		if oldInvoice != id || oldKind != kind || oldAccount != req.FinancialAccountID || oldAmount != req.Amount || oldRef != req.Reference || oldNotes != req.Notes || oldReason != req.Reason || oldActor != principal(r).ID {
			problem(w, 409, "Request ID already used for another transaction")
			return
		}
		respond(w, 200, map[string]any{"id": existingID, "duplicate": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		problem(w, 500, "Could not check request")
		return
	}
	lockRows, lockErr := tx.QueryContext(r.Context(), `SELECT id FROM purchases WHERE invoice_id=$1 ORDER BY id FOR UPDATE`, id)
	if lockErr != nil {
		problem(w, 500, "Could not lock invoice purchases")
		return
	}
	for lockRows.Next() {
		var ignored int64
		if lockRows.Scan(&ignored) != nil {
			lockRows.Close()
			problem(w, 500, "Could not lock invoice purchases")
			return
		}
	}
	lockRows.Close()
	var total, paid, refunded int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(p.total),0),COALESCE((SELECT SUM(pp.amount) FROM purchase_payments pp JOIN purchases px ON px.id=pp.purchase_id WHERE px.invoice_id=$1),0),COALESCE((SELECT SUM(t.amount) FROM supplier_invoice_transactions t WHERE t.invoice_id=$1 AND t.transaction_type='refund'),0) FROM purchases p WHERE p.invoice_id=$1`, id).Scan(&total, &paid, &refunded); err != nil {
		problem(w, 500, "Could not calculate invoice balance")
		return
	}
	if kind == "payment" && req.Amount > total-paid {
		problem(w, 409, "Payment exceeds outstanding invoice balance")
		return
	}
	if kind == "refund" && req.Amount > paid-refunded {
		problem(w, 409, "Refund exceeds the previously paid amount available for refund")
		return
	}
	var accountCurrency, accountType string
	var balance int64
	var owner sql.NullInt64
	if err = tx.QueryRowContext(r.Context(), `SELECT currency,account_type,balance,user_id FROM financial_accounts WHERE id=$1 FOR UPDATE`, req.FinancialAccountID).Scan(&accountCurrency, &accountType, &balance, &owner); errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Financial account not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load financial account")
		return
	}
	if accountCurrency != currency {
		problem(w, 409, "Financial account currency mismatch")
		return
	}
	if accountType != "bank" && accountType != "central_cash" && accountType != "superadmin_cash" {
		problem(w, 403, "Select an eligible cash or bank account")
		return
	}
	if accountType == "superadmin_cash" && (!owner.Valid || owner.Int64 != principal(r).ID) {
		problem(w, 403, "Financial account belongs to another user")
		return
	}
	if kind == "payment" && balance < req.Amount {
		problem(w, 409, "Insufficient financial account balance")
		return
	}
	var transactionID int64
	var funding any = req.FundingType
	if kind == "refund" {
		funding = nil
	}
	if err = tx.QueryRowContext(r.Context(), `INSERT INTO supplier_invoice_transactions(invoice_id,transaction_type,financial_account_id,amount,currency,funding_type,request_id,reference,notes,reason,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, id, kind, req.FinancialAccountID, req.Amount, currency, funding, req.RequestID, req.Reference, req.Notes, req.Reason, principal(r).ID).Scan(&transactionID); err != nil {
		problem(w, 500, "Could not save invoice transaction")
		return
	}
	movement := req.Amount
	entryType := "supplier_invoice_refund"
	if kind == "payment" {
		movement = -req.Amount
		entryType = "supplier_invoice_payment"
	}
	if err = postFinancialCashMovement(r.Context(), tx, req.FinancialAccountID, movement, entryType, "supplier_invoice_transaction", transactionID, principal(r).ID, req.Reference+": "+req.Notes); err != nil {
		problem(w, 500, "Could not post invoice transaction")
		return
	}
	if kind == "payment" {
		remaining := req.Amount
		rows, queryErr := tx.QueryContext(r.Context(), `SELECT p.id,p.total-COALESCE((SELECT SUM(pp.amount) FROM purchase_payments pp WHERE pp.purchase_id=p.id),0) outstanding FROM purchases p WHERE p.invoice_id=$1 ORDER BY p.created_at,p.id FOR UPDATE`, id)
		if queryErr != nil {
			problem(w, 500, "Could not lock invoice purchases")
			return
		}
		type allocation struct{ id, amount int64 }
		allocations := []allocation{}
		for rows.Next() {
			var pid, outstanding int64
			if rows.Scan(&pid, &outstanding) != nil {
				rows.Close()
				problem(w, 500, "Could not read invoice purchases")
				return
			}
			if outstanding > 0 && remaining > 0 {
				amount := min(outstanding, remaining)
				allocations = append(allocations, allocation{pid, amount})
				remaining -= amount
			}
		}
		rows.Close()
		if remaining != 0 {
			problem(w, 409, "Invoice balance changed; refresh and retry")
			return
		}
		for _, a := range allocations {
			var paymentID int64
			allocationRequest := fmt.Sprintf("%s-p%d", req.RequestID, a.id)
			if err = tx.QueryRowContext(r.Context(), `INSERT INTO purchase_payments(purchase_id,financial_account_id,amount,currency,funding_type,request_id,reference,notes,created_by,invoice_transaction_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, a.id, req.FinancialAccountID, a.amount, currency, req.FundingType, allocationRequest, req.Reference, req.Notes, principal(r).ID, transactionID).Scan(&paymentID); err != nil {
				problem(w, 500, "Could not allocate invoice payment")
				return
			}
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO purchase_funding_allocations(purchase_id,funding_type,amount,currency,created_by) VALUES($1,$2,$3,$4,$5)`, a.id, req.FundingType, a.amount, currency, principal(r).ID); err != nil {
				problem(w, 500, "Could not record invoice funding allocation")
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		problem(w, 500, "Could not commit invoice transaction")
		return
	}
	respond(w, 201, map[string]any{"id": transactionID, "type": kind, "amount": req.Amount, "outstanding": max(total-paid-func() int64 {
		if kind == "payment" {
			return req.Amount
		}
		return 0
	}(), 0)})
}
