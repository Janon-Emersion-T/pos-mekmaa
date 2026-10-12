package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOwnerCostPriceCreditCheckout(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL")
	}
	setTestBootstrapAdmin(t)

	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	schema := fmt.Sprintf("owner_checkout_%d", time.Now().UnixNano())
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

	var ownerID, cashierID, productID int64
	if err := db.QueryRow(`
		SELECT id FROM customers WHERE is_owner=true
	`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		INSERT INTO users(email,password_hash,role)
		VALUES('owner-test-cashier@example.test','unused','cashier')
		RETURNING id
	`).Scan(&cashierID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		INSERT INTO products(name,category,price,cost,stock)
		VALUES('Owner test item','Test',5000,3000,100)
		RETURNING id
	`).Scan(&productID); err != nil {
		t.Fatal(err)
	}

	token := strings.Repeat("owner-test", 8)
	if _, err := db.Exec(`
		INSERT INTO auth_sessions(user_id,token_hash,expires_at)
		VALUES($1,$2,now()+interval '1 hour')
	`, cashierID, tokenHash(token)); err != nil {
		t.Fatal(err)
	}

	h := (&Server{db: db}).routes()

	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s",
				method, path, w.Code, want, w.Body.String())
		}
		return w
	}

	if _, err := db.Exec(`
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,assigned_by,assigned_by_email,note
		) VALUES(
			$1,1000,(SELECT currency FROM store_settings WHERE id=1),
			$1,'owner-test-cashier@example.test','Owner test allocation'
		)
	`, cashierID); err != nil {
		t.Fatal(err)
	}

	var session struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(
		request("POST", "/api/session/open", `{"openingCash":1000}`, 201).Body.Bytes(),
		&session,
	); err != nil {
		t.Fatal(err)
	}

	checkout := func(key, payment string, initial int, method string, total int) string {
		return fmt.Sprintf(
			`{"requestId":%q,"items":[{"productId":%d,"quantity":2}],"payment":%q,"customerId":%d,"initialPayment":%d,"initialPaymentMethod":%q,"sessionId":%d,"expectedTotal":%d}`,
			key, productID, payment, ownerID, initial, method, session.ID, total,
		)
	}

	request("POST", "/api/checkout",
		checkout("owner-cash-invalid", "cash", 0, "", 6000), 400)
	request("POST", "/api/checkout",
		checkout("owner-deposit-invalid", "credit", 1000, "cash", 6000), 400)
	request("POST", "/api/checkout",
		checkout("owner-selling-price-invalid", "credit", 0, "", 10000), 409)

	body := checkout("owner-credit-success", "credit", 0, "", 6000)
	response := request("POST", "/api/checkout", body, 201)

	var sale struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &sale); err != nil {
		t.Fatal(err)
	}

	request("POST", "/api/checkout", body, 200)

	var total, stock, outstanding, payments, items int

	if err := db.QueryRow(
		`SELECT total FROM sales WHERE id=$1`, sale.ID,
	).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT stock FROM products WHERE id=$1`, productID,
	).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`, sale.ID,
	).Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT count(*) FROM customer_payments WHERE sale_id=$1`, sale.ID,
	).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT count(*) FROM sale_items WHERE sale_id=$1 AND unit_price=3000`, sale.ID,
	).Scan(&items); err != nil {
		t.Fatal(err)
	}

	if total != 6000 || stock != 98 || outstanding != 6000 ||
		payments != 0 || items != 1 {
		t.Fatalf(
			"owner checkout: total=%d stock=%d outstanding=%d payments=%d cost-price lines=%d",
			total, stock, outstanding, payments, items,
		)
	}

	// Owner repayments use the ordinary customer-payment system.
	paymentPath := fmt.Sprintf("/api/customers/%d/payments", ownerID)
	paymentBody := func(key string, amount int, method string) string {
		return fmt.Sprintf(
			`{"requestId":%q,"saleId":%d,"sessionId":%d,"amount":%d,"payment":%q,"note":"Owner settlement"}`,
			key, sale.ID, session.ID, amount, method,
		)
	}

	request("POST", paymentPath,
		paymentBody("owner-overpayment-invalid", 6001, "cash"), 409)

	cashBody := paymentBody("owner-cash-settlement", 2000, "cash")
	request("POST", paymentPath, cashBody, 201)
	request("POST", paymentPath, cashBody, 200)

	if err := db.QueryRow(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		sale.ID,
	).Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if outstanding != 4000 {
		t.Fatalf("owner outstanding after cash=%d want=4000", outstanding)
	}

	var cashPaymentID int64
	if err := db.QueryRow(
		`SELECT id FROM customer_payments WHERE sale_id=$1 AND payment='cash'`,
		sale.ID,
	).Scan(&cashPaymentID); err != nil {
		t.Fatal(err)
	}

	var ledgerCount, cashBalance int
	if err := db.QueryRow(`
        SELECT COUNT(*)
        FROM financial_ledger_entries
        WHERE entry_type='cash_collection'
          AND reference_type='customer_payment'
          AND reference_id=$1
          AND amount=2000
    `, cashPaymentID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 1 {
		t.Fatalf("owner cash ledger postings=%d want=1", ledgerCount)
	}

	if err := db.QueryRow(`
        SELECT COALESCE(SUM(balance),0)
        FROM financial_accounts
        WHERE account_type='staff_cash' AND user_id=$1
    `, cashierID).Scan(&cashBalance); err != nil {
		t.Fatal(err)
	}
	if cashBalance != 2000 {
		t.Fatalf("owner cash balance=%d want=2000", cashBalance)
	}

	request("POST", paymentPath,
		paymentBody("owner-duplicate-key-changed", 5000, "card"), 409)

	request("POST", paymentPath,
		paymentBody("owner-card-settlement", 1000, "card"), 201)

	var detail struct {
		Customer Customer          `json:"customer"`
		Sales    []Sale            `json:"sales"`
		Payments []CustomerPayment `json:"payments"`
	}
	if err := json.Unmarshal(
		request("GET", fmt.Sprintf("/api/customers/%d", ownerID), "", 200).Body.Bytes(),
		&detail,
	); err != nil {
		t.Fatal(err)
	}

	if !detail.Customer.IsOwner ||
		len(detail.Sales) != 1 ||
		detail.Sales[0].Outstanding != 3000 ||
		len(detail.Payments) != 2 {
		t.Fatalf("owner settlement history: %+v", detail)
	}

	if err := db.QueryRow(
		`SELECT stock FROM products WHERE id=$1`,
		productID,
	).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 98 {
		t.Fatalf("owner settlement changed stock=%d want=98", stock)
	}

	request("POST", paymentPath,
		paymentBody("owner-settlement-exceeds-remaining", 3001, "cash"), 409)

	request("POST", paymentPath,
		paymentBody("owner-final-settlement", 3000, "card"), 201)

	if err := db.QueryRow(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		sale.ID,
	).Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if outstanding != 0 {
		t.Fatalf("owner final outstanding=%d want=0", outstanding)
	}

	request("POST", paymentPath,
		paymentBody("owner-payment-after-settled", 1, "cash"), 409)

}
