package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupplierInvoicesPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	setTestBootstrapAdmin(t)
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := fmt.Sprintf("invoice_test_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := openDB(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var adminID int64
	var currency string
	if err = db.QueryRow(`SELECT id FROM users WHERE role='superadmin' AND active LIMIT 1`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT currency FROM store_settings WHERE id=1`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	var bankID, cashID int64
	if err = db.QueryRow(`UPDATE financial_accounts SET balance=100000 WHERE account_type='bank' AND currency=$1 RETURNING id`, currency).Scan(&bankID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`UPDATE financial_accounts SET balance=100000 WHERE account_type='central_cash' AND currency=$1 RETURNING id`, currency).Scan(&cashID); err != nil {
		t.Fatal(err)
	}

	createInvoicePurchase := func(supplier, number string, total int64) (int64, int64) {
		t.Helper()
		tx, _ := db.Begin()
		defer tx.Rollback()
		invoiceID, e := resolveSupplierInvoice(context.Background(), tx, supplier, number, currency)
		if e != nil {
			t.Fatal(e)
		}
		var purchaseID int64
		e = tx.QueryRow(`INSERT INTO purchases(supplier,invoice_number,total,status,created_by,invoice_id) VALUES($1,$2,$3,'received',$4,$5) RETURNING id`, strings.TrimSpace(supplier), strings.TrimSpace(number), total, adminID, invoiceID).Scan(&purchaseID)
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		return invoiceID, purchaseID
	}
	invoiceID, p1 := createInvoicePurchase("Kenady", "ken001", 1000)
	invoiceID2, p2 := createInvoicePurchase("  kenady  ", " KEN001 ", 2000)
	if invoiceID2 != invoiceID {
		t.Fatalf("normalized invoice did not group: %d != %d", invoiceID2, invoiceID)
	}
	otherInvoice, _ := createInvoicePurchase("Other Supplier", "ken001", 900)
	if otherInvoice == invoiceID {
		t.Fatal("same invoice number merged different suppliers")
	}
	concurrentInvoice, _ := createInvoicePurchase("Concurrent Supplier", "C-1", 1000)

	// A historical individual-purchase payment is part of the invoice totals.
	var legacyPaymentID int64
	if err = db.QueryRow(`INSERT INTO purchase_payments(purchase_id,financial_account_id,amount,currency,funding_type,request_id,created_by) VALUES($1,$2,200,$3,'other','legacy-invoice-payment-0001',$4) RETURNING id`, p1, cashID, currency, adminID).Scan(&legacyPaymentID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO financial_ledger_entries(account_id,amount,entry_type,reference_type,reference_id,created_by) VALUES($1,-200,'stock_purchase_payment','purchase_payment',$2,$3)`, cashID, legacyPaymentID, adminID); err != nil {
		t.Fatal(err)
	}

	s := &Server{db: db}
	mux := http.NewServeMux()
	s.invoiceRoutes(mux)
	admin := &User{ID: adminID, Email: "admin@example.test", Role: "superadmin", Active: true}
	call := func(method, path, body string, user *User) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, user))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	payload := func(request string, account, amount int64) string {
		return fmt.Sprintf(`{"requestId":%q,"financialAccountId":%d,"amount":%d,"fundingType":"other","reference":"INV-REF","notes":"test"}`, request, account, amount)
	}

	list := call(http.MethodGet, "/api/invoices", "", admin)
	if list.Code != 200 {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	var invoices []map[string]any
	if json.Unmarshal(list.Body.Bytes(), &invoices) != nil || len(invoices) != 3 {
		t.Fatalf("expected 3 grouped invoices: %s", list.Body.String())
	}
	if w := call(http.MethodGet, "/api/invoices", "", &User{ID: 999, Role: "admin"}); w.Code != 403 {
		t.Fatalf("admin invoice access=%d", w.Code)
	}

	firstBody := payload("invoice-payment-first-0001", bankID, 1000)
	if w := call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/payments", invoiceID), firstBody, admin); w.Code != 201 {
		t.Fatalf("partial payment: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/payments", invoiceID), firstBody, admin); w.Code != 200 {
		t.Fatalf("duplicate payment: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/payments", invoiceID), payload("invoice-payment-over-0001", cashID, 1900), admin); w.Code != 409 {
		t.Fatalf("overpayment=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/payments", invoiceID), payload("invoice-payment-final-0001", cashID, 1800), admin); w.Code != 201 {
		t.Fatalf("final payment: %d %s", w.Code, w.Body.String())
	}
	refund := fmt.Sprintf(`{"requestId":"invoice-refund-0001","financialAccountId":%d,"amount":500,"reference":"SUP-REF","reason":"Supplier repaid overpayment","notes":"No stock return"}`, bankID)
	if w := call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/refunds", invoiceID), refund, admin); w.Code != 201 {
		t.Fatalf("refund: %d %s", w.Code, w.Body.String())
	}

	detail := call(http.MethodGet, fmt.Sprintf("/api/invoices/%d", invoiceID), "", admin)
	if detail.Code != 200 {
		t.Fatalf("detail: %d %s", detail.Code, detail.Body.String())
	}
	var summary struct {
		Total, Paid, Refunded, Outstanding int64
		Purchases                          []any
		Transactions                       []any
	}
	if err = json.Unmarshal(detail.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3000 || summary.Paid != 3000 || summary.Refunded != 500 || summary.Outstanding != 0 || len(summary.Purchases) != 2 || len(summary.Transactions) != 4 {
		t.Fatalf("bad invoice summary: %+v", summary)
	}
	var ledgerSum int64
	var ledgerCount int
	if err = db.QueryRow(`SELECT COALESCE(SUM(amount),0),COUNT(*) FROM financial_ledger_entries WHERE reference_type='supplier_invoice_transaction'`).Scan(&ledgerSum, &ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerSum != -2300 || ledgerCount != 3 {
		t.Fatalf("invoice ledger sum=%d count=%d", ledgerSum, ledgerCount)
	}
	var allocated int64
	if err = db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM purchase_payments WHERE purchase_id IN ($1,$2)`, p1, p2).Scan(&allocated); err != nil {
		t.Fatal(err)
	}
	if allocated != 3000 {
		t.Fatalf("allocated payments=%d", allocated)
	}

	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes <- call(http.MethodPost, fmt.Sprintf("/api/invoices/%d/payments", concurrentInvoice), payload(fmt.Sprintf("invoice-concurrent-%04d", i), cashID, 700), admin).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 201 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Errorf("concurrent status=%d", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent success=%d conflict=%d", success, conflict)
	}
}
