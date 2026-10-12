package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCashShortageRecoveryPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 30*time.Second,
	)
	defer cancel()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var adminID, cashierID int64

	err = db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin' AND active=true
		ORDER BY id LIMIT 1
	`).Scan(&adminID)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='cashier' AND active=true
		ORDER BY id LIMIT 1
	`).Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	// Separate currency prevents interference with real cash balances.
	currency := fmt.Sprintf("RECOVERY-TEST-%d", time.Now().UnixNano())

	var shortageID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,currency,original_amount,remarks
		)
		VALUES($1,$2,500,'Recovery integration test')
		RETURNING id
	`, cashierID, currency).Scan(&shortageID)
	if err != nil {
		t.Fatal(err)
	}

	key1 := fmt.Sprintf("recovery-%d-first", shortageID)
	key2 := fmt.Sprintf("recovery-%d-final", shortageID)

	if err := recoverCashShortage(
		ctx, db, shortageID, cashierID,
		100, key1, "Unauthorized attempt",
	); err == nil {
		t.Fatal("cashier recovery was accepted")
	}

	if err := recoverCashShortage(
		ctx, db, shortageID, adminID,
		200, key1, "First cash recovery",
	); err != nil {
		t.Fatal(err)
	}

	// Repeating the same request must not credit cash twice.
	if err := recoverCashShortage(
		ctx, db, shortageID, adminID,
		200, key1, "Duplicate cash recovery",
	); err == nil {
		t.Fatal("duplicate recovery was accepted")
	}

	if err := recoverCashShortage(
		ctx, db, shortageID, adminID,
		400, "excess-"+key1, "Excessive recovery",
	); err == nil {
		t.Fatal("excessive recovery was accepted")
	}

	var recovered int64
	var status string

	err = db.QueryRowContext(ctx, `
		SELECT recovered_amount,status
		FROM cash_shortages WHERE id=$1
	`, shortageID).Scan(&recovered, &status)
	if err != nil {
		t.Fatal(err)
	}

	if recovered != 200 || status != "unresolved" {
		t.Fatalf(
			"unexpected partial recovery: %d %s",
			recovered, status,
		)
	}

	var balance int64
	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts
		WHERE account_type='central_cash'
		  AND currency=$1
	`, currency).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	if balance != 200 {
		t.Fatalf("expected cash balance 200, got %d", balance)
	}

	if err := recoverCashShortage(
		ctx, db, shortageID, adminID,
		300, key2, "Final cash recovery",
	); err != nil {
		t.Fatal(err)
	}

	var ledgerCount int
	var ledgerTotal int64

	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*),COALESCE(SUM(l.amount),0)
		FROM financial_ledger_entries l
		JOIN cash_shortage_resolutions r
		  ON l.reference_id=r.id
		WHERE l.reference_type='cash_shortage_resolution'
		  AND l.entry_type='shortage_recovery'
		  AND r.shortage_id=$1
	`, shortageID).Scan(&ledgerCount, &ledgerTotal)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 2 || ledgerTotal != 500 {
		t.Fatalf(
			"unexpected ledger: count=%d total=%d",
			ledgerCount, ledgerTotal,
		)
	}

	err = db.QueryRowContext(ctx, `
		SELECT recovered_amount,status
		FROM cash_shortages WHERE id=$1
	`, shortageID).Scan(&recovered, &status)
	if err != nil {
		t.Fatal(err)
	}

	if recovered != 500 || status != "resolved" {
		t.Fatalf(
			"unexpected final recovery: %d %s",
			recovered, status,
		)
	}

	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts
		WHERE account_type='central_cash'
		  AND currency=$1
	`, currency).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	if balance != 500 {
		t.Fatalf("expected final balance 500, got %d", balance)
	}
}
