package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCashShortageWaiverPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 15*time.Second,
	)
	defer cancel()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var adminID int64
	err = db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin' AND active=true
		ORDER BY id LIMIT 1
	`).Scan(&adminID)
	if err != nil {
		t.Fatal(err)
	}

	var cashierID int64
	err = db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='cashier' AND active=true
		ORDER BY id LIMIT 1
	`).Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test shortage in the isolated database.
	var shortageID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,currency,original_amount,remarks
		)
		VALUES($1,'TEST-WAIVER',500,'Integration test')
		RETURNING id
	`, cashierID).Scan(&shortageID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		// Remove the test data only if it remains safe to do so.
		// The append-only trigger prevents deleting resolution events,
		// so this test's records remain in the disposable database.
	})

	// A cashier cannot approve a waiver.
	if err := resolveCashShortage(
		ctx, db, shortageID, cashierID,
		100, "waive", "Unauthorized waiver",
	); err == nil {
		t.Fatal("cashier waiver was accepted")
	}

	// Cash recovery remains disabled.
	if err := resolveCashShortage(
		ctx, db, shortageID, adminID,
		100, "recover", "Unverified recovery",
	); err == nil {
		t.Fatal("unverified cash recovery was accepted")
	}

	// Partial waiver: Rs. 200.
	if err := resolveCashShortage(
		ctx, db, shortageID, adminID,
		200, "waive", "First partial waiver",
	); err != nil {
		t.Fatal(err)
	}

	// Excessive waiver must be rejected.
	if err := resolveCashShortage(
		ctx, db, shortageID, adminID,
		400, "waive", "Excessive waiver",
	); err == nil {
		t.Fatal("excessive waiver was accepted")
	}

	// Final waiver: Rs. 300.
	if err := resolveCashShortage(
		ctx, db, shortageID, adminID,
		300, "waive", "Final waiver",
	); err != nil {
		t.Fatal(err)
	}

	var waived int64
	var status string

	err = db.QueryRowContext(ctx, `
		SELECT waived_amount,status
		FROM cash_shortages
		WHERE id=$1
	`, shortageID).Scan(&waived, &status)
	if err != nil {
		t.Fatal(err)
	}

	if waived != 500 || status != "resolved" {
		t.Fatalf(
			"unexpected resolution: waived=%d status=%s",
			waived, status,
		)
	}

	var count int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM cash_shortage_resolutions
		WHERE shortage_id=$1
	`, shortageID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("expected 2 resolution events, got %d", count)
	}

	// A resolved shortage cannot be waived again.
	if err := resolveCashShortage(
		ctx, db, shortageID, adminID,
		1, "waive", "Duplicate waiver",
	); err == nil {
		t.Fatal("resolved shortage was modified")
	}
}
