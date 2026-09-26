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

func TestSessionAttributionAndConcurrentClose(t *testing.T) {
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
	schema := fmt.Sprintf("session_test_%d", time.Now().UnixNano())
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
	s := &Server{db: db}
	var openerID, cashierID, productID int64
	if err = db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES('opener@example.com','unused','admin') RETURNING id`).Scan(&openerID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES('cashier@example.com','unused','cashier') RETURNING id`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`INSERT INTO products(name,category,price,stock) VALUES('Test','Test',450,100) RETURNING id`).Scan(&productID); err != nil {
		t.Fatal(err)
	}
	opener := &User{ID: openerID, Email: "opener@example.com", Role: "admin"}
	cashier := &User{ID: cashierID, Email: "cashier@example.com", Role: "cashier"}
	call := func(fn http.HandlerFunc, actor *User, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, actor))
		w := httptest.NewRecorder()
		fn(w, r)
		return w
	}
	// Each attempted sale needs its own key so idempotency cannot replay an earlier sale.
	order := func(requestID string) string {
		return fmt.Sprintf(`{"requestId":%q,"items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":450,"expectedTotal":450}`, requestID, productID)
	}
	if w := call(s.checkout, cashier, "POST", "/api/checkout", order("session-not-open-checkout")); w.Code != 409 {
		t.Fatalf("sale without session: %d", w.Code)
	}
	open := func(actor *User) int64 {
		t.Helper()
		w := call(s.sessions, actor, "POST", "/api/session/open", `{"openingCash":1000}`)
		if w.Code != 201 {
			t.Fatalf("open: %d %s", w.Code, w.Body.String())
		}
		var result struct{ ID int64 }
		json.Unmarshal(w.Body.Bytes(), &result)
		return result.ID
	}
	foreignID := open(opener)
	assertStatus := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	assertStatus(call(s.sessions, opener, "POST", "/api/session/open", `{"openingCash":0}`), 409)
	own := call(s.sessions, cashier, "GET", "/api/session", "")
	assertStatus(own, 200)
	if strings.TrimSpace(own.Body.String()) != "null" {
		t.Fatalf("cashier can see another user's active register: %s", own.Body.String())
	}
	assertStatus(call(s.checkout, cashier, "POST", "/api/checkout", order("session-other-user-open")), 409)
	assertStatus(call(s.checkout, cashier, "POST", "/api/checkout", strings.TrimSuffix(order("session-other-id-attempt"), "}")+fmt.Sprintf(`,"sessionId":%d}`, foreignID)), 409)
	assertStatus(call(s.sessions, cashier, "POST", "/api/session/close", fmt.Sprintf(`{"closingCash":1000,"sessionId":%d}`, foreignID)), 409)
	assertStatus(call(s.pettyCash, cashier, "POST", "/api/petty-cash", `{"direction":"in","amount":100,"category":"Test"}`), 409)
	sessionID := open(cashier)
	assertStatus(call(s.sessions, cashier, "POST", "/api/session/open", `{"openingCash":0}`), 409)
	assertStatus(call(s.sessions, cashier, "POST", "/api/session/close", fmt.Sprintf(`{"closingCash":1000,"sessionId":%d}`, foreignID)), 409)
	assertStatus(call(s.checkout, cashier, "POST", "/api/checkout", strings.TrimSuffix(order("session-wrong-id-with-own"), "}")+fmt.Sprintf(`,"sessionId":%d}`, foreignID)), 409)
	own = call(s.sessions, cashier, "GET", "/api/session", "")
	var current Session
	if err = json.Unmarshal(own.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.ID != sessionID || current.OpenedBy != cashierID {
		t.Fatalf("wrong active session: %+v", current)
	}
	w := call(s.checkout, cashier, "POST", "/api/checkout", order("session-initial-checkout"))
	if w.Code != 201 {
		t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
	}
	var sale Sale
	if err = json.Unmarshal(w.Body.Bytes(), &sale); err != nil {
		t.Fatal(err)
	}
	if sale.CreatedBy != cashierID || sale.CashierEmail != cashier.Email || sale.SessionID != sessionID {
		t.Fatalf("wrong attribution: %+v", sale)
	}
	if _, err = db.Exec(`UPDATE users SET email='renamed@example.com' WHERE id=$1`, cashierID); err != nil {
		t.Fatal(err)
	}
	var sales []Sale
	w = call(s.sales, opener, "GET", fmt.Sprintf("/api/sales?sessionId=%d", sessionID), "")
	if err = json.Unmarshal(w.Body.Bytes(), &sales); err != nil {
		t.Fatal(err)
	}
	if len(sales) != 1 || sales[0].CashierEmail != "cashier@example.com" {
		t.Fatalf("cashier snapshot changed: %+v", sales)
	}
	w = call(s.sessions, cashier, "POST", "/api/session/close", fmt.Sprintf(`{"closingCash":1450,"sessionId":%d}`, sessionID))
	if w.Code != 200 {
		t.Fatalf("close: %s", w.Body.String())
	}
	w = call(s.checkout, cashier, "POST", "/api/checkout", order("session-closed-checkout"))
	if w.Code != 409 {
		t.Fatalf("sale on closed register: %d", w.Code)
	}
	var history []Session
	w = call(s.sessionHistory, opener, "GET", "/api/sessions", "")
	if err = json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].OpenedBy != cashierID || history[0].ClosedBy != cashierID || history[0].ClosingExpectedCash == nil || *history[0].ClosingExpectedCash != 1450 || history[0].SaleCount != 1 {
		t.Fatalf("history: %+v", history)
	}
	for i := 0; i < 5; i++ {
		id := open(cashier)
		stale := strings.TrimSuffix(order(fmt.Sprintf("session-stale-checkout-%d", i)), "}") + fmt.Sprintf(`,"sessionId":%d}`, sessionID)
		if w = call(s.checkout, cashier, "POST", "/api/checkout", stale); w.Code != 409 {
			t.Fatalf("stale session accepted: %d", w.Code)
		}
		raceOrder := order(fmt.Sprintf("session-race-checkout-%d", i))
		start := make(chan struct{})
		var wg sync.WaitGroup
		var saleResult, closeResult *httptest.ResponseRecorder
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			saleResult = call(s.checkout, cashier, "POST", "/api/checkout", raceOrder)
		}()
		go func() {
			defer wg.Done()
			<-start
			closeResult = call(s.sessions, cashier, "POST", "/api/session/close", fmt.Sprintf(`{"closingCash":1450,"sessionId":%d}`, id))
		}()
		close(start)
		wg.Wait()
		if closeResult.Code != 200 || (saleResult.Code != 201 && saleResult.Code != 409) {
			t.Fatalf("race: close=%d sale=%d", closeResult.Code, saleResult.Code)
		}
		var expected, total int
		if err = db.QueryRow(`SELECT closing_expected_cash,COALESCE((SELECT SUM(total) FROM sales WHERE session_id=r.id),0) FROM register_sessions r WHERE id=$1`, id).Scan(&expected, &total); err != nil {
			t.Fatal(err)
		}
		if expected != 1000+total {
			t.Fatalf("sale omitted from close: expected=%d total=%d", expected, total)
		}
	}
	// The other staff member's session remains open and has independent totals.
	own = call(s.sessions, opener, "GET", "/api/session", "")
	if err = json.Unmarshal(own.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.ID != foreignID || current.ExpectedCash != 1000 {
		t.Fatalf("other user's register changed: %+v", current)
	}
	assertStatus(call(s.checkout, opener, "POST", "/api/checkout", order("session-opener-own-sale")), 201)
	own = call(s.sessions, opener, "GET", "/api/session", "")
	if err = json.Unmarshal(own.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.ExpectedCash != 1450 {
		t.Fatalf("opener cash=%d", current.ExpectedCash)
	}
	assertStatus(call(s.updateSettings, opener, "PUT", "/api/settings", `{"currency":"LKR"}`), 409)
	// Completed retries may recover a receipt after closing, but do not create a new sale.
	assertStatus(call(s.checkout, cashier, "POST", "/api/checkout", order("session-initial-checkout")), 200)
	assertStatus(call(s.sessions, opener, "POST", "/api/session/close", fmt.Sprintf(`{"closingCash":1450,"sessionId":%d}`, foreignID)), 200)
	// Simultaneous attempts by the same user must open exactly one session.
	var opened [2]*httptest.ResponseRecorder
	var openWG sync.WaitGroup
	for i := range opened {
		openWG.Add(1)
		go func(i int) {
			defer openWG.Done()
			opened[i] = call(s.sessions, cashier, "POST", "/api/session/open", `{"openingCash":0}`)
		}(i)
	}
	openWG.Wait()
	if !((opened[0].Code == 201 && opened[1].Code == 409) || (opened[1].Code == 201 && opened[0].Code == 409)) {
		t.Fatalf("duplicate session opens: %d/%d", opened[0].Code, opened[1].Code)
	}

}
