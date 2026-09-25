package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProductValidation(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"name":" ","category":"Food"}`, `{"name":"Bread","category":""}`,
		`{"name":"Bread","category":"Food","price":-1}`,
		`{"name":"Bread","category":"Food","stock":1.5}`,
		`{"name":"Bread","category":"Food","price":2147483648}`,
		`{"name":"Bread","category":"Food","color":"red; background:url(x)"}`,
		`{"name":"Bread","category":"Food","art":"unknown"}`,
	} {
		w := httptest.NewRecorder()
		(&Server{}).saveProduct(w, httptest.NewRequest("POST", "/api/products", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("%s: got %d", body, w.Code)
		}
	}
}

// This test uses its own schema so bulk-delete tests never touch an existing catalog.
func TestProductLifecycleAndRoutes(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	db, err := openDB(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("product_test_%d", time.Now().UnixNano())
	if _, err = db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
	if _, err = db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}
	if err = runMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var userID int64
	err = db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES('test@example.com','unused','admin') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("test", 10)
	if _, err = db.Exec(`INSERT INTO auth_sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, userID, tokenHash(token)); err != nil {
		t.Fatal(err)
	}
	h := (&Server{db: db}).routes()
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	for _, path := range []string{"/pos", "/products", "/sales", "/inventory", "/purchases", "/petty", "/session", "/users", "/settings"} {
		w := request("GET", path, "", 200)
		if !strings.Contains(w.Body.String(), `id="pos-page"`) {
			t.Fatalf("missing app at %s", path)
		}
	}
	if strings.TrimSpace(request("GET", "/api/products", "", 200).Body.String()) != "[]" {
		t.Fatal("new catalogs must be empty")
	}
	body := `{"name":"Bread & butter","category":"Custom bakery","price":12550,"cost":7550,"stock":8,"reorderLevel":2,"art":"box","color":"#aabbcc"}`
	w := request("POST", "/api/products", body, 201)
	var created struct{ ID int64 }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	var stock, movements int
	db.QueryRow(`SELECT stock FROM products WHERE id=$1`, created.ID).Scan(&stock)
	db.QueryRow(`SELECT count(*) FROM inventory_movements WHERE product_id=$1 AND created_by=$2 AND quantity=8`, created.ID, userID).Scan(&movements)
	if stock != 8 || movements != 1 {
		t.Fatalf("opening stock=%d movements=%d", stock, movements)
	}
	path := fmt.Sprintf("/api/products/%d", created.ID)
	request("PUT", path, strings.ReplaceAll(body, `"stock":8`, `"stock":0`), 200)
	request("PUT", path, body, 400)
	request("POST", "/api/session/open", `{"openingCash":10000}`, 201)
	request("POST", "/api/checkout", fmt.Sprintf(`{"items":[{"productId":%d,"quantity":1}],"payment":"cash"}`, created.ID), 201)
	request("DELETE", path, "", 200)
	request("PUT", path, strings.ReplaceAll(body, `"stock":8`, `"stock":0`), 404)
	request("POST", "/api/checkout", fmt.Sprintf(`{"items":[{"productId":%d,"quantity":1}],"payment":"cash"}`, created.ID), 400)
	request("POST", "/api/purchases", fmt.Sprintf(`{"supplier":"Test","items":[{"productId":%d,"quantity":1,"unitCost":100}]}`, created.ID), 400)
	request("POST", "/api/inventory/adjustments", fmt.Sprintf(`{"productId":%d,"quantity":1,"note":"test"}`, created.ID), 404)
	var savedSales []Sale
	if err := json.Unmarshal(request("GET", "/api/sales", "", 200).Body.Bytes(), &savedSales); err != nil {
		t.Fatal(err)
	}
	if len(savedSales) != 1 || !strings.Contains(savedSales[0].Items, "Bread & butter") {
		t.Fatal("sale history lost")
	}
	request("POST", "/api/products", body, 201)
	request("DELETE", "/api/products", `{"confirmation":"no"}`, 400)
	request("DELETE", "/api/products", `{"confirmation":"DELETE ALL PRODUCTS"}`, 200)
	if err = runMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(request("GET", "/api/products", "", 200).Body.String()) != "[]" {
		t.Fatal("deleted products returned")
	}
	if _, err = db.Exec(`UPDATE users SET role='cashier' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/products", body, 403)
	request("PUT", path, body, 403)
	request("DELETE", path, "", 403)
	request("DELETE", "/api/products", `{"confirmation":"DELETE ALL PRODUCTS"}`, 403)
	request("GET", "/api/products", "", 200)
}
