package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFinancialTransferPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx := context.Background()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var actorID int64

	err = tx.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin'
		ORDER BY id LIMIT 1
	`).Scan(&actorID)

	if err != nil {
		t.Fatalf("test requires bootstrap superadmin: %v", err)
	}

	var sourceID, destinationID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			account_type,currency,balance
		)
		VALUES('central_cash','TST',100000)
		RETURNING id
	`).Scan(&sourceID)

	if err != nil {
		t.Fatal(err)
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			account_type,currency,balance
		)
		VALUES('bank','TST',0)
		RETURNING id
	`).Scan(&destinationID)

	if err != nil {
		t.Fatal(err)
	}

	err = transferFinancialBalance(
		ctx, tx,
		sourceID, destinationID,
		50000, actorID,
		"integration_test", 1,
	)

	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	var sourceBalance, destinationBalance int64

	err = tx.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, sourceID).Scan(&sourceBalance)
	if err != nil {
		t.Fatal(err)
	}

	err = tx.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, destinationID).Scan(&destinationBalance)
	if err != nil {
		t.Fatal(err)
	}

	if sourceBalance != 50000 || destinationBalance != 50000 {
		t.Fatalf(
			"unexpected balances: source=%d destination=%d",
			sourceBalance, destinationBalance,
		)
	}

	var count int
	var ledgerTotal int64

	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount),0)
		FROM financial_ledger_entries
		WHERE reference_type='integration_test'
		  AND reference_id=1
	`).Scan(&count, &ledgerTotal)

	if err != nil {
		t.Fatal(err)
	}

	if count != 2 || ledgerTotal != 0 {
		t.Fatalf(
			"ledger invalid: entries=%d total=%d",
			count, ledgerTotal,
		)
	}

	err = transferFinancialBalance(
		ctx, tx,
		sourceID, destinationID,
		999999, actorID,
		"integration_test", 2,
	)

	if err == nil {
		t.Fatal("insufficient funds transfer was accepted")
	}
}
