package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

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
