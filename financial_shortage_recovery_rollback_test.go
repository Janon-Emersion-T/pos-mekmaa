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

func TestCashShortageRecoveryRollbackPostgres(t *testing.T) {
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

	currency := fmt.Sprintf(
		"ROLLBACK-TEST-%d",
		time.Now().UnixNano(),
	)

	var shortageID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,currency,original_amount,remarks
		)
		VALUES($1,$2,500,'Rollback test')
		RETURNING id
	`, cashierID, currency).Scan(&shortageID)
	if err != nil {
		t.Fatal(err)
	}

	// Reject an invalid request before any financial changes.
	err = recoverCashShortage(
		ctx, db, shortageID, adminID,
		600,
		fmt.Sprintf("rollback-%d", shortageID),
		"Amount exceeds shortage",
	)
	if err == nil {
		t.Fatal("expected recovery rejection")
	}

	var recovered int64
	err = db.QueryRowContext(ctx, `
		SELECT recovered_amount
		FROM cash_shortages
		WHERE id=$1
	`, shortageID).Scan(&recovered)
	if err != nil {
		t.Fatal(err)
	}

	if recovered != 0 {
		t.Fatalf("expected recovered=0, got %d", recovered)
	}

	var resolutionCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_shortage_resolutions
		WHERE shortage_id=$1
	`, shortageID).Scan(&resolutionCount)
	if err != nil {
		t.Fatal(err)
	}

	if resolutionCount != 0 {
		t.Fatalf(
			"expected zero resolution events, got %d",
			resolutionCount,
		)
	}

	var ledgerCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_ledger_entries l
		JOIN cash_shortage_resolutions r
		  ON r.id=l.reference_id
		WHERE r.shortage_id=$1
		  AND l.entry_type='shortage_recovery'
		  AND l.reference_type='cash_shortage_resolution'
	`, shortageID).Scan(&ledgerCount)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 0 {
		t.Fatalf("expected zero ledger entries, got %d", ledgerCount)
	}
}
