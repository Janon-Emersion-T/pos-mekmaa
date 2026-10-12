package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestOpeningFundingLedgerRollbackPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx := context.Background()

	setTestBootstrapAdmin(t)
	db, err := openDB(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var adminID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin' AND active
		ORDER BY id LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	var cashierID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role,active)
		VALUES(
			'opening-rollback@example.invalid',
			'test-only-hash','cashier',true
		)
		RETURNING id
	`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}

	var sourceID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			account_type,currency,balance
		)
		VALUES('central_cash','ROLLBACK-OPENING',5000)
		RETURNING id
	`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}

	var allocationID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,
			assigned_by,assigned_by_email,
			funding_type,funding_account_id
		)
		VALUES(
			$1,1500,'ROLLBACK-OPENING',
			$2,'admin@example.invalid',
			'new_funds',$3
		)
		RETURNING id
	`, cashierID, adminID, sourceID).Scan(&allocationID); err != nil {
		t.Fatal(err)
	}

	var sessionID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO register_sessions(
			opening_cash,opened_by,
			opened_by_email,currency
		)
		VALUES(
			1500,$1,
			'opening-rollback@example.invalid',
			'ROLLBACK-OPENING'
		)
		RETURNING id
	`, cashierID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	// The trigger exists only inside this transaction.
	// PostgreSQL will roll it back with the test data.
	_, err = tx.ExecContext(ctx, `
		CREATE FUNCTION reject_opening_funding_test()
		RETURNS trigger AS $$
		BEGIN
			IF NEW.reference_type='register_cash_allocation'
			   AND NEW.entry_type='transfer_in' THEN
				RAISE EXCEPTION 'Simulated opening funding ledger failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;

		CREATE TRIGGER reject_opening_funding_test_trigger
		BEFORE INSERT ON financial_ledger_entries
		FOR EACH ROW
		EXECUTE FUNCTION reject_opening_funding_test();
	`)
	if err != nil {
		t.Fatal(err)
	}

	err = postRegisterOpeningFunding(
		ctx, tx,
		allocationID, sessionID,
		cashierID, cashierID,
	)

	if err == nil ||
		!strings.Contains(
			err.Error(),
			"Simulated opening funding ledger failure",
		) {
		t.Fatalf("expected simulated ledger failure, got %v", err)
	}

	// A PostgreSQL statement error aborts the transaction.
	// Rolling it back must undo the source debit, cashier
	// credit, allocation, session, and trigger.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_ledger_entries
		WHERE reference_type='register_cash_allocation'
		  AND reference_id=$1
	`, allocationID).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("ledger entries survived rollback: %d", count)
	}

	var allocationCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM register_cash_allocations
		WHERE id=$1
	`, allocationID).Scan(&allocationCount); err != nil {
		t.Fatal(err)
	}

	if allocationCount != 0 {
		t.Fatal("allocation survived transaction rollback")
	}

	var accountCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_accounts
		WHERE id=$1
	`, sourceID).Scan(&accountCount); err != nil {
		t.Fatal(err)
	}

	if accountCount != 0 {
		t.Fatal("funding account survived transaction rollback")
	}

	t.Log(fmt.Sprintf(
		"ledger failure correctly rolled back allocation %d",
		allocationID,
	))
}
