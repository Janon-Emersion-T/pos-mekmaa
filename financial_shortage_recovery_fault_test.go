package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCashShortageRecoveryLedgerFailureRollback(t *testing.T) {
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

	currency := fmt.Sprintf("FAULT-TEST-%d", time.Now().UnixNano())

	var shortageID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,currency,original_amount,remarks
		)
		VALUES($1,$2,500,'Ledger rollback test')
		RETURNING id
	`, cashierID, currency).Scan(&shortageID)
	if err != nil {
		t.Fatal(err)
	}

	var accountID int64
	err = db.QueryRowContext(ctx, `
		UPDATE financial_accounts SET balance=0
		WHERE account_type='central_cash' AND currency=$1 AND user_id IS NULL
		RETURNING id
	`, currency).Scan(&accountID)
	if err != nil {
		t.Fatal(err)
	}

	// Use a unique trigger name so the test does not interfere
	// with other tests or existing database triggers.
	triggerName := fmt.Sprintf(
		"test_recovery_rollback_%d", accountID,
	)

	functionName := fmt.Sprintf(
		"test_recovery_rollback_fn_%d", accountID,
	)

	createSQL := fmt.Sprintf(`
		CREATE FUNCTION %s()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF NEW.entry_type='shortage_recovery'
			   AND NEW.account_id=%d
			THEN
				RAISE EXCEPTION
					'Simulated recovery ledger failure';
			END IF;
			RETURN NEW;
		END;
		$$;

		CREATE TRIGGER %s
		BEFORE INSERT ON financial_ledger_entries
		FOR EACH ROW
		EXECUTE FUNCTION %s();
	`, functionName, accountID, triggerName, functionName)

	_, err = db.ExecContext(ctx, createSQL)
	if err != nil {
		t.Fatal(err)
	}

	// Cleanup is registered after creation and runs before
	// the deferred db.Close().
	t.Cleanup(func() {
		cleanupDB, openErr := sql.Open("pgx", url)
		if openErr != nil {
			t.Errorf("open cleanup database: %v", openErr)
			return
		}
		defer cleanupDB.Close()

		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cleanupCancel()

		cleanupSQL := fmt.Sprintf(`
			DROP TRIGGER IF EXISTS %s
			ON financial_ledger_entries;
			DROP FUNCTION IF EXISTS %s();
		`, triggerName, functionName)

		if _, cleanupErr := cleanupDB.ExecContext(
			cleanupCtx, cleanupSQL,
		); cleanupErr != nil {
			t.Errorf("cleanup fault trigger: %v", cleanupErr)
		}
	})

	err = recoverCashShortage(
		ctx, db, shortageID, adminID,
		200,
		fmt.Sprintf("fault-%d", shortageID),
		"Verified physical cash recovery",
	)

	if err == nil {
		t.Fatal("expected simulated ledger failure")
	}

	if !strings.Contains(
		err.Error(), "Simulated recovery ledger failure",
	) {
		t.Fatalf("unexpected recovery error: %v", err)
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

	if recovered != 0 || status != "unresolved" {
		t.Fatalf(
			"shortage changed after rollback: %d %s",
			recovered, status,
		)
	}

	var balance int64
	err = db.QueryRowContext(ctx, `
		SELECT balance
		FROM financial_accounts
		WHERE id=$1
	`, accountID).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	if balance != 0 {
		t.Fatalf("cash credited despite rollback: %d", balance)
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
			"resolution persisted after rollback: %d",
			resolutionCount,
		)
	}

	var ledgerCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_ledger_entries
		WHERE account_id=$1
		  AND entry_type='shortage_recovery'
	`, accountID).Scan(&ledgerCount)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 0 {
		t.Fatalf(
			"ledger persisted after rollback: %d",
			ledgerCount,
		)
	}
}
