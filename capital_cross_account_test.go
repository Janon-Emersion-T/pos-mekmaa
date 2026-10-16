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
	"sync"
	"testing"
	"time"
)

func TestCapitalCrossAccountConcurrencyPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("capital_cross_%d", time.Now().UnixNano())

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

	var adminID int64
	var currency string

	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var bankID, cashID int64

	if err := db.QueryRow(`
		UPDATE financial_accounts SET balance=0
		WHERE account_type='bank' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&bankID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		UPDATE financial_accounts SET balance=0
		WHERE account_type='central_cash' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&cashID); err != nil {
		t.Fatal(err)
	}

	admin := &User{
		ID:     adminID,
		Role:   "superadmin",
		Email:  "bootstrap@example.test",
		Active: true,
	}

	s := &Server{db: db}
	handler := s.capitalRoutesTestHandler()

	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			http.MethodPost,
			"/api/capital",
			strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(
			context.WithValue(r.Context(), userContextKey, admin),
		)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	// Total owner investment: 10,000.
	deposit := fmt.Sprintf(
		`{"requestId":"capital-capital_cross_account_test-0001","entryType":"contribution","amount":10000,"fundingAccountId":%d}`,
		bankID,
	)

	if w := call(deposit); w.Code != http.StatusCreated {
		t.Fatalf("contribution failed: %d %s", w.Code, w.Body.String())
	}

	// Move 7,000 between accounts. This is an internal transfer,
	// not a new contribution or withdrawal.
	if _, err := db.Exec(`
		UPDATE financial_accounts
		SET balance=balance-7000
		WHERE id=$1
	`, bankID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
		UPDATE financial_accounts
		SET balance=balance+7000
		WHERE id=$1
	`, cashID); err != nil {
		t.Fatal(err)
	}

	// Add non-capital cash to the bank, representing other receipts.
	// This creates sufficient account balances for both requests,
	// while aggregate invested capital remains only 10,000.
	if _, err := db.Exec(`
		UPDATE financial_accounts
		SET balance=balance+7000
		WHERE id=$1
	`, bankID); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	statuses := make(chan int, 2)

	for _, accountID := range []int64{bankID, cashID} {
		wg.Add(1)

		go func(id int64) {
			defer wg.Done()

			body := fmt.Sprintf(
				`{"requestId":"capital-cross-withdraw-%d","entryType":"withdrawal","amount":7000,"fundingAccountId":%d}`,
				id, id,
			)

			statuses <- call(body).Code
		}(accountID)
	}

	wg.Wait()
	close(statuses)

	successes, conflicts := 0, 0

	for status := range statuses {
		switch status {
		case http.StatusCreated:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("unexpected status: %d", status)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf(
			"expected one success and one conflict; got %d successes and %d conflicts",
			successes, conflicts,
		)
	}

	var remainingCapital int64
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(amount),0)
		FROM capital_entries
		WHERE entry_type IN ('contribution','withdrawal')
	`).Scan(&remainingCapital); err != nil {
		t.Fatal(err)
	}

	if remainingCapital != 3000 {
		t.Fatalf(
			"remaining capital=%d want=3000",
			remainingCapital,
		)
	}

	var withdrawalCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM capital_entries
		WHERE entry_type='withdrawal'
	`).Scan(&withdrawalCount); err != nil {
		t.Fatal(err)
	}

	if withdrawalCount != 1 {
		t.Fatalf("withdrawals=%d want=1", withdrawalCount)
	}

	t.Log("PASS: Cross-account capital withdrawals are serialized")
}
