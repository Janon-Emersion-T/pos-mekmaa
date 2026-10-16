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

func TestCapitalIdempotencyPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("capital_idempotency_%d", time.Now().UnixNano())
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

	var adminID, accountID int64
	var currency string

	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		UPDATE financial_accounts SET balance=0
		WHERE account_type='bank' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&accountID); err != nil {
		t.Fatal(err)
	}

	admin := &User{
		ID: adminID, Role: "superadmin",
		Email: "bootstrap@example.test", Active: true,
	}
	s := &Server{db: db}
	handler := s.capitalRoutesTestHandler()

	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			http.MethodPost, "/api/capital", strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, admin))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	check := func(t *testing.T, entries, movements int64, balance int64) {
		t.Helper()
		var gotEntries, gotMovements, gotBalance int64

		if err := db.QueryRow(
			`SELECT count(*) FROM capital_entries`,
		).Scan(&gotEntries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`
			SELECT count(*) FROM financial_ledger_entries
			WHERE reference_type='capital_entry'
			  AND entry_type='capital_contribution'
			  AND account_id=$1
		`, accountID).Scan(&gotMovements); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(
			`SELECT balance FROM financial_accounts WHERE id=$1`,
			accountID,
		).Scan(&gotBalance); err != nil {
			t.Fatal(err)
		}

		if gotEntries != entries || gotMovements != movements || gotBalance != balance {
			t.Fatalf(
				"entries=%d movements=%d balance=%d; expected %d %d %d",
				gotEntries, gotMovements, gotBalance,
				entries, movements, balance,
			)
		}
	}

	body := fmt.Sprintf(
		`{"requestId":"capital-idempotency-first-0001","entryType":"contribution","amount":12500,"fundingAccountId":%d}`,
		accountID,
	)

	t.Run("first submission and identical replay", func(t *testing.T) {
		first := call(body)
		if first.Code != http.StatusCreated {
			t.Fatalf("first: %d %s", first.Code, first.Body.String())
		}

		replay := call(body)
		if replay.Code != http.StatusOK {
			t.Fatalf("replay: %d %s", replay.Code, replay.Body.String())
		}

		var firstResult, replayResult struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(replay.Body.Bytes(), &replayResult); err != nil {
			t.Fatal(err)
		}
		if firstResult.ID <= 0 || firstResult.ID != replayResult.ID {
			t.Fatalf("replay returned different ID: %d vs %d", firstResult.ID, replayResult.ID)
		}

		check(t, 1, 1, 12500)
	})

	t.Run("changed payload with reused request ID", func(t *testing.T) {
		changed := strings.Replace(body, `"amount":12500`, `"amount":25000`, 1)
		w := call(changed)
		if w.Code != http.StatusConflict {
			t.Fatalf("changed payload: %d %s", w.Code, w.Body.String())
		}
		check(t, 1, 1, 12500)
	})

	t.Run("concurrent identical submissions", func(t *testing.T) {
		concurrentBody := fmt.Sprintf(
			`{"requestId":"capital-idempotency-concurrent-0002","entryType":"contribution","amount":7500,"fundingAccountId":%d}`,
			accountID,
		)

		const workers = 8
		var wg sync.WaitGroup
		statuses := make(chan int, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses <- call(concurrentBody).Code
			}()
		}

		wg.Wait()
		close(statuses)

		created, replayed := 0, 0
		for status := range statuses {
			switch status {
			case http.StatusCreated:
				created++
			case http.StatusOK:
				replayed++
			default:
				t.Errorf("unexpected concurrent response: %d", status)
			}
		}

		if created != 1 || replayed != workers-1 {
			t.Fatalf("created=%d replayed=%d", created, replayed)
		}
		check(t, 2, 2, 20000)
	})
}
