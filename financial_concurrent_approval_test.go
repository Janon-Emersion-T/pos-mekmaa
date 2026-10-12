package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestFinancialConcurrentApprovalPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)

	var adminID int64
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE role='superadmin' AND active=true
		ORDER BY id LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	// A unique currency keeps this test independent from
	// other account fixtures in the disposable test database.
	var suffix string
	if err := db.QueryRowContext(ctx,
		`SELECT txid_current()::text`,
	).Scan(&suffix); err != nil {
		t.Fatal(err)
	}

	currency := "C" + suffix

	var cashierID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role)
		VALUES($1,'test-only','cashier')
		RETURNING id
	`, fmt.Sprintf("concurrent-%s@example.test", suffix)).
		Scan(&cashierID); err != nil {
		t.Fatal(err)
	}

	var sourceID, destinationID int64

	if err := db.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			user_id,account_type,currency,balance
		)
		VALUES($1,'staff_cash',$2,100000)
		RETURNING id
	`, cashierID, currency).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			user_id,account_type,currency,balance
		)
		VALUES($1,'superadmin_cash',$2,0)
		RETURNING id
	`, adminID, currency).Scan(&destinationID); err != nil {
		t.Fatal(err)
	}

	requestID, err := requestFinancialTransfer(
		ctx, db,
		sourceID, destinationID,
		80000, cashierID,
		"Concurrent approval test",
	)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			<-start

			results <- reviewFinancialTransfer(
				ctx, db,
				requestID, adminID,
				80000, true,
				"Concurrent approval",
			)
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	successes := 0
	failures := 0

	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 || failures != 1 {
		t.Fatalf(
			"expected one approval and one rejection: success=%d failure=%d",
			successes, failures,
		)
	}

	var sourceBalance, destinationBalance int64

	if err := db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, sourceID).Scan(&sourceBalance); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT balance FROM financial_accounts WHERE id=$1
	`, destinationID).Scan(&destinationBalance); err != nil {
		t.Fatal(err)
	}

	if sourceBalance != 20000 || destinationBalance != 80000 {
		t.Fatalf(
			"double posting detected: source=%d destination=%d",
			sourceBalance, destinationBalance,
		)
	}

	var ledgerCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_ledger_entries
		WHERE reference_type='financial_transfer'
		  AND reference_id=$1
	`, requestID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 2 {
		t.Fatalf("expected exactly two ledger entries, got %d", ledgerCount)
	}
}
