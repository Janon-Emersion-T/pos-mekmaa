package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRegisterOpeningFundingPostgres(t *testing.T) {
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

	err = tx.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin' AND active
		ORDER BY id LIMIT 1
	`).Scan(&adminID)
	if err != nil {
		t.Fatalf("active superadmin required: %v", err)
	}

	var cashierID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO users(
			email,password_hash,role,active
		)
		VALUES(
			'funding-test-cashier@example.invalid',
			'test-only-hash','cashier',true
		)
		RETURNING id
	`).Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	var sourceID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			account_type,currency,balance
		)
		VALUES('central_cash','OPENING-FUNDING-TEST',5000)
		RETURNING id
	`).Scan(&sourceID)
	if err != nil {
		t.Fatal(err)
	}

	var allocationID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,
			assigned_by,assigned_by_email,
			funding_type,funding_account_id
		)
		VALUES(
			$1,1500,'OPENING-FUNDING-TEST',
			$2,'admin@example.invalid',
			'new_funds',$3
		)
		RETURNING id
	`, cashierID, adminID, sourceID).Scan(&allocationID)
	if err != nil {
		t.Fatal(err)
	}

	var sessionID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO register_sessions(
			opening_cash,opened_by,
			opened_by_email,currency
		)
		VALUES(
			1500,$1,
			'funding-test-cashier@example.invalid',
			'OPENING-FUNDING-TEST'
		)
		RETURNING id
	`, cashierID).Scan(&sessionID)
	if err != nil {
		t.Fatal(err)
	}

	err = postRegisterOpeningFunding(
		ctx, tx,
		allocationID,
		sessionID,
		cashierID,
		cashierID,
	)
	if err != nil {
		t.Fatalf("opening funding failed: %v", err)
	}

	var sourceBalance int64
	err = tx.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts
		WHERE id=$1
	`, sourceID).Scan(&sourceBalance)
	if err != nil {
		t.Fatal(err)
	}

	var cashierBalance int64
	err = tx.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts
		WHERE account_type='staff_cash'
		  AND user_id=$1
		  AND currency='OPENING-FUNDING-TEST'
	`, cashierID).Scan(&cashierBalance)
	if err != nil {
		t.Fatal(err)
	}

	if sourceBalance != 3500 || cashierBalance != 1500 {
		t.Fatalf(
			"incorrect balances: source=%d cashier=%d",
			sourceBalance, cashierBalance,
		)
	}

	var entryCount int
	var ledgerTotal int64

	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*),COALESCE(SUM(amount),0)
		FROM financial_ledger_entries
		WHERE reference_type='register_cash_allocation'
		  AND reference_id=$1
	`, allocationID).Scan(&entryCount, &ledgerTotal)
	if err != nil {
		t.Fatal(err)
	}

	if entryCount != 2 || ledgerTotal != 0 {
		t.Fatalf(
			"invalid ledger: entries=%d net=%d",
			entryCount, ledgerTotal,
		)
	}

	var posted bool

	err = tx.QueryRowContext(ctx, `
		SELECT funding_posted_at IS NOT NULL
		FROM register_cash_allocations
		WHERE id=$1
	`, allocationID).Scan(&posted)
	if err != nil {
		t.Fatal(err)
	}

	if !posted {
		t.Fatal("funding posting timestamp missing")
	}

	if err := postRegisterOpeningFunding(
		ctx, tx,
		allocationID,
		sessionID,
		cashierID,
		cashierID,
	); err == nil {
		t.Fatal("duplicate funding was accepted")
	}
}
