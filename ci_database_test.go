package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// Run explicitly before the full CI test suite.
// This initializes only the database specified by TEST_DATABASE_URL.
func TestCIInitializeDatabase(t *testing.T) {
	if os.Getenv("CI_DATABASE_INIT") != "1" {
		t.Skip("CI database initialization not requested")
	}

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}

	if os.Getenv("BOOTSTRAP_ADMIN_EMAIL") == "" ||
		os.Getenv("BOOTSTRAP_ADMIN_PASSWORD") == "" {
		t.Fatal("CI bootstrap credentials are required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	db, err := openDB(ctx, url)
	if err != nil {
		t.Fatalf("initialize CI database: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM schema_migrations
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count < 19 {
		t.Fatalf("expected at least 19 migrations, found %d", count)
	}

	var adminCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM users
		WHERE role='superadmin' AND active
	`).Scan(&adminCount); err != nil {
		t.Fatal(err)
	}

	if adminCount == 0 {
		t.Fatal("CI bootstrap superadmin missing")
	}

	t.Logf(
		"CI database initialized: %d migrations, %d active superadmin(s)",
		count, adminCount,
	)
}
