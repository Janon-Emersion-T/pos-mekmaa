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

func TestConcurrentCashShortageRecoveryPostgres(t *testing.T) {
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
		"CONCURRENT-RECOVERY-%d",
		time.Now().UnixNano(),
	)

	var shortageID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,currency,original_amount,remarks
		)
		VALUES($1,$2,500,'Concurrent recovery test')
		RETURNING id
	`, cashierID, currency).Scan(&shortageID)
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			results <- recoverCashShortage(
				ctx,
				db,
				shortageID,
				adminID,
				400,
				fmt.Sprintf(
					"concurrent-recovery-%d-%d",
					shortageID,
					index,
				),
				"Concurrent cash recovery test",
			)
		}(i)
	}

	wg.Wait()
	close(results)

	successes := 0
	failures := 0

	for result := range results {
		if result == nil {
			successes++
		} else {
			failures++
		}
	}

	if successes != 1 || failures != 1 {
		t.Fatalf(
			"expected one success and one rejection, got %d/%d",
			successes, failures,
		)
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

	if recovered != 400 {
		t.Fatalf("expected recovered 400, got %d", recovered)
	}

	var balance int64
	err = db.QueryRowContext(ctx, `
		SELECT balance
		FROM financial_accounts
		WHERE account_type='central_cash'
		  AND currency=$1
	`, currency).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	if balance != 400 {
		t.Fatalf("expected cash balance 400, got %d", balance)
	}

	var entries int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM financial_ledger_entries l
		JOIN cash_shortage_resolutions r
		  ON r.id=l.reference_id
		WHERE l.entry_type='shortage_recovery'
		  AND l.reference_type='cash_shortage_resolution'
		  AND r.shortage_id=$1
	`, shortageID).Scan(&entries)
	if err != nil {
		t.Fatal(err)
	}

	if entries != 1 {
		t.Fatalf("expected one ledger entry, got %d", entries)
	}
}
