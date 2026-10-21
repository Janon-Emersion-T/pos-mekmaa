package main

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"testing"
)

func TestSuperadminTransferDatabaseSchema(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for _, column := range []string{
		"source_account_id",
		"destination_account_id",
		"requested_amount",
		"approved_amount",
		"status",
		"requested_by",
		"reviewed_by",
		"reviewed_at",
		"request_id",
	} {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_name='financial_transfers'
				  AND column_name=$1
			)
		`, column).Scan(&exists)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("missing financial_transfers column: %s", column)
		}
	}
}
