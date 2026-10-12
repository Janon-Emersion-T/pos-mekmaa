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
	"testing"
	"time"
)

func TestFinancialFundingSourcesPostgres(t *testing.T) {
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

	schema := fmt.Sprintf(
		"funding_sources_test_%d",
		time.Now().UnixNano(),
	)

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec(
			"DROP SCHEMA " + schema + " CASCADE",
		); err != nil {
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
		INSERT INTO users(
			email,password_hash,role,active
		)
		VALUES('funding-test-cashier@example.invalid',
		       'unused','cashier',true)
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

	for _, a := range []struct {
		accountType string
		owner       any
		balance     int64
	}{
		{"central_cash", nil, 5000},
		{"superadmin_cash", adminID, 2500},
		{"staff_cash", cashierID, 1800},
		{"bank", nil, 10000},
	} {
		if _, err := db.Exec(`
			INSERT INTO financial_accounts(
				user_id,account_type,currency,balance
			)
			VALUES($1,$2,$3,$4)
		`, a.owner, a.accountType, currency, a.balance); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{db: db}

	call := func(user *User) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			http.MethodGet,
			"/api/financial/funding-sources",
			nil,
		)
		r = r.WithContext(context.WithValue(
			r.Context(), userContextKey, user,
		))

		w := httptest.NewRecorder()
		s.financialFundingSources(w, r)
		return w
	}

	admin := &User{
		ID: adminID, Role: "superadmin",
	}
	w := call(admin)

	if w.Code != 200 {
		t.Fatalf("admin response: %d %s", w.Code, w.Body.String())
	}

	var accounts []FinancialAccount
	if err := json.Unmarshal(w.Body.Bytes(), &accounts); err != nil {
		t.Fatal(err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 funding sources, got %d", len(accounts))
	}

	found := map[string]int64{}
	for _, a := range accounts {
		found[a.AccountType] = a.Balance
	}

	if found["central_cash"] != 5000 ||
		found["superadmin_cash"] != 2500 {
		t.Fatalf("incorrect authorized accounts: %+v", found)
	}

	cashier := &User{
		ID: cashierID, Role: "cashier",
	}
	if w := call(cashier); w.Code != 403 {
		t.Fatalf("cashier access: expected 403, got %d", w.Code)
	}
}
