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

func TestPettyCashFinancialHTTPPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("petty_finance_test_%d", time.Now().UnixNano())
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

	var cashierID, adminID int64
	if err := db.QueryRow(`
		INSERT INTO users(email,password_hash,role,active)
		VALUES('petty-cashier@example.invalid','unused','cashier',true)
		RETURNING id
	`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`
		SELECT id FROM users WHERE role='superadmin' AND active LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	var currency string
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,assigned_by,assigned_by_email,note
		) VALUES($1,0,$2,$3,'admin@example.invalid','petty test')
	`, cashierID, currency, adminID); err != nil {
		t.Fatal(err)
	}

	token := strings.Repeat("pettycash", 8)
	if _, err := db.Exec(`
		INSERT INTO auth_sessions(user_id,token_hash,expires_at)
		VALUES($1,$2,now()+interval '1 hour')
	`, cashierID, tokenHash(token)); err != nil {
		t.Fatal(err)
	}

	handler := (&Server{db: db}).routes()
	request := func(body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(
			"POST", "/api/petty-cash", strings.NewReader(body),
		)
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("petty cash status=%d want=%d body=%s",
				w.Code, want, w.Body.String())
		}
		return w
	}
	var sessionID int64
	{
		r := httptest.NewRequest(
			"POST", "/api/session/open",
			strings.NewReader(`{"openingCash":0}`),
		)
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatalf("open session: %d %s", w.Code, w.Body.String())
		}
		var result struct{ ID int64 }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		sessionID = result.ID
	}
	if sessionID <= 0 {
		t.Fatal("session was not opened")
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
			SELECT balance FROM financial_accounts
			WHERE account_type='staff_cash' AND user_id=$1
		`, cashierID)
	}

	inBody := `{"requestId":"petty-cash-in-test-0001","direction":"in","amount":1500,"category":"Float","note":"Cash received"}`
	request(inBody, 201)
	if got := balance(); got != 1500 {
		t.Fatalf("petty IN balance=%d want=1500", got)
	}

	request(inBody, 200)
	if got := scalar(`SELECT COUNT(*) FROM petty_cash_entries`); got != 1 {
		t.Fatalf("replayed petty IN entries=%d want=1", got)
	}

	outBody := `{"requestId":"petty-cash-out-test-0001","direction":"out","amount":2000,"category":"Supplies","note":"Office supplies"}`
	request(outBody, 201)
	if got := balance(); got != -500 {
		t.Fatalf("petty OUT balance=%d want=-500", got)
	}

	request(outBody, 200)
	request(`{"requestId":"petty-cash-out-test-0001","direction":"out","amount":1000,"category":"Supplies","note":"Changed amount"}`, 409)

	if got := balance(); got != -500 {
		t.Fatalf("duplicate changed balance=%d", got)
	}
	if got := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type IN ('petty_cash_in','petty_cash_out')
	`); got != 2 {
		t.Fatalf("petty ledger entries=%d want=2", got)
	}
	if got := scalar(`SELECT COUNT(*) FROM petty_cash_entries`); got != 2 {
		t.Fatalf("petty records=%d want=2", got)
	}
	if got := scalar(`
		SELECT COALESCE(SUM(amount),0)
		FROM financial_ledger_entries
		WHERE entry_type IN ('petty_cash_in','petty_cash_out')
	`); got != -500 {
		t.Fatalf("petty ledger net=%d want=-500", got)
	}

	// Petty cash rollback must preserve both the register entry
	// and the persistent financial account.
	beforeBalance := balance()
	beforeRecords := scalar(`SELECT COUNT(*) FROM petty_cash_entries`)
	beforeLedger := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type IN ('petty_cash_in','petty_cash_out')
	`)

	if _, err := db.Exec(`
		CREATE FUNCTION reject_test_petty_ledger() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.entry_type='petty_cash_out' THEN
				RAISE EXCEPTION 'simulated petty cash posting failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_test_petty_ledger_trigger
		BEFORE INSERT ON financial_ledger_entries
		FOR EACH ROW EXECUTE FUNCTION reject_test_petty_ledger();
	`); err != nil {
		t.Fatal(err)
	}

	rollbackBody := `{"requestId":"petty-rollback-test-0001","direction":"out","amount":700,"category":"Test","note":"Rollback verification"}`
	request(rollbackBody, 500)

	if got := balance(); got != beforeBalance {
		t.Fatalf("failed petty cash changed balance=%d want=%d", got, beforeBalance)
	}
	if got := scalar(`SELECT COUNT(*) FROM petty_cash_entries`); got != beforeRecords {
		t.Fatalf("failed petty cash saved entry count=%d want=%d", got, beforeRecords)
	}
	if got := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type IN ('petty_cash_in','petty_cash_out')
	`); got != beforeLedger {
		t.Fatalf("failed petty cash saved ledger count=%d want=%d", got, beforeLedger)
	}

	if _, err := db.Exec(
		`DROP TRIGGER reject_test_petty_ledger_trigger ON financial_ledger_entries`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP FUNCTION reject_test_petty_ledger()`); err != nil {
		t.Fatal(err)
	}

	// Retry after rollback must succeed exactly once.
	request(rollbackBody, 201)
	request(rollbackBody, 200)

	if got := balance(); got != beforeBalance-700 {
		t.Fatalf("retry cash balance=%d want=%d", got, beforeBalance-700)
	}
	if got := scalar(`SELECT COUNT(*) FROM petty_cash_entries`); got != beforeRecords+1 {
		t.Fatalf("retry petty records=%d want=%d", got, beforeRecords+1)
	}
	if got := scalar(`
		SELECT COUNT(*) FROM financial_ledger_entries
		WHERE entry_type IN ('petty_cash_in','petty_cash_out')
	`); got != beforeLedger+1 {
		t.Fatalf("retry petty ledger=%d want=%d", got, beforeLedger+1)
	}

}
