package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInventoryValuationIntegrationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("Set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	setTestBootstrapAdmin(t)

	db, err := openDB(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	schema := fmt.Sprintf(
		"inventory_valuation_test_%d",
		time.Now().UnixNano(),
	)

	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP SCHEMA " + schema + " CASCADE")

	if _, err := db.Exec("SET search_path TO " + schema); err != nil {
		t.Fatal(err)
	}

	if err := runMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	var userID int64

	err = db.QueryRow(`
		INSERT INTO users(email,password_hash,role)
		VALUES('valuation@example.test','unused','admin')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	token := strings.Repeat("valuation-test-", 4)

	_, err = db.Exec(`
		INSERT INTO auth_sessions(user_id,token_hash,expires_at)
		VALUES($1,$2,now()+interval '1 hour')
	`, userID, tokenHash(token))
	if err != nil {
		t.Fatal(err)
	}

	server := (&Server{db: db}).routes()

	request := func(method, path, body string, expected int) []byte {
		t.Helper()

		r := httptest.NewRequest(
			method,
			path,
			strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{
			Name:  "counter_session",
			Value: token,
		})

		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)

		if w.Code != expected {
			t.Fatalf(
				"%s %s: got %d want %d: %s",
				method,
				path,
				w.Code,
				expected,
				w.Body.String(),
			)
		}

		return w.Body.Bytes()
	}

	// Product creation requires an existing category.
	request(
		"POST",
		"/api/categories",
		`{"name":"Valuation Test Category"}`,
		201,
	)

	var product struct {
		ID int64 `json:"id"`
	}

	err = json.Unmarshal(
		request(
			"POST",
			"/api/products",
			`{"name":"Valuation Test Product","category":"Valuation Test Category","price":200,"cost":100,"stock":0}`,
			201,
		),
		&product,
	)
	if err != nil {
		t.Fatal(err)
	}

	purchase := func(key string, qty, cost int) {
		t.Helper()

		body := fmt.Sprintf(
			`{"requestId":%q,"supplier":"Valuation Test Supplier","items":[{"productId":%d,"quantity":%d,"unitCost":%d}]}`,
			key,
			product.ID,
			qty,
			cost,
		)

		request("POST", "/api/purchases", body, 201)
	}

	checkInventory := func(wantStock int64, wantValue int64) {
		t.Helper()

		var stock int64
		var value sql.NullInt64

		err := db.QueryRow(`
			SELECT stock,inventory_cost_balance
			FROM products WHERE id=$1
		`, product.ID).Scan(&stock, &value)

		if err != nil {
			t.Fatal(err)
		}

		if stock != wantStock ||
			!value.Valid ||
			value.Int64 != wantValue {
			t.Fatalf(
				"inventory stock=%d value=%v; want stock=%d value=%d",
				stock,
				value,
				wantStock,
				wantValue,
			)
		}
	}

	t.Run("first purchase", func(t *testing.T) {
		purchase("valuation-first-purchase", 10, 100)
		checkInventory(10, 1000)
	})

	t.Run("second purchase weighted average", func(t *testing.T) {
		purchase("valuation-second-purchase", 10, 150)
		checkInventory(20, 2500)
	})

	_, err = db.Exec(`
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,
			assigned_by,assigned_by_email,note
		)
		VALUES(
			$1,1000,
			(SELECT currency FROM store_settings WHERE id=1),
			$1,'valuation@example.test','Valuation test'
		)
	`, userID)
	if err != nil {
		t.Fatal(err)
	}

	request(
		"POST",
		"/api/session/open",
		`{"openingCash":1000}`,
		201,
	)

	var sale struct {
		ID int64 `json:"id"`
	}

	t.Run("checkout records exact historical COGS", func(t *testing.T) {
		body := fmt.Sprintf(
			`{"requestId":"valuation-checkout-0001","items":[{"productId":%d,"quantity":5}],"payment":"cash","cashReceived":1000,"expectedTotal":1000}`,
			product.ID,
		)

		if err := json.Unmarshal(
			request("POST", "/api/checkout", body, 201),
			&sale,
		); err != nil {
			t.Fatal(err)
		}

		checkInventory(15, 1875)

		var cost sql.NullInt64
		var method sql.NullString

		err := db.QueryRow(`
			SELECT total_cost_snapshot,cost_method
			FROM sale_items
			WHERE sale_id=$1
		`, sale.ID).Scan(&cost, &method)

		if err != nil {
			t.Fatal(err)
		}

		if !cost.Valid || cost.Int64 != 625 ||
			!method.Valid || method.String != "weighted_average" {
			t.Fatalf("COGS=%v method=%v", cost, method)
		}
	})

	t.Run("valuation ledger reconciles", func(t *testing.T) {
		var quantity, value int64

		err := db.QueryRow(`
			SELECT
				COALESCE(SUM(quantity_change),0),
				COALESCE(SUM(value_change),0)
			FROM inventory_valuation_entries
			WHERE product_id=$1
		`, product.ID).Scan(&quantity, &value)

		if err != nil {
			t.Fatal(err)
		}

		if quantity != 15 || value != 1875 {
			t.Fatalf(
				"ledger quantity=%d value=%d; want 15 and 1875",
				quantity,
				value,
			)
		}
	})

	t.Run("later purchase does not change historical COGS", func(t *testing.T) {
		purchase("valuation-third-purchase", 5, 300)
		checkInventory(20, 3375)

		var cost int64
		err := db.QueryRow(`
			SELECT total_cost_snapshot
			FROM sale_items
			WHERE sale_id=$1
		`, sale.ID).Scan(&cost)

		if err != nil {
			t.Fatal(err)
		}

		if cost != 625 {
			t.Fatalf("historical COGS changed to %d", cost)
		}
	})

	var saleItemID int64

	err = db.QueryRow(
		`SELECT id FROM sale_items WHERE sale_id=$1`,
		sale.ID,
	).Scan(&saleItemID)

	if err != nil {
		t.Fatal(err)
	}

	refund := func(key string, quantity int, restock bool, expected int) {
		t.Helper()

		body := fmt.Sprintf(
			`{"requestId":%q,"reason":"Historical valuation integration test","confirmed":true,"items":[{"saleItemId":%d,"quantity":%d,"restock":%t}]}`,
			key,
			saleItemID,
			quantity,
			restock,
		)

		request(
			"POST",
			fmt.Sprintf("/api/sales/%d/refunds", sale.ID),
			body,
			expected,
		)
	}

	checkRefundCost := func(wantQuantity, wantCost int64) {
		t.Helper()

		var quantity, cost int64

		err := db.QueryRow(`
			SELECT
				COALESCE(SUM(quantity),0),
				COALESCE(SUM(historical_cost_allocated),0)
			FROM refund_items
			WHERE sale_item_id=$1
		`, saleItemID).Scan(&quantity, &cost)

		if err != nil {
			t.Fatal(err)
		}

		if quantity != wantQuantity || cost != wantCost {
			t.Fatalf(
				"refund quantity=%d cost=%d; want quantity=%d cost=%d",
				quantity, cost, wantQuantity, wantCost,
			)
		}
	}

	t.Run("partial refund restores historical COGS", func(t *testing.T) {
		refund("valuation-refund-first-0001", 2, true, 201)
		checkInventory(22, 3625)
		checkRefundCost(2, 250)
	})

	t.Run("non-restocked refund does not increase inventory", func(t *testing.T) {
		refund("valuation-refund-second-0001", 1, false, 201)
		checkInventory(22, 3625)
		checkRefundCost(3, 375)
	})

	t.Run("final refund restores exact remaining cost", func(t *testing.T) {
		refund("valuation-refund-third-0001", 2, true, 201)
		checkInventory(24, 3875)
		checkRefundCost(5, 625)
	})

	t.Run("over-refund is rejected without changing valuation", func(t *testing.T) {
		refund("valuation-refund-excess-0001", 1, true, 409)
		checkInventory(24, 3875)
		checkRefundCost(5, 625)
	})

	t.Run("refund valuation ledger reconciles", func(t *testing.T) {
		var quantity, value int64

		err := db.QueryRow(`
			SELECT
				COALESCE(SUM(quantity_change),0),
				COALESCE(SUM(value_change),0)
			FROM inventory_valuation_entries
			WHERE product_id=$1
		`, product.ID).Scan(&quantity, &value)

		if err != nil {
			t.Fatal(err)
		}

		if quantity != 24 || value != 3875 {
			t.Fatalf(
				"ledger quantity=%d value=%d; want 24 and 3875",
				quantity, value,
			)
		}
	})

	t.Run("sale deletion restores historical COGS", func(t *testing.T) {
		// The earlier sale has refunds and must remain undeletable.
		refundedSalePath := fmt.Sprintf("/api/sales/%d", sale.ID)

		request(
			"DELETE",
			refundedSalePath,
			fmt.Sprintf(
				`{"confirmation":"DELETE %d","reason":"Refunded sale must remain"}`,
				sale.ID,
			),
			403,
		)

		// Deletion requires a superadmin.
		if _, err := db.Exec(
			`UPDATE users SET role='superadmin' WHERE id=$1`,
			userID,
		); err != nil {
			t.Fatal(err)
		}

		request(
			"DELETE",
			refundedSalePath,
			fmt.Sprintf(
				`{"confirmation":"DELETE %d","reason":"Refunded sale must remain"}`,
				sale.ID,
			),
			409,
		)

		// Create a fresh, fully unpaid credit sale so deletion
		// cannot interfere with completed cash collections.
		var customer struct {
			ID int64 `json:"id"`
		}

		customerBody := request(
			"POST",
			"/api/customers",
			`{"name":"Valuation Deletion Customer","phone":"0770000199"}`,
			201,
		)

		if err := json.Unmarshal(customerBody, &customer); err != nil {
			t.Fatal(err)
		}

		var deletionSale struct {
			ID int64 `json:"id"`
		}

		checkoutBody := fmt.Sprintf(
			`{"requestId":"valuation-delete-checkout-0001","customerId":%d,"items":[{"productId":%d,"quantity":2}],"payment":"credit","initialPayment":0,"initialPaymentMethod":"cash","expectedTotal":400}`,
			customer.ID,
			product.ID,
		)

		if err := json.Unmarshal(
			request("POST", "/api/checkout", checkoutBody, 201),
			&deletionSale,
		); err != nil {
			t.Fatal(err)
		}

		var paymentCount int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM customer_payments WHERE sale_id=$1`,
			deletionSale.ID,
		).Scan(&paymentCount); err != nil {
			t.Fatal(err)
		}
		if paymentCount != 0 {
			t.Fatalf("unpaid sale unexpectedly has %d payments", paymentCount)
		}

		var originalCost sql.NullInt64

		if err := db.QueryRow(
			`SELECT SUM(total_cost_snapshot)
			 FROM sale_items WHERE sale_id=$1`,
			deletionSale.ID,
		).Scan(&originalCost); err != nil {
			t.Fatal(err)
		}

		if !originalCost.Valid {
			t.Fatal("New sale has no verified historical COGS")
		}

		var stockBefore int64
		var valueBefore int64

		if err := db.QueryRow(
			`SELECT stock,inventory_cost_balance
			 FROM products WHERE id=$1`,
			product.ID,
		).Scan(&stockBefore, &valueBefore); err != nil {
			t.Fatal(err)
		}

		deletePath := fmt.Sprintf("/api/sales/%d", deletionSale.ID)
		deleteBody := fmt.Sprintf(
			`{"confirmation":"DELETE %d","reason":"Valuation reversal test"}`,
			deletionSale.ID,
		)

		request("DELETE", deletePath, deleteBody, 200)

		checkInventory(
			stockBefore+2,
			valueBefore+originalCost.Int64,
		)

		var reversalQuantity, reversalValue int64

		if err := db.QueryRow(
			`SELECT
				COALESCE(SUM(quantity_change),0),
				COALESCE(SUM(value_change),0)
			 FROM inventory_valuation_entries
			 WHERE movement_type='sale_reversal'
			   AND reference_type='sale_deletion'
			   AND reference_id=$1`,
			deletionSale.ID,
		).Scan(&reversalQuantity, &reversalValue); err != nil {
			t.Fatal(err)
		}

		if reversalQuantity != 2 ||
			reversalValue != originalCost.Int64 {
			t.Fatalf(
				"reversal quantity=%d value=%d; want 2 and %d",
				reversalQuantity,
				reversalValue,
				originalCost.Int64,
			)
		}

		request("DELETE", deletePath, deleteBody, 409)

		checkInventory(
			stockBefore+2,
			valueBefore+originalCost.Int64,
		)
	})

	t.Run("duplicate product lines consolidate correctly", func(t *testing.T) {
		var beforeStock, beforeValue int64

		if err := db.QueryRow(
			`SELECT stock,inventory_cost_balance FROM products WHERE id=$1`,
			product.ID,
		).Scan(&beforeStock, &beforeValue); err != nil {
			t.Fatal(err)
		}

		issueCost, err := weightedAverageIssueValue(
			beforeStock, beforeValue, 3,
		)
		if err != nil {
			t.Fatal(err)
		}

		var duplicateSale struct {
			ID int64 `json:"id"`
		}

		body := fmt.Sprintf(
			`{"requestId":"valuation-duplicate-checkout-0001","items":[{"productId":%d,"quantity":1},{"productId":%d,"quantity":2}],"payment":"cash","cashReceived":600,"expectedTotal":600}`,
			product.ID,
			product.ID,
		)

		if err := json.Unmarshal(
			request("POST", "/api/checkout", body, 201),
			&duplicateSale,
		); err != nil {
			t.Fatal(err)
		}

		var lineCount, quantity, cost int64

		if err := db.QueryRow(
			`SELECT COUNT(*),COALESCE(SUM(quantity),0),
			        COALESCE(SUM(total_cost_snapshot),0)
			 FROM sale_items WHERE sale_id=$1`,
			duplicateSale.ID,
		).Scan(&lineCount, &quantity, &cost); err != nil {
			t.Fatal(err)
		}

		if lineCount != 1 || quantity != 3 || cost != issueCost {
			t.Fatalf(
				"sale lines=%d quantity=%d COGS=%d; want 1, 3, %d",
				lineCount, quantity, cost, issueCost,
			)
		}

		checkInventory(
			beforeStock-3,
			beforeValue-issueCost,
		)
	})

	t.Run("duplicate lines cannot oversell inventory", func(t *testing.T) {
		var beforeStock, beforeValue int64

		if err := db.QueryRow(
			`SELECT stock,inventory_cost_balance FROM products WHERE id=$1`,
			product.ID,
		).Scan(&beforeStock, &beforeValue); err != nil {
			t.Fatal(err)
		}

		body := fmt.Sprintf(
			`{"requestId":"valuation-duplicate-oversell-0001","items":[{"productId":%d,"quantity":%d},{"productId":%d,"quantity":1}],"payment":"cash","cashReceived":10000}`,
			product.ID,
			beforeStock,
			product.ID,
		)

		request("POST", "/api/checkout", body, 409)

		checkInventory(beforeStock, beforeValue)

		var count int64

		if err := db.QueryRow(
			`SELECT COUNT(*) FROM sales
			 WHERE id IN (
			   SELECT sale_id FROM sale_items
			   WHERE product_id=$1
			 ) AND deleted_at IS NULL`,
			product.ID,
		).Scan(&count); err != nil {
			t.Fatal(err)
		}

		if count < 1 {
			t.Fatal("Existing legitimate sales unexpectedly disappeared")
		}
	})

	t.Run("historical COGS cannot be modified", func(t *testing.T) {
		_, err := db.Exec(`
			UPDATE sale_items
			SET total_cost_snapshot=1
			WHERE sale_id=$1
		`, sale.ID)

		if err == nil {
			t.Fatal("Historical COGS modification was accepted")
		}
	})
}
