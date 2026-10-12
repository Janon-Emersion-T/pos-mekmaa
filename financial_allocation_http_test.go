package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRegisterCashAllocationHTTPAuthorizationPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("allocation_http_test_%d", time.Now().UnixNano())

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("cleanup test schema: %v", err)
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

	var adminID int64
	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	var cashierID int64
	if err := db.QueryRow(`
		INSERT INTO users(email,password_hash,role,active)
		VALUES('allocation-http-cashier@example.invalid','unused','cashier',true)
		RETURNING id
	`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}

	var currency string
	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var sourceID int64
	if err := db.QueryRow(`
		INSERT INTO financial_accounts(account_type,currency,balance)
		VALUES('central_cash',$1,500000)
		RETURNING id
	`, currency).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}

	s := &Server{db: db}
	mux := http.NewServeMux()
	s.ownerControlRoutes(mux)

	admin := &User{
		ID:    adminID,
		Email: "admin@example.invalid",
		Role:  "superadmin",
	}
	cashier := &User{
		ID:    cashierID,
		Email: "allocation-http-cashier@example.invalid",
		Role:  "cashier",
	}

	send := func(actor *User, body string) *httptest.ResponseRecorder {
		t.Helper()

		r := httptest.NewRequest(
			http.MethodPost,
			"/api/register-cash-allocations",
			strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(
			r.Context(), userContextKey, actor,
		))

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	countAllocations := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM register_cash_allocations
		`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	carryForward := fmt.Sprintf(
		`{"userId":%d,"amount":100000,"fundingType":"carry_forward","fundingAccountId":null}`,
		cashierID,
	)

	t.Run("carry-forward rejected", func(t *testing.T) {
		before := countAllocations()
		w := send(admin, carryForward)

		if w.Code != http.StatusConflict {
			t.Fatalf("got %d want 409: %s", w.Code, w.Body.String())
		}
		if countAllocations() != before {
			t.Fatal("rejected carry-forward created an allocation")
		}
	})

	newFunds := fmt.Sprintf(
		`{"userId":%d,"amount":100000,"fundingType":"new_funds","fundingAccountId":%d}`,
		cashierID, sourceID,
	)

	t.Run("cashier forbidden", func(t *testing.T) {
		before := countAllocations()
		w := send(cashier, newFunds)

		if w.Code != http.StatusForbidden {
			t.Fatalf("got %d want 403: %s", w.Code, w.Body.String())
		}
		if countAllocations() != before {
			t.Fatal("unauthorized request created an allocation")
		}
	})

	t.Run("insufficient funds rejected", func(t *testing.T) {
		before := countAllocations()

		body := fmt.Sprintf(
			`{"userId":%d,"amount":600000,"fundingType":"new_funds","fundingAccountId":%d}`,
			cashierID, sourceID,
		)

		w := send(admin, body)
		if w.Code != http.StatusConflict {
			t.Fatalf("got %d want 409: %s", w.Code, w.Body.String())
		}
		if countAllocations() != before {
			t.Fatal("insufficient funding created an allocation")
		}
	})

	t.Run("valid new funding accepted", func(t *testing.T) {
		before := countAllocations()
		w := send(admin, newFunds)

		if w.Code != http.StatusCreated {
			t.Fatalf("got %d want 201: %s", w.Code, w.Body.String())
		}
		if countAllocations() != before+1 {
			t.Fatal("valid funding did not create exactly one allocation")
		}

		var fundingType string
		var fundingAccountID int64
		if err := db.QueryRow(`
			SELECT funding_type,funding_account_id
			FROM register_cash_allocations
			WHERE user_id=$1 AND status='pending'
		`, cashierID).Scan(&fundingType, &fundingAccountID); err != nil {
			t.Fatal(err)
		}

		if fundingType != "new_funds" || fundingAccountID != sourceID {
			t.Fatal("saved allocation funding details do not match request")
		}
	})
}
