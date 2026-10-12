package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestFinancialCashPostingPostgres(t *testing.T) {
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
		"cash_posting_test_%d",
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

	ctx := context.Background()

	var userID int64
	err = db.QueryRow(`
		INSERT INTO users(email,password_hash,role)
		VALUES('posting-test@example.test','unused','cashier')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	account, err := getOrCreateFinancialAccount(
		ctx, tx, "staff_cash", &userID, "USD",
	)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	post := func(
		amount int64,
		entryType string,
		referenceID int64,
	) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		err = postFinancialCashMovement(
			ctx, tx, account.ID, amount,
			entryType, "cash_posting_test",
			referenceID, userID, "Integration test",
		)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

	balance := func() int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, account.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	ledgerCount := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*)
			FROM financial_ledger_entries
			WHERE account_id=$1
		`, account.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	// Positive cash posting.
	if err := post(5000, "cash_sale", 101); err != nil {
		t.Fatal(err)
	}
	if balance() != 5000 || ledgerCount() != 1 {
		t.Fatal("positive posting mismatch")
	}

	// Negative cash posting.
	if err := post(-1200, "cash_refund", 102); err != nil {
		t.Fatal(err)
	}
	if balance() != 3800 || ledgerCount() != 2 {
		t.Fatal("negative posting mismatch")
	}

	// Duplicate reference must be rejected.
	if err := post(5000, "cash_sale", 101); err == nil {
		t.Fatal("duplicate posting accepted")
	}
	if balance() != 3800 || ledgerCount() != 2 {
		t.Fatal("duplicate changed financial balance")
	}

	// Invalid request must not change the ledger.
	if err := post(0, "cash_sale", 103); err == nil {
		t.Fatal("zero posting accepted")
	}
	if balance() != 3800 || ledgerCount() != 2 {
		t.Fatal("invalid posting changed financial balance")
	}

	// An explicit rollback must undo both the balance and ledger.
	rollbackTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = postFinancialCashMovement(
		ctx, rollbackTx, account.ID, 700,
		"cash_collection", "cash_posting_test",
		104, userID, "Rollback test",
	)
	if err != nil {
		rollbackTx.Rollback()
		t.Fatal(err)
	}
	if err := rollbackTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if balance() != 3800 || ledgerCount() != 2 {
		t.Fatal("rollback did not restore balance and ledger")
	}

	// Operational debit may create a negative balance.
	if err := post(-5000, "petty_cash_out", 105); err != nil {
		t.Fatal(err)
	}
	if balance() != -1200 || ledgerCount() != 3 {
		t.Fatal("negative account balance was not recorded")
	}
}
