package main

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFinancialApprovalPostgres(t *testing.T) {
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

	// Generate a unique identifier for this test execution.
	// All records remain confined to the disposable test database.
	var schema string
	err = db.QueryRowContext(ctx,
		`SELECT 'approval_test_' || txid_current()::text`,
	).Scan(&schema)
	if err != nil {
		t.Fatal(err)
	}
	var cashierID, adminID int64

	err = db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin'
		ORDER BY id LIMIT 1
	`).Scan(&adminID)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role)
		VALUES(
			'finance-approval-test-' || $1 || '@example.test',
			'test-only',
			'cashier'
		)
		RETURNING id
	`, schema).Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	var sourceID, destinationID int64

	err = db.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			user_id,account_type,currency,balance
		)
		VALUES($1,'staff_cash',$2,650000)
		RETURNING id
	`, cashierID, schema).Scan(&sourceID)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			user_id,account_type,currency,balance
		)
		VALUES($1,'superadmin_cash',$2,0)
		RETURNING id
	`, adminID, schema).Scan(&destinationID)
	if err != nil {
		t.Fatal(err)
	}

	var requestID int64

	requestID, err = requestFinancialTransfer(
		ctx, db,
		sourceID, destinationID,
		600000, cashierID,
		"End of shift cash transfer",
	)
	if err != nil {
		t.Fatal(err)
	}

	var pendingBalance int64

	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, sourceID).Scan(&pendingBalance)
	if err != nil {
		t.Fatal(err)
	}

	if pendingBalance != 650000 {
		t.Fatalf("pending request changed balance: %d", pendingBalance)
	}

	// The cashier must not be able to approve their own request.
	err = reviewFinancialTransfer(
		ctx, db,
		requestID, cashierID,
		500000, true,
		"Unauthorized cashier approval",
	)
	if err == nil {
		t.Fatal("cashier approval was incorrectly accepted")
	}

	// Ordinary admins must not approve financial transfers.
	var ordinaryAdminID int64

	err = db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role)
		VALUES($1,'test-only','admin')
		RETURNING id
	`, "finance-admin-"+schema+"@example.test").Scan(&ordinaryAdminID)
	if err != nil {
		t.Fatal(err)
	}

	err = reviewFinancialTransfer(
		ctx, db,
		requestID, ordinaryAdminID,
		500000, true,
		"Unauthorized ordinary admin approval",
	)
	if err == nil {
		t.Fatal("ordinary admin approval was incorrectly accepted")
	}

	// An inactive superadmin must also be rejected.
	var inactiveAdminID int64

	err = db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role,active)
		VALUES($1,'test-only','superadmin',false)
		RETURNING id
	`, "finance-inactive-"+schema+"@example.test").Scan(&inactiveAdminID)
	if err != nil {
		t.Fatal(err)
	}

	err = reviewFinancialTransfer(
		ctx, db,
		requestID, inactiveAdminID,
		500000, true,
		"Unauthorized inactive superadmin approval",
	)
	if err == nil {
		t.Fatal("inactive superadmin approval was incorrectly accepted")
	}

	// Unauthorized review must not change the request or balances.
	var unauthorizedStatus string
	var unauthorizedBalance int64

	err = db.QueryRowContext(ctx, `
		SELECT status FROM financial_transfers WHERE id=$1
	`, requestID).Scan(&unauthorizedStatus)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, sourceID).Scan(&unauthorizedBalance)
	if err != nil {
		t.Fatal(err)
	}

	if unauthorizedStatus != "pending" || unauthorizedBalance != 650000 {
		t.Fatalf(
			"unauthorized review changed state: status=%s balance=%d",
			unauthorizedStatus, unauthorizedBalance,
		)
	}

	err = reviewFinancialTransfer(
		ctx, db,
		requestID, adminID,
		500000, true,
		"Partial approval",
	)
	if err != nil {
		t.Fatal(err)
	}

	var sourceBalance, destinationBalance int64

	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, sourceID).Scan(&sourceBalance)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, destinationID).Scan(&destinationBalance)
	if err != nil {
		t.Fatal(err)
	}

	if sourceBalance != 150000 || destinationBalance != 500000 {
		t.Fatalf(
			"incorrect balances: cashier=%d admin=%d",
			sourceBalance, destinationBalance,
		)
	}

	err = reviewFinancialTransfer(
		ctx, db,
		requestID, adminID,
		500000, true,
		"Duplicate approval",
	)

	if err == nil {
		t.Fatal("duplicate approval was accepted")
	}

	var status string
	err = db.QueryRowContext(ctx, `
		SELECT status FROM financial_transfers WHERE id=$1
	`, requestID).Scan(&status)
	if err != nil {
		t.Fatal(err)
	}

	if status != "partially_approved" {
		t.Fatalf("unexpected transfer status: %s", status)
	}
}
