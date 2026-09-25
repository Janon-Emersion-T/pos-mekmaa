package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Fresh integration databases need bootstrap credentials just like first launch.
// Keep these test-only values scoped to each test; t.Setenv restores the environment.
func setTestBootstrapAdmin(t *testing.T) {
	t.Helper()
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "bootstrap@example.test")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "test-only-bootstrap-password")
}

func TestAuthenticationGate(t *testing.T) {
	h := (&Server{}).routes()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("root: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/products", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("API status=%d, want 401", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d, want 200", w.Code)
	}
}

func TestRoleChecks(t *testing.T) {
	if !hasRole("superadmin", "superadmin") {
		t.Fatal("superadmin should be authorized")
	}
	if !hasRole("cashier", "superadmin", "admin", "cashier") {
		t.Fatal("cashier should be authorized for till operations")
	}
	if hasRole("cashier", "superadmin", "admin") {
		t.Fatal("cashier must not be authorized for management operations")
	}
	called := false
	h := requireRoles(func(http.ResponseWriter, *http.Request) { called = true }, "superadmin", "admin")
	r := httptest.NewRequest(http.MethodPost, "/api/purchases", nil)
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, &User{Role: "cashier"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called || w.Code != http.StatusForbidden {
		t.Fatalf("cashier management request: called=%v status=%d", called, w.Code)
	}
}

func TestSecureCookieBehindProxy(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if !s.cookieSecure(r) {
		t.Fatal("HTTPS proxy requests must receive secure cookies")
	}
}

func TestPostgresMigration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	setTestBootstrapAdmin(t)
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
	var migrations int
	if err = db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations < 2 {
		t.Fatalf("found %d migrations, want at least 2", migrations)
	}
	dbAgain, err := openDB(context.Background(), url)
	if err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}
	dbAgain.Close()
	var auditColumns int
	if err = db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND ((table_name='sales' AND column_name='created_by') OR (table_name='register_sessions' AND column_name IN ('opened_by','closed_by')) OR (table_name='purchases' AND column_name='created_by') OR (table_name='petty_cash_entries' AND column_name='created_by') OR (table_name='inventory_movements' AND column_name='created_by'))`).Scan(&auditColumns); err != nil {
		t.Fatal(err)
	}
	if auditColumns != 6 {
		t.Fatalf("found %d audit columns, want 6", auditColumns)
	}
	w := httptest.NewRecorder()
	(&Server{db: db}).routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSettingsValidationAndPermissions(t *testing.T) {
	s := &Server{}
	for _, body := range []string{`{"currency":"XYZ"}`, `{"currency":""}`, `{"currency":"usd"}`, `{"currency":123}`, `{"currency":"USD","extra":true}`} {
		w := httptest.NewRecorder()
		s.updateSettings(w, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s: status=%d, want 400", body, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"currency":"LKR"}`))
	r = r.WithContext(context.WithValue(r.Context(), userContextKey, &User{Role: "cashier"}))
	w := httptest.NewRecorder()
	requireRoles(s.updateSettings, "superadmin", "admin").ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cashier settings update: status=%d, want 403", w.Code)
	}
}

func TestSettingsPersistence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	setTestBootstrapAdmin(t)
	db, err := openDB(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	var original string
	if err := db.QueryRow("SELECT currency FROM store_settings WHERE id=1").Scan(&original); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("UPDATE store_settings SET currency=$1 WHERE id=1", original)
	for _, currency := range supportedCurrencies {
		w := httptest.NewRecorder()
		s.updateSettings(w, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"currency":"`+currency.Code+`"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", currency.Code, w.Code, w.Body.String())
		}
		// Reopening runs startup migrations again: saved preferences must survive.
		reopened, err := openDB(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		w = httptest.NewRecorder()
		(&Server{db: reopened}).settings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
		reopened.Close()
		var got Settings
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || got.Currency != currency.Code || len(got.Currencies) != len(supportedCurrencies) {
			t.Fatalf("read saved %s: status=%d settings=%+v", currency.Code, w.Code, got)
		}
	}
}
