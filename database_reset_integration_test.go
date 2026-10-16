package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestDatabaseResetIntegration(t *testing.T) {
	const testURL = "postgres://mekmaa_reset_test:local_reset_test_2026@127.0.0.1:55439/mekmaa_reset_test?sslmode=disable"

	if os.Getenv("MEKMAA_ALLOW_RESET_TEST") != "1" ||
		os.Getenv("TEST_DATABASE_URL") != testURL {
		t.Skip("reset integration test requires the isolated local test database")
	}

	setTestBootstrapAdmin(t)

	db, err := openDB(context.Background(), testURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var userID int64
	err = db.QueryRow(`
		SELECT id FROM users
		WHERE email='reset-test@example.test'
		  AND role='superadmin' AND active
	`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	const password = "ResetTest@2026"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		"UPDATE users SET password_hash=$1 WHERE id=$2",
		string(hash), userID,
	); err != nil {
		t.Fatal(err)
	}

	var originalCurrency string
	if err := db.QueryRow(
		"SELECT currency FROM store_settings WHERE id=1",
	).Scan(&originalCurrency); err != nil {
		t.Fatal(err)
	}

	var originalOwnerID int64
	if err := db.QueryRow(
		"SELECT id FROM customers WHERE is_owner=true",
	).Scan(&originalOwnerID); err != nil {
		t.Fatal(err)
	}

	// Add a normal customer and a financial account to prove they are cleared.
	if _, err := db.Exec(
		"INSERT INTO customers(name,notes) VALUES ('Reset Test Customer','Disposable test record')",
	); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
		INSERT INTO financial_accounts(account_type,currency,balance)
		VALUES ('central_cash',$1,15000)
		ON CONFLICT DO NOTHING
	`, originalCurrency); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: db}

	send := func(pass string) *httptest.ResponseRecorder {
		t.Helper()

		body := `{"password":"` + pass + `","confirmation":"RESET DATABASE"}`

		request := httptest.NewRequest(
			http.MethodPost, "/api/system/reset",
			strings.NewReader(body),
		)
		request = request.WithContext(
			context.WithValue(
				request.Context(),
				userContextKey,
				&User{ID: userID, Role: "superadmin", Active: true},
			),
		)

		response := httptest.NewRecorder()
		requireRoles(server.resetDatabase, "superadmin").ServeHTTP(response, request)
		return response
	}

	wrong := send("IncorrectPassword@123")
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf(
			"wrong password: status=%d body=%s",
			wrong.Code, wrong.Body.String(),
		)
	}

	result := send(password)
	if result.Code != http.StatusOK {
		t.Fatalf(
			"reset failed: status=%d body=%s",
			result.Code, result.Body.String(),
		)
	}

	checkCount := func(query string, expected int) {
		t.Helper()
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("%s: got %d, want %d", query, count, expected)
		}
	}

	checkCount("SELECT COUNT(*) FROM customers WHERE is_owner=true", 1)
	checkCount("SELECT COUNT(*) FROM customers", 1)
	checkCount("SELECT COUNT(*) FROM financial_accounts", 3)
	checkCount("SELECT COUNT(*) FROM financial_accounts WHERE balance=0 AND user_id IS NULL AND account_type IN ('central_cash','investment_cash','bank')", 3)
	checkCount("SELECT COUNT(*) FROM financial_ledger_entries", 0)
	checkCount("SELECT COUNT(*) FROM capital_entries", 0)
	checkCount("SELECT COUNT(*) FROM register_cash_allocations", 0)
	checkCount("SELECT COUNT(*) FROM customer_opening_balances", 0)
	checkCount("SELECT COUNT(*) FROM customer_payments", 0)
	checkCount("SELECT COUNT(*) FROM sales", 0)
	checkCount("SELECT COUNT(*) FROM register_sessions", 0)
	checkCount("SELECT COUNT(*) FROM guest_refreshments", 0)
	checkCount("SELECT COUNT(*) FROM users WHERE role='superadmin' AND active", 1)

	var ownerID int64
	var ownerName string
	var ownerFlag bool

	if err := db.QueryRow(`
		SELECT id,name,is_owner
		FROM customers WHERE is_owner=true
	`).Scan(&ownerID, &ownerName, &ownerFlag); err != nil {
		t.Fatal(err)
	}

	if ownerName != "Owner" || !ownerFlag {
		t.Fatalf("Owner not restored correctly: %q %v", ownerName, ownerFlag)
	}

	var currency string
	if err := db.QueryRow(
		"SELECT currency FROM store_settings WHERE id=1",
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	if currency != originalCurrency {
		t.Fatalf("currency changed: %s -> %s", originalCurrency, currency)
	}

	t.Logf(
		"PASS: Owner restored (previous ID %d, current ID %d), zero transactions and financial balances",
		originalOwnerID, ownerID,
	)
}

// Ensure the test file continues using the standard SQL database type.
var _ *sql.DB
