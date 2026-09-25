package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestProductAppearanceValidation(t *testing.T) {
	for _, art := range []string{"", "box", "bottle", "none", "coffee", "iced", "matcha", "tea", "croissant", "cookie", "toast", "sandwich", "cake", "roll"} {
		t.Run(art, func(t *testing.T) {
			p := productInput{Name: "Product", CategoryID: 1, Price: 12550, Cost: 7550, Stock: 8, ReorderLevel: 2, Art: art, Color: "#aabbcc"}
			if !p.validate() {
				t.Fatalf("valid product rejected with appearance %q", art)
			}
		})
	}
}

// Every appearance offered by the form must also be accepted by the API.
func TestProductFormAppearancesAccepted(t *testing.T) {
	source, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`const appearances = (\[[^;]+\]);`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("product form appearance options not found")
	}
	var appearances []string
	if err := json.Unmarshal(match[1], &appearances); err != nil {
		t.Fatal(err)
	}
	if len(appearances) == 0 {
		t.Fatal("product form has no appearance options")
	}
	for _, art := range appearances {
		p := productInput{Name: "Product", CategoryID: 1, Art: art}
		if !p.validate() {
			t.Errorf("form offers appearance %q, but API rejects it", art)
		}
	}
}

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
	setTestBootstrapAdmin(t)
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
	request("POST", "/api/categories", `{"name":"   "}`, 400)
	cw := request("POST", "/api/categories", `{"name":"Custom bakery"}`, 201)
	var originalCategory Category
	if err := json.Unmarshal(cw.Body.Bytes(), &originalCategory); err != nil {
		t.Fatal(err)
	}
	categoryPath := fmt.Sprintf("/api/categories/%d", originalCategory.ID)
	request("POST", "/api/categories", `{"name":" custom BAKERY "}`, 409)
	cw = request("POST", "/api/categories", `{"name":"Other"}`, 201)
	var otherCategory Category
	if err := json.Unmarshal(cw.Body.Bytes(), &otherCategory); err != nil {
		t.Fatal(err)
	}
	request("PUT", fmt.Sprintf("/api/categories/%d", otherCategory.ID), `{"name":"Custom bakery"}`, 409)
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
	request("DELETE", categoryPath, "", 409)
	request("PUT", categoryPath, `{"name":"Renamed bakery"}`, 200)
	var catalog []Product
	if err := json.Unmarshal(request("GET", "/api/products", "", 200).Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Category != "Renamed bakery" || catalog[0].CategoryID != originalCategory.ID {
		t.Fatalf("rename did not propagate: %+v", catalog)
	}
	request("PUT", categoryPath, `{"name":"Custom bakery"}`, 200)
	request("POST", "/api/products", strings.ReplaceAll(body, "Custom bakery", "Missing category"), 400)
	path := fmt.Sprintf("/api/products/%d", created.ID)
	request("PUT", path, strings.ReplaceAll(body, `"stock":8`, `"stock":0`), 200)
	request("PUT", path, body, 400)
	request("POST", "/api/session/open", `{"openingCash":10000}`, 201)
	request("POST", "/api/checkout", fmt.Sprintf(`{"requestId":"product-price-mismatch","items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":12550,"expectedTotal":1}`, created.ID), 409)
	request("POST", "/api/checkout", fmt.Sprintf(`{"requestId":"product-valid-checkout","items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":12550,"expectedTotal":12550}`, created.ID), 201)
	request("DELETE", path, "", 200)
	request("PUT", path, strings.ReplaceAll(body, `"stock":8`, `"stock":0`), 404)
	request("POST", "/api/checkout", fmt.Sprintf(`{"requestId":"product-deleted-checkout","items":[{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":12550}`, created.ID), 400)
	request("POST", "/api/purchases", fmt.Sprintf(`{"supplier":"Test","items":[{"productId":%d,"quantity":1,"unitCost":100}]}`, created.ID), 400)
	request("POST", "/api/inventory/adjustments", fmt.Sprintf(`{"productId":%d,"quantity":1,"note":"test"}`, created.ID), 404)
	var savedSales []Sale
	if err := json.Unmarshal(request("GET", "/api/sales", "", 200).Body.Bytes(), &savedSales); err != nil {
		t.Fatal(err)
	}
	if len(savedSales) != 1 || !strings.Contains(savedSales[0].Items, "Bread & butter") {
		t.Fatal("sale history lost")
	}
	// Sale deletion must be restricted, reversible in inventory, and audited.
	saleID := savedSales[0].ID
	salePath := fmt.Sprintf("/api/sales/%d", saleID)
	deleteBody := fmt.Sprintf(`{"confirmation":"DELETE %d","reason":"Duplicate entry"}`, saleID)
	request("DELETE", salePath, deleteBody, 403)
	if _, err = db.Exec(`UPDATE users SET role='superadmin' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	request("GET", "/api/audit", "", 200)
	request("GET", "/api/reports/daily", "", 200)
	request("DELETE", salePath, `{"confirmation":"DELETE","reason":"Duplicate entry"}`, 400)
	request("DELETE", salePath, fmt.Sprintf(`{"confirmation":"DELETE %d","reason":"   "}`, saleID), 400)
	var before Session
	if err = json.Unmarshal(request("GET", "/api/session", "", 200).Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if before.ExpectedCash != 22550 {
		t.Fatalf("cash before deletion: %d", before.ExpectedCash)
	}
	// Force stock restoration to fail and verify the transaction leaves the sale intact.
	if _, err = db.Exec(`UPDATE products SET stock=2147483647 WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	request("DELETE", salePath, deleteBody, 500)
	var deleted bool
	if err = db.QueryRow(`SELECT deleted_at IS NOT NULL FROM sales WHERE id=$1`, saleID).Scan(&deleted); err != nil || deleted {
		t.Fatalf("failed deletion changed sale: %v %v", deleted, err)
	}
	if _, err = db.Exec(`UPDATE products SET stock=7 WHERE id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	request("DELETE", salePath, deleteBody, 200)
	request("DELETE", salePath, deleteBody, 409)
	var after Session
	if err = json.Unmarshal(request("GET", "/api/session", "", 200).Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.ExpectedCash != 10000 {
		t.Fatalf("cash after deletion: %d", after.ExpectedCash)
	}
	if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, created.ID).Scan(&stock); err != nil || stock != 8 {
		t.Fatalf("restored stock: %d %v", stock, err)
	}
	var auditCount int
	if err = db.QueryRow(`SELECT count(*) FROM sales WHERE id=$1 AND deleted_by=$2 AND deleted_at IS NOT NULL AND deletion_reason='Duplicate entry'`, saleID, userID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("sale audit: %d %v", auditCount, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM inventory_movements WHERE reference_type='sale_deletion' AND reference_id=$1 AND created_by=$2 AND quantity=1`, saleID, userID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("stock audit: %d %v", auditCount, err)
	}
	if strings.TrimSpace(request("GET", "/api/sales", "", 200).Body.String()) != "[]" {
		t.Fatal("deleted sale still listed")
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
	request("DELETE", categoryPath, "", 200)
	request("DELETE", categoryPath, "", 404)
	request("POST", "/api/products", body, 400)
	request("DELETE", fmt.Sprintf("/api/categories/%d", otherCategory.ID), "", 200)
	if strings.TrimSpace(request("GET", "/api/categories", "", 200).Body.String()) != "[]" {
		t.Fatal("deleted categories returned")
	}
	if err = runMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(request("GET", "/api/categories", "", 200).Body.String()) != "[]" {
		t.Fatal("restart recreated categories")
	}
	if _, err = db.Exec(`UPDATE users SET role='cashier' WHERE id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/products", body, 403)
	request("PUT", path, body, 403)
	request("DELETE", path, "", 403)
	request("DELETE", "/api/products", `{"confirmation":"DELETE ALL PRODUCTS"}`, 403)
	request("GET", "/api/products", "", 200)
	request("DELETE", salePath, deleteBody, 403)
	request("GET", "/api/categories", "", 200)
	request("POST", "/api/categories", `{"name":"Denied"}`, 403)
	request("PUT", categoryPath, `{"name":"Denied"}`, 403)
	request("DELETE", categoryPath, "", 403)
}
