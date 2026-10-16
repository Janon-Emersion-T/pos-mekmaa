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

func TestCapitalFinancialIntegrityPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("capital_integrity_%d", time.Now().UnixNano())

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
		WHERE role='superadmin' AND active LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var accountID int64
	if err := db.QueryRow(`
		UPDATE financial_accounts SET balance=0
		WHERE account_type='bank' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&accountID); err != nil {
		t.Fatal(err)
	}

	admin := &User{
		ID:     adminID,
		Role:   "superadmin",
		Email:  "bootstrap@example.test",
		Active: true,
	}

	s := &Server{db: db}

	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			http.MethodPost, "/api/capital", strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(
			context.WithValue(r.Context(), userContextKey, admin),
		)
		w := httptest.NewRecorder()
		s.capitalRoutesTestHandler().ServeHTTP(w, r)
		return w
	}

	balance := func() int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, accountID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	t.Run("ledger failure rolls back contribution", func(t *testing.T) {
		_, err := db.Exec(`
			CREATE FUNCTION reject_capital_ledger_test()
			RETURNS trigger AS $$
			BEGIN
				IF NEW.entry_type='capital_contribution'
				   AND NEW.remarks='FORCED-ROLLBACK' THEN
					RAISE EXCEPTION 'Simulated capital ledger failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;

			CREATE TRIGGER reject_capital_ledger_test_trigger
			BEFORE INSERT ON financial_ledger_entries
			FOR EACH ROW
			EXECUTE FUNCTION reject_capital_ledger_test();
		`)
		if err != nil {
			t.Fatal(err)
		}

		body := fmt.Sprintf(
			`{"requestId":"capital-capital_integrity_test-0001","entryType":"contribution","amount":10000,"fundingAccountId":%d,"notes":"FORCED-ROLLBACK"}`,
			accountID,
		)

		w := call(body)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
		}

		if got := balance(); got != 0 {
			t.Fatalf("failed posting changed balance to %d", got)
		}

		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM capital_entries
			WHERE notes='FORCED-ROLLBACK'
		`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed posting left %d capital entries", count)
		}

		if _, err := db.Exec(`
			DROP TRIGGER reject_capital_ledger_test_trigger
			ON financial_ledger_entries;
			DROP FUNCTION reject_capital_ledger_test();
		`); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("concurrent withdrawals cannot overdraw", func(t *testing.T) {
		deposit := fmt.Sprintf(
			`{"requestId":"capital-capital_integrity_test-0002","entryType":"contribution","amount":10000,"fundingAccountId":%d}`,
			accountID,
		)

		if w := call(deposit); w.Code != http.StatusCreated {
			t.Fatalf("deposit failed: %d %s", w.Code, w.Body.String())
		}

		withdraw := func(worker int) string {
			return fmt.Sprintf(
				`{"requestId":"capital-integrity-withdraw-%04d","entryType":"withdrawal","amount":7000,"fundingAccountId":%d}`,
				worker, accountID,
			)
		}

		const workers = 2
		statuses := make(chan int, workers)
		var wg sync.WaitGroup

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				statuses <- call(withdraw(worker)).Code
			}(i)
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
				t.Errorf("unexpected withdrawal status %d", status)
			}
		}

		if successes != 1 || conflicts != 1 {
			t.Fatalf(
				"expected one success and one conflict; got %d and %d",
				successes, conflicts,
			)
		}

		if got := balance(); got != 3000 {
			t.Fatalf("final balance=%d want=3000", got)
		}

		var withdrawals int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM capital_entries
			WHERE entry_type='withdrawal' AND amount=-7000
		`).Scan(&withdrawals); err != nil {
			t.Fatal(err)
		}
		if withdrawals != 1 {
			t.Fatalf("withdrawal entries=%d want=1", withdrawals)
		}
	})
}
