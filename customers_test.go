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

func TestCustomersAndCreditSales(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	setTestBootstrapAdmin(t)
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := fmt.Sprintf("customers_test_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := openDB(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var userID, productID int64
	if err = db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES('cashier@example.test','unused','cashier') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO products(name,category,price,stock) VALUES('Item','Test',5000,100) RETURNING id`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("customer", 8)
	if _, err = db.Exec(`INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, userID, tokenHash(token)); err != nil {
		t.Fatal(err)
	}
	h := (&Server{db: db}).routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := call(method, path, body)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	idFrom := func(w *httptest.ResponseRecorder) int64 {
		t.Helper()
		var v struct{ ID int64 }
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	scalar := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	usersBefore := scalar(`SELECT count(*) FROM users`)
	request("POST", "/api/customers", `{"name":" "}`, 400)
	request("POST", "/api/customers", `{"name":"Jane","email":"not-an-email"}`, 400)
	customerID := idFrom(request("POST", "/api/customers", `{"name":" Jane Silva ","phone":"0771234567","notes":"Regular customer"}`, 201))
	otherID := idFrom(request("POST", "/api/customers", `{"name":"Other customer"}`, 201))
	customerPath := fmt.Sprintf("/api/customers/%d", customerID)
	if scalar(`SELECT count(*) FROM users`) != usersBefore {
		t.Fatal("customer creation created a login")
	}
	request("GET", "/customers", "", 200)
	request("GET", customerPath, "", 200)
	sessionID := idFrom(request("POST", "/api/session/open", `{"openingCash":1000}`, 201))
	checkout := func(key string, cid int64, initial int, method string) string {
		return fmt.Sprintf(`{"requestId":%q,"items":[{"productId":%d,"quantity":2}],"payment":"credit","customerId":%d,"initialPayment":%d,"initialPaymentMethod":%q,"sessionId":%d,"expectedTotal":10000}`, key, productID, cid, initial, method, sessionID)
	}
	request("POST", "/api/checkout", fmt.Sprintf(`{"items":[{"productId":%d,"quantity":1}],"payment":"credit"}`, productID), 400)
	request("POST", "/api/checkout", checkout("credit-invalid-customer", 999999, 0, "cash"), 400)
	request("POST", "/api/checkout", checkout("credit-invalid-deposit", customerID, 10001, "cash"), 400)
	request("POST", "/api/checkout", checkout("credit-full-deposit", customerID, 10000, "cash"), 400)
	body := checkout("credit-first-purchase", customerID, 2000, "cash")
	saleID := idFrom(request("POST", "/api/checkout", body, 201))
	request("POST", "/api/checkout", body, 200)
	if scalar(`SELECT count(*) FROM customer_payments WHERE sale_id=$1`, saleID) != 1 || scalar(`SELECT stock FROM products WHERE id=$1`, productID) != 98 {
		t.Fatal("checkout replay changed payments or stock")
	}
	balance := func(want int) {
		t.Helper()
		if got := scalar(`SELECT outstanding FROM credit_sale_balances WHERE id=$1`, saleID); got != want {
			t.Fatalf("balance=%d want=%d", got, want)
		}
	}
	cash := func(want int) {
		t.Helper()
		var x Session
		if err := json.Unmarshal(request("GET", "/api/session", "", 200).Body.Bytes(), &x); err != nil {
			t.Fatal(err)
		}
		if x.ExpectedCash != want {
			t.Fatalf("expected cash=%d want=%d", x.ExpectedCash, want)
		}
	}
	balance(8000)
	cash(3000)
	var saved Sale
	if err = json.Unmarshal(request("GET", fmt.Sprintf("/api/sales/%d", saleID), "", 200).Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CustomerName != "Jane Silva" || saved.PaidAmount != 2000 || saved.Outstanding != 8000 {
		t.Fatalf("receipt: %+v", saved)
	}
	request("PUT", customerPath, `{"name":"Jane Updated","phone":"0771234567"}`, 200)
	if err = json.Unmarshal(request("GET", fmt.Sprintf("/api/sales/%d", saleID), "", 200).Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.CustomerName != "Jane Silva" {
		t.Fatal("customer edit changed receipt snapshot")
	}
	payment := func(key string, amount int, method string, sid int64) string {
		return fmt.Sprintf(`{"requestId":%q,"saleId":%d,"sessionId":%d,"amount":%d,"payment":%q,"note":"Collection"}`, key, saleID, sid, amount, method)
	}
	path := customerPath + "/payments"
	request("POST", path, payment("credit-stale-register", 100, "cash", sessionID+100), 409)
	request("POST", fmt.Sprintf("/api/customers/%d/payments", otherID), payment("credit-wrong-customer", 100, "cash", sessionID), 404)
	request("POST", path, payment("credit-zero-payment", 0, "cash", sessionID), 400)
	request("POST", path, payment("credit-overpayment", 8001, "cash", sessionID), 409)
	body = payment("credit-cash-collection", 3000, "cash", sessionID)
	request("POST", path, body, 201)
	request("POST", path, body, 200)
	request("POST", path, payment("credit-cash-collection", 2000, "cash", sessionID), 409)
	request("POST", path, payment("credit-card-collection", 1000, "card", sessionID), 201)
	balance(4000)
	cash(6000)
	// Two collectors cannot both spend the same remaining balance.
	var results [2]*httptest.ResponseRecorder
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = call("POST", path, payment(fmt.Sprintf("credit-concurrent-%d", i), 3000, "cash", sessionID))
		}(i)
	}
	wg.Wait()
	if !((results[0].Code == 201 && results[1].Code == 409) || (results[1].Code == 201 && results[0].Code == 409)) {
		t.Fatalf("concurrent payments: %d %s / %d %s", results[0].Code, results[0].Body.String(), results[1].Code, results[1].Body.String())
	}
	balance(1000)
	cash(9000)
	var list []Customer
	if err = json.Unmarshal(request("GET", "/api/customers", "", 200).Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("customers=%d", len(list))
	}
	for _, c := range list {
		if c.ID == customerID && (len(c.Balances) != 1 || c.Balances[0].Outstanding != 1000) {
			t.Fatalf("balances: %+v", c)
		}
	}
	request("POST", fmt.Sprintf("/api/sales/%d/refunds", saleID), `{}`, 403)
	if _, err = db.Exec(`UPDATE users SET role='superadmin' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	request("DELETE", fmt.Sprintf("/api/sales/%d", saleID), fmt.Sprintf(`{"confirmation":"DELETE %d","reason":"test"}`, saleID), 409)
	var lineID int64
	if err = db.QueryRow(`SELECT id FROM sale_items WHERE sale_id=$1`, saleID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	refundPath := fmt.Sprintf("/api/sales/%d/refunds", saleID)
	refund := func(key string, outstanding int) string {
		return fmt.Sprintf(`{"requestId":%q,"reason":"Returned","confirmed":true,"expectedOutstanding":%d,"items":[{"saleItemId":%d,"quantity":1,"restock":true}]}`, key, outstanding, lineID)
	}
	request("POST", refundPath, refund("credit-return-stale-balance", 4000), 409)
	request("POST", refundPath, refund("credit-return-first-item", 1000), 201)
	request("POST", refundPath, refund("credit-return-first-item", 1000), 200)
	balance(0)
	cash(5000)
	if scalar(`SELECT cash_returned FROM refunds WHERE sale_id=$1`, saleID) != 4000 {
		t.Fatal("credit refund must cancel debt before returning money")
	}
	request("POST", refundPath, refund("credit-return-second-item", 0), 201)
	cash(0)
	balance(0)
	if scalar(`SELECT stock FROM products WHERE id=$1`, productID) != 100 {
		t.Fatal("returned stock mismatch")
	}
	request("POST", path, payment("credit-payment-after-return", 1, "cash", sessionID), 409)
	var report struct{ Rows []DailyRow }
	if err = json.Unmarshal(request("GET", "/api/reports/daily", "", 200).Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("report: %+v", report)
	}
	d := report.Rows[0]
	if d.CreditSales != 10000 || d.CashCollections != 8000 || d.CardCollections != 1000 || d.CashSales != 0 || d.CashRefunds != 9000 || d.CreditReturns != 1000 {
		t.Fatalf("credit report: %+v", d)
	}
	// A fully unpaid sale has no cash effect and can be deleted without losing payments.
	unpaidID := idFrom(request("POST", "/api/checkout", checkout("credit-unpaid-purchase", customerID, 0, "cash"), 201))
	cash(0)
	request("DELETE", fmt.Sprintf("/api/sales/%d", unpaidID), fmt.Sprintf(`{"confirmation":"DELETE %d","reason":"Cancelled unpaid sale"}`, unpaidID), 200)
	if scalar(`SELECT outstanding FROM credit_sale_balances WHERE id=$1`, unpaidID) != 0 {
		t.Fatal("deleted unpaid sale still contributes debt")
	}
	// Card deposits are recorded as collections and never increase cash in the till.
	cardID := idFrom(request("POST", "/api/checkout", checkout("credit-card-deposit", customerID, 1500, "card"), 201))
	cash(0)
	if scalar(`SELECT outstanding FROM credit_sale_balances WHERE id=$1`, cardID) != 8500 {
		t.Fatal("card deposit balance")
	}
	request("POST", "/api/session/close", fmt.Sprintf(`{"sessionId":%d,"closingCash":0}`, sessionID), 200)
	request("POST", path, payment("credit-payment-closed-till", 1, "cash", sessionID), 409)
	originalSession := sessionID
	sessionID = idFrom(request("POST", "/api/session/open", `{"openingCash":500}`, 201))
	settlement := fmt.Sprintf(`{"requestId":"credit-final-settlement","saleId":%d,"sessionId":%d,"amount":8500,"payment":"cash"}`, cardID, sessionID)
	request("POST", path, settlement, 201)
	request("POST", path, settlement, 200)
	cash(9000)
	if scalar(`SELECT outstanding FROM credit_sale_balances WHERE id=$1`, cardID) != 0 || scalar(`SELECT closing_expected_cash FROM register_sessions WHERE id=$1`, originalSession) != 0 {
		t.Fatal("later payment must settle the balance without changing the earlier closed register")
	}
	request("POST", "/api/session/close", fmt.Sprintf(`{"sessionId":%d,"closingCash":9000}`, sessionID), 200)
	request("PUT", "/api/settings", `{"currency":"LKR"}`, 200)
	sessionID = idFrom(request("POST", "/api/session/open", `{"openingCash":0}`, 201))
	req := fmt.Sprintf(`{"requestId":"credit-wrong-currency","saleId":%d,"sessionId":%d,"amount":100,"payment":"cash"}`, cardID, sessionID)
	request("POST", path, req, 409)
	request("GET", customerPath, "", 200)
	request("GET", "/api/audit", "", 200)
}
