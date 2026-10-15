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
	"sync"
	"testing"
	"time"
)

func TestPurchasePaymentsPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("purchase_payment_test_%d", time.Now().UnixNano())

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
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

	var adminID int64
	var currency string

	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	var accountID int64
	if err := db.QueryRow(`
		INSERT INTO financial_accounts(account_type,currency,balance)
		VALUES('bank',$1,50000)
		RETURNING id
	`, currency).Scan(&accountID); err != nil {
		t.Fatal(err)
	}

	createPurchase := func(total int64) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(`
			INSERT INTO purchases(
				supplier,invoice_number,total,status,created_by
			)
			VALUES('Test Supplier','TEST', $1,'received',$2)
			RETURNING id
		`, total, adminID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	admin := &User{
		ID:     adminID,
		Role:   "superadmin",
		Email:  "bootstrap@example.test",
		Active: true,
	}

	s := &Server{db: db}
	mux := http.NewServeMux()
	s.purchasePaymentRoutes(mux)

	call := func(purchaseID int64, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			http.MethodPost,
			fmt.Sprintf("/api/purchases/%d/payments", purchaseID),
			strings.NewReader(body),
		)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(
			context.WithValue(r.Context(), userContextKey, admin),
		)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	payment := func(id int64, requestID string, amount int64) string {
		return fmt.Sprintf(
			`{"requestId":%q,"financialAccountId":%d,"amount":%d,"fundingType":"other"}`,
			requestID, accountID, amount,
		)
	}

	accountBalance := func() int64 {
		t.Helper()
		var balance int64
		if err := db.QueryRow(`
			SELECT balance FROM financial_accounts WHERE id=$1
		`, accountID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		return balance
	}

	t.Run("partial payment and outstanding balance", func(t *testing.T) {
		id := createPurchase(10000)

		w := call(id, payment(id, "payment-partial-0001", 4000))
		if w.Code != http.StatusCreated {
			t.Fatalf("payment: %d %s", w.Code, w.Body.String())
		}

		var paid, outstanding int64
		if err := db.QueryRow(`
			SELECT paid_amount,outstanding_amount
			FROM purchase_payment_balances
			WHERE purchase_id=$1
		`, id).Scan(&paid, &outstanding); err != nil {
			t.Fatal(err)
		}

		if paid != 4000 || outstanding != 6000 {
			t.Fatalf("paid=%d outstanding=%d", paid, outstanding)
		}
	})

	t.Run("duplicate request does not deduct twice", func(t *testing.T) {
		id := createPurchase(5000)
		body := payment(id, "payment-duplicate-0001", 2000)

		first := call(id, body)
		if first.Code != http.StatusCreated {
			t.Fatalf("first: %d %s", first.Code, first.Body.String())
		}

		before := accountBalance()
		second := call(id, body)

		if second.Code != http.StatusOK {
			t.Fatalf("duplicate: %d %s", second.Code, second.Body.String())
		}

		if after := accountBalance(); after != before {
			t.Fatalf("duplicate changed account: %d -> %d", before, after)
		}
	})

	t.Run("reject altered duplicate request", func(t *testing.T) {
		id := createPurchase(5000)
		original := fmt.Sprintf(
			`{"requestId":"payment-altered-duplicate-0001","financialAccountId":%d,"amount":1000,"fundingType":"other","reference":"ORIGINAL"}`,
			accountID,
		)
		if w := call(id, original); w.Code != http.StatusCreated {
			t.Fatalf("original payment: %d %s", w.Code, w.Body.String())
		}

		before := accountBalance()
		altered := strings.Replace(original, "ORIGINAL", "ALTERED", 1)
		w := call(id, altered)

		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}
		if got := accountBalance(); got != before {
			t.Fatalf("altered retry changed balance: %d -> %d", before, got)
		}
	})

	t.Run("funding allocation failure rolls back payment", func(t *testing.T) {
		id := createPurchase(2000)

		_, err := db.Exec(`
			CREATE FUNCTION reject_purchase_funding_test()
			RETURNS trigger AS $$
			BEGIN
				RAISE EXCEPTION 'Simulated funding allocation failure';
			END;
			$$ LANGUAGE plpgsql;

			CREATE TRIGGER reject_purchase_funding_test_trigger
			BEFORE INSERT ON purchase_funding_allocations
			FOR EACH ROW
			EXECUTE FUNCTION reject_purchase_funding_test();
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_, err := db.Exec(`
				DROP TRIGGER IF EXISTS reject_purchase_funding_test_trigger
				ON purchase_funding_allocations;
				DROP FUNCTION IF EXISTS reject_purchase_funding_test();
			`)
			if err != nil {
				t.Errorf("cleanup funding trigger: %v", err)
			}
		}()

		before := accountBalance()
		w := call(id, payment(id, "payment-funding-rollback-0001", 1000))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
		}

		if got := accountBalance(); got != before {
			t.Fatalf("failed allocation changed balance: %d -> %d", before, got)
		}

		var payments, ledgerEntries int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM purchase_payments WHERE purchase_id=$1
		`, id).Scan(&payments); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`
			SELECT COUNT(*)
			FROM financial_ledger_entries
			WHERE entry_type='stock_purchase_payment'
			  AND reference_id IN (
				SELECT id FROM purchase_payments WHERE purchase_id=$1
			  )
		`, id).Scan(&ledgerEntries); err != nil {
			t.Fatal(err)
		}

		if payments != 0 || ledgerEntries != 0 {
			t.Fatalf("rollback left payments=%d ledger=%d", payments, ledgerEntries)
		}
	})

	t.Run("reject overpayment", func(t *testing.T) {
		id := createPurchase(3000)

		w := call(id, payment(id, "payment-overpay-0001", 4000))
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("reject insufficient funds", func(t *testing.T) {
		id := createPurchase(100000)

		w := call(id, payment(id, "payment-insufficient-0001", 90000))
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("reject invalid funding classification", func(t *testing.T) {
		id := createPurchase(1000)

		body := fmt.Sprintf(
			`{"requestId":"payment-invalid-funding-0001","financialAccountId":%d,"amount":500,"fundingType":"invalid"}`,
			accountID,
		)

		w := call(id, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("concurrent payments cannot exceed purchase total", func(t *testing.T) {
		id := createPurchase(10000)

		var wg sync.WaitGroup
		statuses := make(chan int, 2)

		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				body := payment(
					id,
					fmt.Sprintf("payment-concurrent-%04d", i),
					7000,
				)
				statuses <- call(id, body).Code
			}(i)
		}

		wg.Wait()
		close(statuses)

		success, conflict := 0, 0
		for code := range statuses {
			switch code {
			case http.StatusCreated:
				success++
			case http.StatusConflict:
				conflict++
			default:
				t.Errorf("unexpected status %d", code)
			}
		}

		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d", success, conflict)
		}

		var paid int64
		if err := db.QueryRow(`
			SELECT paid_amount FROM purchase_payment_balances
			WHERE purchase_id=$1
		`, id).Scan(&paid); err != nil {
			t.Fatal(err)
		}

		if paid != 7000 {
			t.Fatalf("paid=%d want=7000", paid)
		}
	})

	t.Run("ledger failure rolls back payment", func(t *testing.T) {
		id := createPurchase(2000)

		_, err := db.Exec(`
			CREATE FUNCTION reject_purchase_payment_test()
			RETURNS trigger AS $$
			BEGIN
				IF NEW.entry_type='stock_purchase_payment' THEN
					RAISE EXCEPTION 'Simulated ledger failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;

			CREATE TRIGGER reject_purchase_payment_test_trigger
			BEFORE INSERT ON financial_ledger_entries
			FOR EACH ROW
			EXECUTE FUNCTION reject_purchase_payment_test();
		`)
		if err != nil {
			t.Fatal(err)
		}

		before := accountBalance()

		w := call(id, payment(id, "payment-rollback-0001", 1000))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
		}

		if after := accountBalance(); after != before {
			t.Fatalf("rollback changed balance: %d -> %d", before, after)
		}

		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM purchase_payments
			WHERE purchase_id=$1
		`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rollback left %d payment records", count)
		}

		if _, err := db.Exec(`
			DROP TRIGGER reject_purchase_payment_test_trigger
			ON financial_ledger_entries;
			DROP FUNCTION reject_purchase_payment_test();
		`); err != nil {
			t.Fatal(err)
		}
	})
}
