package main

import (
	"context"
	"os"
	"testing"
)

func TestPostgresMigration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	db, err := openDB(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err = db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('products','sales','register_sessions','petty_cash_entries','purchases','inventory_movements')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 6 {
		t.Fatalf("found %d core tables, want 6", tables)
	}
}
