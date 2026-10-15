package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOptimizedDebtsAPI(t *testing.T) {
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

	schema := fmt.Sprintf("optimized_api_%d", time.Now().UnixNano())

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

	var adminID int64
	if err := db.QueryRow(`
        SELECT id FROM users WHERE role='superadmin' LIMIT 1
    `).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	admin := &User{
		ID:     adminID,
		Role:   "superadmin",
		Email:  "bootstrap@example.test",
		Active: true,
	}

	cashier := &User{
		ID:     adminID,
		Role:   "cashier",
		Email:  "cashier@example.test",
		Active: true,
	}

	server := &Server{db: db}

	toggle := requireRoles(server.updateOptimizedDebts, "superadmin")
	capacity := requireRoles(server.getOptimizedDebtCapacity, "superadmin")

	call := func(
		handler http.Handler,
		actor *User,
		method, path, body string,
	) *httptest.ResponseRecorder {
		t.Helper()

		request := httptest.NewRequest(
			method, path, strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")

		request = request.WithContext(
			context.WithValue(
				request.Context(), userContextKey, actor,
			),
		)

		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	assertStatus := func(
		response *httptest.ResponseRecorder,
		expected int,
	) {
		t.Helper()
		if response.Code != expected {
			t.Fatalf(
				"expected HTTP %d, got %d: %s",
				expected, response.Code, response.Body.String(),
			)
		}
	}

	t.Run("cashier cannot change policy", func(t *testing.T) {
		assertStatus(call(
			toggle, cashier, "PUT",
			"/api/settings/optimized-debts",
			`{"enabled":true}`,
		), http.StatusForbidden)
	})

	t.Run("cashier cannot read capacity", func(t *testing.T) {
		assertStatus(call(
			capacity, cashier, "GET",
			"/api/optimized-debts/capacity", "",
		), http.StatusForbidden)
	})

	t.Run("missing enabled rejected", func(t *testing.T) {
		assertStatus(call(
			toggle, admin, "PUT",
			"/api/settings/optimized-debts", `{}`,
		), http.StatusBadRequest)
	})

	t.Run("enable policy", func(t *testing.T) {
		assertStatus(call(
			toggle, admin, "PUT",
			"/api/settings/optimized-debts",
			`{"enabled":true}`,
		), http.StatusOK)
	})

	t.Run("capacity endpoint", func(t *testing.T) {
		response := call(
			capacity, admin, "GET",
			"/api/optimized-debts/capacity", "",
		)
		assertStatus(response, http.StatusOK)

		if !strings.Contains(response.Body.String(), `"enabled":true`) {
			t.Fatalf("enabled policy not reflected: %s",
				response.Body.String())
		}
	})

	t.Run("repeated enable is idempotent", func(t *testing.T) {
		assertStatus(call(
			toggle, admin, "PUT",
			"/api/settings/optimized-debts",
			`{"enabled":true}`,
		), http.StatusOK)

		var count int
		if err := db.QueryRow(`
            SELECT COUNT(*)
            FROM optimized_debts_setting_changes
        `).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != 1 {
			t.Fatalf("expected one audit record, got %d", count)
		}
	})

	t.Run("disable policy", func(t *testing.T) {
		assertStatus(call(
			toggle, admin, "PUT",
			"/api/settings/optimized-debts",
			`{"enabled":false}`,
		), http.StatusOK)

		var enabled bool
		if err := db.QueryRow(`
            SELECT optimized_debts_enabled
            FROM store_settings WHERE id=1
        `).Scan(&enabled); err != nil {
			t.Fatal(err)
		}

		if enabled {
			t.Fatal("policy should be disabled")
		}

		var count int
		if err := db.QueryRow(`
            SELECT COUNT(*)
            FROM optimized_debts_setting_changes
        `).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count != 2 {
			t.Fatalf("expected two audit records, got %d", count)
		}
	})
}
