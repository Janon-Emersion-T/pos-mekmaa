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
	"testing"
	"time"
)

func TestFinancialCashRefundHTTPPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("cash_refund_test_%d", time.Now().UnixNano())
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
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

	var adminID, productID int64
	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		INSERT INTO products(name,category,price,stock)
		VALUES('Refund Test Item','Test',5000,20)
		RETURNING id
	`).Scan(&productID); err != nil {
		t.Fatal(err)
	}

	admin := &User{ID: adminID, Role: "superadmin"}

	s := &Server{db: db}
	call := func(handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		if handlerPath := strings.TrimPrefix(path, "/api/sales/"); handlerPath != path {
			var saleID int64
			if _, err := fmt.Sscanf(handlerPath, "%d/refunds", &saleID); err != nil {
				t.Fatalf("invalid refund test path %q: %v", path, err)
			}
			r.SetPathValue("id", fmt.Sprint(saleID))
		}
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, admin))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	check := func(handler http.HandlerFunc, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := call(handler, path, body)
		if w.Code != want {
			t.Fatalf("%s: status=%d want=%d body=%s", path, w.Code, want, w.Body.String())
		}
		return w
	}
	getID := func(w *httptest.ResponseRecorder) int64 {
		t.Helper()
		var v struct{ ID int64 }
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	balance := func() int64 {
		return scalar(`
			SELECT COALESCE(SUM(balance),0)
			FROM financial_accounts
			WHERE account_type='staff_cash' AND user_id=$1
		`, adminID)
	}

	sessionID := getID(check(s.sessions, "/api/session/open", `{"openingCash":0}`, 201))

	sale := func(key, payment string) int64 {
		t.Helper()
		body := fmt.Sprintf(
			`{"requestId":%q,"sessionId":%d,"items":[{"productId":%d,"quantity":1}],"payment":%q,"expectedTotal":5000}`,
			key, sessionID, productID, payment,
		)
		if payment == "cash" {
			body = fmt.Sprintf(
				`{"requestId":%q,"sessionId":%d,"items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":5000,"expectedTotal":5000}`,
				key, sessionID, productID,
			)
		}
		return getID(check(s.checkout, "/api/checkout", body, 201))
	}

	refund := func(saleID int64, key string) string {
		t.Helper()
		lineID := scalar(`SELECT id FROM sale_items WHERE sale_id=$1`, saleID)
		return fmt.Sprintf(
			`{"requestId":%q,"reason":"Financial refund test","confirmed":true,"items":[{"saleItemId":%d,"quantity":1,"restock":true}]}`,
			key, lineID,
		)
	}

	cashSaleID := sale("refund-cash-sale-0001", "cash")
	cardSaleID := sale("refund-card-sale-0001", "card")
	if got := balance(); got != 5000 {
		t.Fatalf("initial financial balance=%d want=5000", got)
	}

	cashPath := fmt.Sprintf("/api/sales/%d/refunds", cashSaleID)
	cashBody := refund(cashSaleID, "refund-cash-return-0001")
	check(s.refundSale, cashPath, cashBody, 201)
	check(s.refundSale, cashPath, cashBody, 200)

	if got := balance(); got != 0 {
		t.Fatalf("cash refund balance=%d want=0", got)
	}
	if got := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type='cash_refund' AND amount=-5000
	`); got != 1 {
		t.Fatalf("cash refund ledger entries=%d want=1", got)
	}

	cardPath := fmt.Sprintf("/api/sales/%d/refunds", cardSaleID)
	cardBody := refund(cardSaleID, "refund-card-return-0001")
	check(s.refundSale, cardPath, cardBody, 201)
	if got := balance(); got != 0 {
		t.Fatalf("card refund changed cash balance=%d", got)
	}
	if got := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type='cash_refund'
	`); got != 1 {
		t.Fatalf("card refund created cash ledger entry; count=%d", got)
	}

	rollbackSaleID := sale("refund-rollback-sale-0001", "cash")
	rollbackPath := fmt.Sprintf("/api/sales/%d/refunds", rollbackSaleID)
	rollbackBody := refund(rollbackSaleID, "refund-rollback-return-0001")

	beforeBalance := balance()
	beforeStock := scalar(`SELECT stock FROM products WHERE id=$1`, productID)
	beforeRefunds := scalar(`SELECT COUNT(*) FROM refunds`)

	if _, err := db.Exec(`
		CREATE FUNCTION reject_test_cash_refund() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.entry_type='cash_refund' THEN
				RAISE EXCEPTION 'simulated refund posting failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_test_cash_refund_trigger
		BEFORE INSERT ON financial_ledger_entries
		FOR EACH ROW EXECUTE FUNCTION reject_test_cash_refund();
	`); err != nil {
		t.Fatal(err)
	}

	check(s.refundSale, rollbackPath, rollbackBody, 500)

	if got := balance(); got != beforeBalance {
		t.Fatalf("rollback changed balance=%d want=%d", got, beforeBalance)
	}
	if got := scalar(`SELECT stock FROM products WHERE id=$1`, productID); got != beforeStock {
		t.Fatalf("rollback changed stock=%d want=%d", got, beforeStock)
	}
	if got := scalar(`SELECT COUNT(*) FROM refunds`); got != beforeRefunds {
		t.Fatalf("rollback persisted refund=%d want=%d", got, beforeRefunds)
	}

	if _, err := db.Exec(`DROP TRIGGER reject_test_cash_refund_trigger ON financial_ledger_entries`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP FUNCTION reject_test_cash_refund()`); err != nil {
		t.Fatal(err)
	}

	check(s.refundSale, rollbackPath, rollbackBody, 201)
	check(s.refundSale, rollbackPath, rollbackBody, 200)

	if got := balance(); got != beforeBalance-5000 {
		t.Fatalf("retry balance=%d want=%d", got, beforeBalance-5000)
	}
	if got := scalar(`SELECT COUNT(*) FROM refunds`); got != beforeRefunds+1 {
		t.Fatalf("retry refund count=%d want=%d", got, beforeRefunds+1)
	}
}
