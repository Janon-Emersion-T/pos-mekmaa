package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFinancialAccountInitializationPostgres(t *testing.T) {
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
	schema := fmt.Sprintf("financial_init_test_%d", time.Now().UnixNano())
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

	var currency string
	if err := db.QueryRow(`SELECT currency FROM store_settings WHERE id=1`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	assertFinancialInitializationState(t, db, currency, 0)

	if _, err := db.Exec(`UPDATE financial_accounts SET balance=432100 WHERE account_type='central_cash' AND currency=$1`, currency); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM financial_accounts WHERE account_type IN ('investment_cash','bank') AND currency=$1`, currency); err != nil {
		t.Fatal(err)
	}
	if err := ensureFinancialAccounts(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertFinancialInitializationState(t, db, currency, 432100)

	for i := 0; i < 3; i++ {
		if err := ensureFinancialAccounts(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	assertFinancialInitializationState(t, db, currency, 432100)
}

func assertFinancialInitializationState(t *testing.T, db *sql.DB, currency string, centralBalance int64) {
	t.Helper()
	rows, err := db.Query(`SELECT account_type,balance FROM financial_accounts WHERE currency=$1 AND user_id IS NULL AND account_type IN ('central_cash','investment_cash','bank')`, currency)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	balances := map[string]int64{}
	for rows.Next() {
		var accountType string
		var balance int64
		if err := rows.Scan(&accountType, &balance); err != nil {
			t.Fatal(err)
		}
		balances[accountType] = balance
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(balances) != 3 || balances["central_cash"] != centralBalance || balances["investment_cash"] != 0 || balances["bank"] != 0 {
		t.Fatalf("unexpected initialized accounts: %#v", balances)
	}
	for table, query := range map[string]string{
		"ledger":      `SELECT COUNT(*) FROM financial_ledger_entries`,
		"capital":     `SELECT COUNT(*) FROM capital_entries`,
		"allocations": `SELECT COUNT(*) FROM register_cash_allocations`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("initialization created %s records: %d", table, count)
		}
	}
}
