package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestInvestmentCashInitializationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	setTestBootstrapAdmin(t)

	db, err := openDB(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var currency string
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var initialID int64
	var initialBalance int64

	err = db.QueryRow(`
        SELECT id,balance
        FROM financial_accounts
        WHERE account_type='investment_cash'
          AND currency=$1
          AND user_id IS NULL
    `, currency).Scan(&initialID, &initialBalance)
	if err != nil {
		t.Fatal(err)
	}

	if initialBalance != 0 {
		t.Fatalf("new investment cash balance must be zero, got %d", initialBalance)
	}

	for i := 0; i < 3; i++ {
		if err := ensureInvestmentCashAccount(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}

	var count int
	var finalID int64
	var finalBalance int64

	err = db.QueryRow(`
        SELECT COUNT(*),MIN(id),MIN(balance)
        FROM financial_accounts
        WHERE account_type='investment_cash'
          AND currency=$1
          AND user_id IS NULL
    `, currency).Scan(&count, &finalID, &finalBalance)

	if err != nil {
		t.Fatal(err)
	}

	if count != 1 || finalID != initialID || finalBalance != initialBalance {
		t.Fatalf(
			"initialization changed account: count=%d id=%d balance=%d",
			count, finalID, finalBalance,
		)
	}

	_ = sql.ErrNoRows
}
