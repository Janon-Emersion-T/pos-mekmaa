package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCashShortagePostgres(t *testing.T) {
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

	var suffix string
	if err := db.QueryRowContext(ctx,
		`SELECT txid_current()::text`,
	).Scan(&suffix); err != nil {
		t.Fatal(err)
	}

	currency := "S" + suffix

	var cashierID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,role)
		VALUES($1,'test-only','cashier')
		RETURNING id
	`, fmt.Sprintf("shortage-%s@example.test", suffix)).
		Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	var sessionID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO register_sessions(
			opening_cash,
			opened_by,
			opened_by_email,
			currency
		)
		VALUES(1500,$1,$2,$3)
		RETURNING id
	`, cashierID,
		fmt.Sprintf("shortage-%s@example.test", suffix),
		currency).Scan(&sessionID)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	shortageID, err := recordCashShortage(
		ctx, tx,
		cashierID, sessionID,
		6500, 6000,
		currency,
		"Physical cash is Rs. 500 short",
	)
	if err != nil {
		t.Fatal(err)
	}

	if shortageID <= 0 {
		t.Fatal("shortage ID was not created")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	balance, err := getCashShortageBalance(
		ctx, db, cashierID, currency,
	)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Original != 500 ||
		balance.Recovered != 0 ||
		balance.Waived != 0 ||
		balance.Outstanding != 500 {
		t.Fatalf("incorrect shortage balance: %+v", balance)
	}

	duplicateTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer duplicateTx.Rollback()

	_, err = recordCashShortage(
		ctx, duplicateTx,
		cashierID, sessionID,
		6500, 6000,
		currency,
		"Duplicate shortage",
	)

	if err == nil {
		t.Fatal("duplicate shortage was accepted")
	}

	// Release the session row lock before closing the session.
	if err := duplicateTx.Rollback(); err != nil {
		t.Fatalf("rollback duplicate transaction: %v", err)
	}

	// Closing a session must not erase the outstanding shortage.
	if _, err := db.ExecContext(ctx, `
		UPDATE register_sessions
		SET status='closed',
		    closed_at=now(),
		    closing_cash=6000,
		    closing_expected_cash=6500
		WHERE id=$1
	`, sessionID); err != nil {
		t.Fatal(err)
	}

	afterClose, err := getCashShortageBalance(
		ctx, db, cashierID, currency,
	)
	if err != nil {
		t.Fatal(err)
	}

	if afterClose.Outstanding != 500 {
		t.Fatalf(
			"shortage did not carry forward: %d",
			afterClose.Outstanding,
		)
	}
}
