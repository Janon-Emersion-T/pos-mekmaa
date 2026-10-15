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

func TestOptimizedDebtsSerializationAndAudit(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	setTestBootstrapAdmin(t)

	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	schema := fmt.Sprintf("optimized_lock_%d", time.Now().UnixNano())

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP SCHEMA " + schema + " CASCADE")

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

	var actorID int64
	if err := db.QueryRow(`
        SELECT id FROM users WHERE role='superadmin' LIMIT 1
    `).Scan(&actorID); err != nil {
		t.Fatal(err)
	}

	// Simulate the settings toggle and its audit insertion.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var previous bool
	if err := tx.QueryRow(`
        SELECT optimized_debts_enabled
        FROM store_settings WHERE id=1 FOR UPDATE
    `).Scan(&previous); err != nil {
		t.Fatal(err)
	}

	if previous {
		t.Fatal("Optimized Debts must default to OFF")
	}

	if _, err := tx.Exec(`
        UPDATE store_settings
        SET optimized_debts_enabled=true WHERE id=1
    `); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(`
        INSERT INTO optimized_debts_setting_changes(
            previous_enabled,new_enabled,changed_by
        ) VALUES(false,true,$1)
    `, actorID); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var changes int
	if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM optimized_debts_setting_changes
        WHERE previous_enabled=false AND new_enabled=true
    `).Scan(&changes); err != nil {
		t.Fatal(err)
	}

	if changes != 1 {
		t.Fatalf("expected one toggle audit record, got %d", changes)
	}

	// A checkout holds the settings-row lock.
	// A competing checkout must wait until the first commits.
	first, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()

	var enabled bool
	if err := first.QueryRow(`
        SELECT optimized_debts_enabled
        FROM store_settings WHERE id=1 FOR UPDATE
    `).Scan(&enabled); err != nil {
		t.Fatal(err)
	}

	if !enabled {
		t.Fatal("expected enabled setting")
	}

	var wg sync.WaitGroup
	wg.Add(1)

	started := make(chan struct{})
	completed := make(chan error, 1)

	go func() {
		defer wg.Done()

		second, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			completed <- err
			return
		}
		defer second.Rollback()

		close(started)

		var secondEnabled bool
		err = second.QueryRow(`
            SELECT optimized_debts_enabled
            FROM store_settings WHERE id=1 FOR UPDATE
        `).Scan(&secondEnabled)

		completed <- err
	}()

	<-started

	select {
	case err := <-completed:
		t.Fatalf("competing transaction completed before lock release: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: competing transaction is blocked.
	}

	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("competing transaction did not resume")
	}

	wg.Wait()
}
