package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConcurrentFinancialCashPostingPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL")
	}

	setTestBootstrapAdmin(t)

	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	schema := fmt.Sprintf(
		"cash_concurrency_test_%d",
		time.Now().UnixNano(),
	)

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	db, err := openDB(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(20)

	ctx := context.Background()

	var userID int64
	err = db.QueryRow(`
		INSERT INTO users(email,password_hash,role)
		VALUES('concurrent-cashier@example.test','unused','cashier')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	setupTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	account, err := getOrCreateFinancialAccount(
		ctx, setupTx, "staff_cash", &userID, "USD",
	)
	if err != nil {
		setupTx.Rollback()
		t.Fatal(err)
	}
	if err := setupTx.Commit(); err != nil {
		t.Fatal(err)
	}

	post := func(referenceID int64, amount int64) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if err := postFinancialCashMovement(
			ctx, tx, account.ID, amount,
			"cash_sale", "concurrency_test",
			referenceID, userID, "Concurrent posting",
		); err != nil {
			return err
		}

		return tx.Commit()
	}

	// Test A: duplicate concurrent requests.
	start := make(chan struct{})
	results := make(chan error, 2)

	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- post(1001, 500)
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
			"duplicate concurrency: successes=%d failures=%d",
			successes, failures,
		)
	}

	// Test B: distinct legitimate postings.
	const workers = 10

	startDistinct := make(chan struct{})
	distinctResults := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-startDistinct
			distinctResults <- post(int64(2000+i), 100)
		}(i)
	}

	close(startDistinct)
	wg.Wait()
	close(distinctResults)

	for err := range distinctResults {
		if err != nil {
			t.Fatalf("distinct concurrent posting failed: %v", err)
		}
	}

	var balance int64
	var count int

	err = db.QueryRow(`
		SELECT balance FROM financial_accounts WHERE id=$1
	`, account.ID).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM financial_ledger_entries
		WHERE account_id=$1
	`, account.ID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	// 500 + (10 x 100) = 1500.
	if balance != 1500 || count != 11 {
		t.Fatalf(
			"concurrent accounting mismatch: balance=%d entries=%d",
			balance, count,
		)
	}
}
