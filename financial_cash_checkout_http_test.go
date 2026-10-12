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

func TestCashCheckoutFinancialLedgerPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("cash_checkout_test_%d", time.Now().UnixNano())
	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	}()

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

	var cashierID, productID int64

	if err := db.QueryRow(`
		INSERT INTO users(email,password_hash,role,active)
		VALUES('ledger-cashier@example.invalid','unused','cashier',true)
		RETURNING id
	`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		INSERT INTO products(name,category,price,stock)
		VALUES('Ledger Test Product','Test',150000,10)
		RETURNING id
	`).Scan(&productID); err != nil {
		t.Fatal(err)
	}

	var currency string
	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var adminID int64
	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	// Legacy allocation keeps the fixture independent of new funding.
	if _, err := db.Exec(`
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,assigned_by,assigned_by_email,note
		)
		VALUES($1,0,$2,$3,'admin@example.invalid','ledger test')
	`, cashierID, currency, adminID); err != nil {
		t.Fatal(err)
	}

	cashier := &User{
		ID:    cashierID,
		Email: "ledger-cashier@example.invalid",
		Role:  "cashier",
	}
	s := &Server{db: db}

	call := func(handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(
			http.MethodPost, path, strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(
			r.Context(), userContextKey, cashier,
		))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}

	open := call(s.sessions, "/api/session/open", `{"openingCash":0}`)
	if open.Code != 201 {
		t.Fatalf("open session: %d %s", open.Code, open.Body.String())
	}

	var session struct{ ID int64 }
	if err := json.Unmarshal(open.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	order := func(key, payment string, tendered int, expected int) string {
		if payment == "cash" {
			return fmt.Sprintf(
				`{"requestId":%q,"sessionId":%d,"items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":%d,"expectedTotal":%d}`,
				key, session.ID, productID, tendered, expected,
			)
		}
		return fmt.Sprintf(
			`{"requestId":%q,"sessionId":%d,"items":[{"productId":%d,"quantity":1}],"payment":%q,"expectedTotal":%d}`,
			key, session.ID, productID, payment, expected,
		)
	}

	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	balance := func() int64 {
		return scalar(`
			SELECT COALESCE(SUM(balance),0)
			FROM financial_accounts
			WHERE account_type='staff_cash'
			  AND user_id=$1 AND currency=$2
		`, cashierID, currency)
	}

	stock := func() int64 {
		return scalar(`SELECT stock FROM products WHERE id=$1`, productID)
	}

	cashOrder := order("ledger-cash-sale-1", "cash", 200000, 150000)

	t.Run("cash sale posts net amount after change", func(t *testing.T) {
		w := call(s.checkout, "/api/checkout", cashOrder)
		if w.Code != 201 {
			t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
		}

		var sale Sale
		if err := json.Unmarshal(w.Body.Bytes(), &sale); err != nil {
			t.Fatal(err)
		}

		if sale.Total != 150000 ||
			sale.ChangeDue == nil ||
			*sale.ChangeDue != 50000 {
			t.Fatalf("incorrect sale totals: %+v", sale)
		}

		if got := balance(); got != 150000 {
			t.Fatalf("cashier balance=%d want=150000", got)
		}
		if got := stock(); got != 9 {
			t.Fatalf("stock=%d want=9", got)
		}

		if got := scalar(`
			SELECT COUNT(*)
			FROM financial_ledger_entries
			WHERE entry_type='cash_sale'
			  AND reference_type='sale'
			  AND reference_id=$1
		`, sale.ID); got != 1 {
			t.Fatalf("cash sale ledger entries=%d want=1", got)
		}
	})

	t.Run("replayed checkout does not post twice", func(t *testing.T) {
		w := call(s.checkout, "/api/checkout", cashOrder)
		if w.Code != 200 {
			t.Fatalf("replay: %d %s", w.Code, w.Body.String())
		}
		if got := balance(); got != 150000 {
			t.Fatalf("balance after replay=%d want=150000", got)
		}
		if got := stock(); got != 9 {
			t.Fatalf("stock after replay=%d want=9", got)
		}
		if got := scalar(`
			SELECT COUNT(*)
			FROM financial_ledger_entries
			WHERE entry_type='cash_sale'
		`); got != 1 {
			t.Fatalf("ledger entries after replay=%d want=1", got)
		}
	})

	t.Run("card sale does not credit physical cash", func(t *testing.T) {
		w := call(s.checkout, "/api/checkout",
			order("ledger-card-sale-1", "card", 0, 150000),
		)
		if w.Code != 201 {
			t.Fatalf("card sale: %d %s", w.Code, w.Body.String())
		}
		if got := balance(); got != 150000 {
			t.Fatalf("cashier balance after card sale=%d", got)
		}
		if got := stock(); got != 8 {
			t.Fatalf("stock after card sale=%d want=8", got)
		}
	})

	t.Run("rejected sale does not change cash or stock", func(t *testing.T) {
		w := call(s.checkout, "/api/checkout",
			order("ledger-invalid-sale-1", "cash", 100000, 150000),
		)
		if w.Code != 400 {
			t.Fatalf("invalid sale: %d %s", w.Code, w.Body.String())
		}
		if got := balance(); got != 150000 {
			t.Fatalf("cashier balance after rejection=%d", got)
		}
		if got := stock(); got != 8 {
			t.Fatalf("stock after rejection=%d want=8", got)
		}
	})
}
