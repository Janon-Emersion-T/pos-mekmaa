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

func TestOptimizedDebtCapacityPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("optimized_capacity_%d", time.Now().UnixNano())

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

	var currency string
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	check := func(wantMargin, wantDebt, wantAvailable, wantUnknown int64) {
		t.Helper()

		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		got, err := optimizedDebtCapacity(context.Background(), tx, currency)
		if err != nil {
			t.Fatal(err)
		}

		if got.VerifiedMargin != wantMargin ||
			got.OutstandingDebt != wantDebt ||
			got.AvailableCredit != wantAvailable ||
			got.UnverifiedItems != wantUnknown {
			t.Fatalf("capacity got %+v, want margin=%d debt=%d available=%d unknown=%d",
				got, wantMargin, wantDebt, wantAvailable, wantUnknown)
		}
	}

	t.Run("empty store", func(t *testing.T) {
		check(0, 0, 0, 0)
	})

	var productID int64

	if err := db.QueryRow(`
        INSERT INTO products(
            name,category,price,cost,stock,inventory_cost_balance
        )
        VALUES('Verified test product','Test',700,500,100,50000)
        RETURNING id
    `).Scan(&productID); err != nil {
		t.Fatal(err)
	}

	t.Run("verified inventory margin", func(t *testing.T) {
		check(20000, 0, 20000, 0)
	})

	var adminID int64
	if err := db.QueryRow(`
        SELECT id FROM users WHERE role='superadmin' LIMIT 1
    `).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	var customerID int64
	if err := db.QueryRow(`
        INSERT INTO customers(name)
        VALUES('Optimized Debt Test Customer')
        RETURNING id
    `).Scan(&customerID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
        INSERT INTO customer_opening_balances(
            customer_id,amount,currency,created_by,cashier_email
        )
        VALUES($1,12000,$2,$3,'bootstrap@example.test')
    `, customerID, currency, adminID); err != nil {
		t.Fatal(err)
	}

	t.Run("existing debt reduces headroom", func(t *testing.T) {
		check(20000, 12000, 8000, 0)
	})

	if _, err := db.Exec(`
        INSERT INTO products(
            name,category,price,cost,stock,inventory_cost_balance
        )
        VALUES('Unverified stock','Test',1000,100,10,NULL)
    `); err != nil {
		t.Fatal(err)
	}

	t.Run("unverified inventory blocks new credit", func(t *testing.T) {
		check(20000, 12000, 0, 1)
	})

	if _, err := db.Exec(`
        UPDATE products
        SET stock=0
        WHERE name='Unverified stock'
    `); err != nil {
		t.Fatal(err)
	}

	t.Run("zero stock does not block capacity", func(t *testing.T) {
		check(20000, 12000, 8000, 0)
	})

	_ = productID
}
