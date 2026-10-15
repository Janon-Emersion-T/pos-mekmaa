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
	"sync"
	"testing"
	"time"
)

func TestOptimizedDebtsConcurrentHTTPCheckout(t *testing.T) {
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

	schema := fmt.Sprintf("optimized_checkout_%d", time.Now().UnixNano())
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

	var currency string
	var adminID int64
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(
		`SELECT id FROM users WHERE role='superadmin' LIMIT 1`,
	).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`
        UPDATE store_settings
        SET optimized_debts_enabled=true WHERE id=1
    `); err != nil {
		t.Fatal(err)
	}

	type fixture struct {
		userID     int64
		productID  int64
		customerID int64
		sessionID  int64
		token      string
	}

	var fixtures [2]fixture
	server := (&Server{db: db}).routes()

	for i := range fixtures {
		f := &fixtures[i]

		if err := db.QueryRow(`
            INSERT INTO users(email,password_hash,role,active)
            VALUES($1,'unused','cashier',true)
            RETURNING id
        `, fmt.Sprintf("optimized-cashier-%d@example.test", i)).
			Scan(&f.userID); err != nil {
			t.Fatal(err)
		}

		if err := db.QueryRow(`
            INSERT INTO customers(name)
            VALUES($1) RETURNING id
        `, fmt.Sprintf("Optimized Customer %d", i)).
			Scan(&f.customerID); err != nil {
			t.Fatal(err)
		}

		// Each product has 10000 potential margin:
		// 10 units * 2000 retail - 10000 inventory cost.
		if err := db.QueryRow(`
            INSERT INTO products(
                name,category,price,cost,stock,inventory_cost_balance
            )
            VALUES($1,'Test',2000,1000,10,10000)
            RETURNING id
        `, fmt.Sprintf("Optimized Product %d", i)).
			Scan(&f.productID); err != nil {
			t.Fatal(err)
		}

		f.token = strings.Repeat(fmt.Sprintf("opt%d", i), 16)
		if _, err := db.Exec(`
            INSERT INTO auth_sessions(user_id,token_hash,expires_at)
            VALUES($1,$2,now()+interval '1 hour')
        `, f.userID, tokenHash(f.token)); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec(`
            INSERT INTO register_cash_allocations(
                user_id,amount,currency,assigned_by,
                assigned_by_email,note
            )
            VALUES($1,0,$2,$3,'bootstrap@example.test','test')
        `, f.userID, currency, adminID); err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequest(
			"POST", "/api/session/open",
			strings.NewReader(`{"openingCash":0}`),
		)
		request.AddCookie(&http.Cookie{
			Name: "counter_session", Value: f.token,
		})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)

		if response.Code != 201 {
			t.Fatalf("open session %d: %d %s",
				i, response.Code, response.Body.String())
		}

		var session struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(
			response.Body.Bytes(), &session,
		); err != nil {
			t.Fatal(err)
		}
		f.sessionID = session.ID
	}

	// Total potential margin is 20000.
	// Each requested credit sale is 12000.
	// Only one sale can fit within the global capacity.
	var responses [2]*httptest.ResponseRecorder
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := range fixtures {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			f := fixtures[i]
			body := fmt.Sprintf(
				`{"requestId":"optimized-concurrent-%d",`+
					`"sessionId":%d,"items":[{"productId":%d,"quantity":6}],`+
					`"payment":"credit","customerId":%d,`+
					`"initialPayment":0,"expectedTotal":12000}`,
				i, f.sessionID, f.productID, f.customerID,
			)

			request := httptest.NewRequest(
				"POST", "/api/checkout", strings.NewReader(body),
			)
			request.AddCookie(&http.Cookie{
				Name: "counter_session", Value: f.token,
			})

			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			responses[i] = response
		}(i)
	}

	close(start)
	wg.Wait()

	success, rejected := 0, 0
	for i, response := range responses {
		switch response.Code {
		case 201:
			success++
		case 409:
			rejected++
		default:
			t.Fatalf(
				"checkout %d returned %d: %s",
				i, response.Code, response.Body.String(),
			)
		}
	}

	if success != 1 || rejected != 1 {
		t.Fatalf(
			"expected one success and one rejection; got %d/%d",
			success, rejected,
		)
	}

	var saleCount int
	if err := db.QueryRow(`
        SELECT COUNT(*) FROM sales WHERE payment='credit'
    `).Scan(&saleCount); err != nil {
		t.Fatal(err)
	}

	if saleCount != 1 {
		t.Fatalf("expected one credit sale, got %d", saleCount)
	}

	var totalOutstanding int64
	if err := db.QueryRow(`
        SELECT COALESCE(SUM(outstanding),0)
        FROM credit_sale_balances
        WHERE deleted_at IS NULL
    `).Scan(&totalOutstanding); err != nil {
		t.Fatal(err)
	}

	if totalOutstanding != 12000 {
		t.Fatalf(
			"outstanding=%d, expected 12000",
			totalOutstanding,
		)
	}

	for i, f := range fixtures {
		var stock int
		if err := db.QueryRow(
			`SELECT stock FROM products WHERE id=$1`,
			f.productID,
		).Scan(&stock); err != nil {
			t.Fatal(err)
		}

		expected := 10
		if responses[i].Code == 201 {
			expected = 4
		}
		if stock != expected {
			t.Fatalf(
				"product %d stock=%d expected=%d",
				i, stock, expected,
			)
		}
	}
}
