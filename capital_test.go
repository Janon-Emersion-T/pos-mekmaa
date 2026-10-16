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

func TestCapitalAPIPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("capital_api_test_%d", time.Now().UnixNano())

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
	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	admin := &User{
		ID: adminID, Role: "superadmin",
		Email: "bootstrap@example.test", Active: true,
	}
	cashier := &User{
		ID: adminID, Role: "cashier",
		Email: "cashier@example.test", Active: true,
	}

	var currency string
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var fundingAccountID int64
	if err := db.QueryRow(`
		UPDATE financial_accounts SET balance=0
		WHERE account_type='bank' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&fundingAccountID); err != nil {
		t.Fatal(err)
	}

	s := &Server{db: db}

	call := func(actor *User, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, actor))
		w := httptest.NewRecorder()
		s.capitalRoutesTestHandler().ServeHTTP(w, r)
		return w
	}

	t.Run("cashier forbidden", func(t *testing.T) {
		w := call(cashier, "POST", "/api/capital",
			`{"requestId":"capital-capital_test-0001","entryType":"contribution","amount":1000}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid amount", func(t *testing.T) {
		w := call(admin, "POST", "/api/capital",
			`{"requestId":"capital-capital_test-0002","entryType":"contribution","amount":0}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	w := call(admin, "POST", "/api/capital",
		fmt.Sprintf(
			`{"requestId":"capital-capital_test-0003","entryType":"contribution","amount":50000,"fundingAccountId":%d,"reference":"TEST-CAPITAL"}`,
			fundingAccountID,
		))
	if w.Code != http.StatusCreated {
		t.Fatalf("contribution failed: %d %s", w.Code, w.Body.String())
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 {
		t.Fatal("missing capital entry ID")
	}

	t.Run("contribution financial posting", func(t *testing.T) {
		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, fundingAccountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != 50000 {
			t.Fatalf("balance=%d want=50000", balance)
		}

		var count int
		var amount int64
		if err := db.QueryRow(`
			SELECT COUNT(*),COALESCE(SUM(amount),0)
			FROM financial_ledger_entries
			WHERE reference_type='capital_entry'
			  AND reference_id=$1
			  AND entry_type='capital_contribution'
		`, created.ID).Scan(&count, &amount); err != nil {
			t.Fatal(err)
		}
		if count != 1 || amount != 50000 {
			t.Fatalf("ledger count=%d amount=%d", count, amount)
		}
	})

	t.Run("audit record", func(t *testing.T) {
		var count int
		err := db.QueryRow(`
			SELECT COUNT(*) FROM audit_events
			WHERE entity='capital_entries'
			  AND entity_id=$1
			  AND actor_id=$2
		`, created.ID, adminID).Scan(&count)
		if err != nil || count != 1 {
			t.Fatalf("audit verification failed: count=%d err=%v", count, err)
		}
	})

	t.Run("list capital", func(t *testing.T) {
		w := call(admin, "GET", "/api/capital", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var entries []CapitalEntry
		if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Amount != 50000 {
			t.Fatalf("unexpected entries: %+v", entries)
		}
	})

	t.Run("withdrawal exceeds account balance", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"requestId":"capital-capital_test-0004","entryType":"withdrawal","amount":60000,"fundingAccountId":%d}`,
			fundingAccountID,
		)
		w := call(admin, "POST", "/api/capital", body)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("valid withdrawal", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"requestId":"capital-capital_test-0005","entryType":"withdrawal","amount":10000,"fundingAccountId":%d}`,
			fundingAccountID,
		)
		w := call(admin, "POST", "/api/capital", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, fundingAccountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != 40000 {
			t.Fatalf("balance=%d want=40000", balance)
		}

		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM financial_ledger_entries
			WHERE entry_type='capital_withdrawal'
			  AND account_id=$1 AND amount=-10000
		`, fundingAccountID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected one withdrawal ledger entry, got %d", count)
		}
	})

	t.Run("reinvestment does not deposit cash", func(t *testing.T) {
		w := call(admin, "POST", "/api/capital",
			`{"requestId":"capital-capital_test-0006","entryType":"reinvestment","amount":5000}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, fundingAccountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != 40000 {
			t.Fatalf("reinvestment changed cash balance to %d", balance)
		}
	})

	path := fmt.Sprintf("/api/capital/%d/correct", created.ID)

	t.Run("insufficient funds correction rejected", func(t *testing.T) {
		w := call(admin, "POST", path,
			`{"notes":"Test insufficient funds correction"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}

		var corrections int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM capital_entries
			WHERE corrected_entry_id=$1
		`, created.ID).Scan(&corrections); err != nil {
			t.Fatal(err)
		}
		if corrections != 0 {
			t.Fatalf("rejected correction created %d entries", corrections)
		}

		var reversals int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM financial_ledger_entries
			WHERE entry_type='capital_correction'
		`).Scan(&reversals); err != nil {
			t.Fatal(err)
		}
		if reversals != 0 {
			t.Fatalf("rejected correction posted %d ledger entries", reversals)
		}
	})

	t.Run("restore sufficient funds", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"requestId":"capital-capital_test-0007","entryType":"contribution","amount":10000,"fundingAccountId":%d,"reference":"TEST-RESTORE"}`,
			fundingAccountID,
		)
		w := call(admin, "POST", "/api/capital", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, fundingAccountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != 50000 {
			t.Fatalf("balance=%d want=50000", balance)
		}
	})

	t.Run("concurrent correction", func(t *testing.T) {
		const requests = 2
		var wg sync.WaitGroup
		statuses := make(chan int, requests)

		for i := 0; i < requests; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := call(admin, "POST", path,
					`{"notes":"Correct test contribution"}`)
				statuses <- w.Code
			}()
		}
		wg.Wait()
		close(statuses)

		createdCount := 0
		conflictCount := 0
		for status := range statuses {
			switch status {
			case http.StatusCreated:
				createdCount++
			case http.StatusConflict:
				conflictCount++
			default:
				t.Errorf("unexpected correction status: %d", status)
			}
		}
		if createdCount != 1 || conflictCount != 1 {
			t.Errorf("expected one 201 and one 409; got %d and %d",
				createdCount, conflictCount)
		}

		var corrections int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM capital_entries
			WHERE corrected_entry_id=$1
		`, created.ID).Scan(&corrections); err != nil {
			t.Fatal(err)
		}
		if corrections != 1 {
			t.Fatalf("expected exactly one correction, got %d", corrections)
		}

		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, fundingAccountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if balance != 0 {
			t.Fatalf("balance after correction=%d want=0", balance)
		}

		var reversalCount int
		var reversalAmount int64
		if err := db.QueryRow(`
			SELECT COUNT(*),COALESCE(SUM(amount),0)
			FROM financial_ledger_entries
			WHERE entry_type='capital_correction'
			  AND account_id=$1
		`, fundingAccountID).Scan(
			&reversalCount, &reversalAmount,
		); err != nil {
			t.Fatal(err)
		}
		if reversalCount != 1 || reversalAmount != -50000 {
			t.Fatalf(
				"correction ledger count=%d amount=%d",
				reversalCount, reversalAmount,
			)
		}
	})
}

func (s *Server) capitalRoutesTestHandler() http.Handler {
	mux := http.NewServeMux()
	s.capitalRoutes(mux)
	return mux
}
