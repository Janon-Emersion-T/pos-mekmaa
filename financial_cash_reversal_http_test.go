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

func TestCashCollectionReversalHTTPPostgres(t *testing.T) {
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

	schema := fmt.Sprintf("cash_reversal_test_%d", time.Now().UnixNano())
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

	var cashierID, adminID, productID int64
	for _, item := range []struct {
		email string
		role  string
		id    *int64
	}{
		{"reversal-cashier@example.invalid", "cashier", &cashierID},
		{"reversal-admin@example.invalid", "admin", &adminID},
	} {
		if err := db.QueryRow(
			`INSERT INTO users(email,password_hash,role,active)
			 VALUES($1,'unused',$2,true) RETURNING id`,
			item.email, item.role,
		).Scan(item.id); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.QueryRow(
		`INSERT INTO products(name,category,price,stock)
		 VALUES('Reversal Product','Test',10000,10)
		 RETURNING id`,
	).Scan(&productID); err != nil {
		t.Fatal(err)
	}

	var currency string
	if err := db.QueryRow(
		`SELECT currency FROM store_settings WHERE id=1`,
	).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		`INSERT INTO register_cash_allocations(
			user_id,amount,currency,assigned_by,assigned_by_email,note
		) VALUES($1,0,$2,$3,'reversal-admin@example.invalid','test')`,
		cashierID, currency, adminID,
	); err != nil {
		t.Fatal(err)
	}

	cashierToken := strings.Repeat("cashreversal", 6)
	adminToken := strings.Repeat("adminreversal", 6)
	for _, item := range []struct {
		id    int64
		token string
	}{
		{cashierID, cashierToken},
		{adminID, adminToken},
	} {
		if _, err := db.Exec(
			`INSERT INTO auth_sessions(user_id,token_hash,expires_at)
			 VALUES($1,$2,now()+interval '1 hour')`,
			item.id, tokenHash(item.token),
		); err != nil {
			t.Fatal(err)
		}
	}

	handler := (&Server{db: db}).routes()
	request := func(token, method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "counter_session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s",
				method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	getID := func(w *httptest.ResponseRecorder) int64 {
		t.Helper()
		var result struct{ ID int64 }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.ID
	}
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var result int64
		if err := db.QueryRow(query, args...).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	sessionID := getID(request(
		cashierToken, "POST", "/api/session/open",
		`{"openingCash":0}`, 201,
	))
	customerID := getID(request(
		cashierToken, "POST", "/api/customers",
		`{"name":"Reversal Customer"}`, 201,
	))

	checkout := fmt.Sprintf(
		`{"requestId":"reversal-credit-checkout","items":[{"productId":%d,"quantity":1}],"payment":"credit","customerId":%d,"initialPayment":2500,"initialPaymentMethod":"cash","sessionId":%d,"expectedTotal":10000}`,
		productID, customerID, sessionID,
	)
	saleID := getID(request(
		cashierToken, "POST", "/api/checkout", checkout, 201,
	))

	var paymentID int64
	if err := db.QueryRow(
		`SELECT id FROM customer_payments WHERE sale_id=$1`,
		saleID,
	).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}

	balance := func() int64 {
		return scalar(
			`SELECT balance FROM financial_accounts
			 WHERE account_type='staff_cash' AND user_id=$1`,
			cashierID,
		)
	}

	if got := balance(); got != 2500 {
		t.Fatalf("before reversal cash balance=%d want=2500", got)
	}

	reversePath := fmt.Sprintf(
		"/api/customer-payments/%d/reverse", paymentID,
	)
	reverseBody := `{"requestId":"reverse-cash-deposit","reason":"Incorrect cash payment"}`

	request(adminToken, "POST", reversePath, reverseBody, 201)

	if got := balance(); got != 0 {
		t.Fatalf("after reversal cash balance=%d want=0", got)
	}
	if got := scalar(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		saleID,
	); got != 10000 {
		t.Fatalf("outstanding=%d want=10000", got)
	}
	if got := scalar(
		`SELECT COUNT(*) FROM financial_ledger_entries
		 WHERE entry_type='cash_collection_reversal'
		   AND reference_type='customer_payment_reversal'
		   AND amount=-2500`,
	); got != 1 {
		t.Fatalf("cash reversal ledger entries=%d want=1", got)
	}

	request(adminToken, "POST", reversePath, reverseBody, 200)
	request(adminToken, "POST", reversePath,
		`{"requestId":"another-reversal","reason":"Duplicate attempt"}`, 409)

	if got := balance(); got != 0 {
		t.Fatalf("duplicate reversal changed cash balance=%d", got)
	}
	if got := scalar(
		`SELECT COUNT(*) FROM customer_payment_reversals WHERE payment_id=$1`,
		paymentID,
	); got != 1 {
		t.Fatalf("payment reversal records=%d want=1", got)
	}

	// Card payments must not affect the cashier's cash account.
	cardCheckout := fmt.Sprintf(
		`{"requestId":"reversal-card-checkout","items":[{"productId":%d,"quantity":1}],"payment":"credit","customerId":%d,"initialPayment":1500,"initialPaymentMethod":"card","sessionId":%d,"expectedTotal":10000}`,
		productID, customerID, sessionID,
	)
	cardSaleID := getID(request(
		cashierToken, "POST", "/api/checkout", cardCheckout, 201,
	))
	var cardPaymentID int64
	if err := db.QueryRow(
		`SELECT id FROM customer_payments WHERE sale_id=$1`,
		cardSaleID,
	).Scan(&cardPaymentID); err != nil {
		t.Fatal(err)
	}
	cardReversePath := fmt.Sprintf(
		"/api/customer-payments/%d/reverse", cardPaymentID,
	)
	request(adminToken, "POST", cardReversePath,
		`{"requestId":"reverse-card-deposit","reason":"Incorrect card payment"}`, 201)

	if got := balance(); got != 0 {
		t.Fatalf("card reversal changed cash balance=%d", got)
	}
	if got := scalar(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		cardSaleID,
	); got != 10000 {
		t.Fatalf("card reversal outstanding=%d want=10000", got)
	}
	if got := scalar(
		`SELECT COUNT(*) FROM financial_ledger_entries
		 WHERE entry_type='cash_collection_reversal'`,
	); got != 1 {
		t.Fatalf("card reversal unexpectedly posted cash; reversal count=%d", got)
	}

	// Simulate a historical cash payment with no financial ledger entry.
	historicalCheckout := fmt.Sprintf(
		`{"requestId":"reversal-historical-checkout","items":[{"productId":%d,"quantity":1}],"payment":"credit","customerId":%d,"initialPayment":1000,"initialPaymentMethod":"cash","sessionId":%d,"expectedTotal":10000}`,
		productID, customerID, sessionID,
	)
	historicalSaleID := getID(request(
		cashierToken, "POST", "/api/checkout", historicalCheckout, 201,
	))
	var historicalPaymentID int64
	if err := db.QueryRow(
		`SELECT id FROM customer_payments WHERE sale_id=$1`,
		historicalSaleID,
	).Scan(&historicalPaymentID); err != nil {
		t.Fatal(err)
	}

	// Remove the posting only in this isolated test database,
	// and restore its corresponding account balance.
	if _, err := db.Exec(
		`DELETE FROM financial_ledger_entries
		 WHERE reference_type='customer_payment'
		   AND reference_id=$1
		   AND entry_type='cash_collection'`,
		historicalPaymentID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`UPDATE financial_accounts SET balance=balance-1000
		 WHERE account_type='staff_cash' AND user_id=$1`,
		cashierID,
	); err != nil {
		t.Fatal(err)
	}

	historicalReversePath := fmt.Sprintf(
		"/api/customer-payments/%d/reverse", historicalPaymentID,
	)
	request(adminToken, "POST", historicalReversePath,
		`{"requestId":"reverse-historical-payment","reason":"Historical payment correction"}`, 201)

	if got := balance(); got != 0 {
		t.Fatalf("historical reversal changed cash balance=%d", got)
	}
	if got := scalar(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		historicalSaleID,
	); got != 10000 {
		t.Fatalf("historical reversal outstanding=%d want=10000", got)
	}
	if got := scalar(
		`SELECT COUNT(*) FROM financial_ledger_entries
		 WHERE entry_type='cash_collection_reversal'`,
	); got != 1 {
		t.Fatalf("historical reversal created unmatched cash entry; count=%d", got)
	}

	// Rollback must preserve the original payment and financial balance.
	rollbackCheckout := fmt.Sprintf(
		`{"requestId":"reversal-rollback-checkout","items":[{"productId":%d,"quantity":1}],"payment":"credit","customerId":%d,"initialPayment":2000,"initialPaymentMethod":"cash","sessionId":%d,"expectedTotal":10000}`,
		productID, customerID, sessionID,
	)
	rollbackSaleID := getID(request(
		cashierToken, "POST", "/api/checkout", rollbackCheckout, 201,
	))
	var rollbackPaymentID int64
	if err := db.QueryRow(
		`SELECT id FROM customer_payments WHERE sale_id=$1`,
		rollbackSaleID,
	).Scan(&rollbackPaymentID); err != nil {
		t.Fatal(err)
	}

	beforeBalance := balance()
	beforeOutstanding := scalar(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		rollbackSaleID,
	)

	if _, err := db.Exec(`
		CREATE FUNCTION reject_test_cash_reversal() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.entry_type='cash_collection_reversal' THEN
				RAISE EXCEPTION 'simulated reversal ledger failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER reject_test_cash_reversal_trigger
		BEFORE INSERT ON financial_ledger_entries
		FOR EACH ROW EXECUTE FUNCTION reject_test_cash_reversal();
	`); err != nil {
		t.Fatal(err)
	}

	rollbackPath := fmt.Sprintf(
		"/api/customer-payments/%d/reverse", rollbackPaymentID,
	)
	rollbackBody := `{"requestId":"reverse-failed-posting","reason":"Rollback verification"}`
	request(adminToken, "POST", rollbackPath, rollbackBody, 500)

	if got := balance(); got != beforeBalance {
		t.Fatalf("failed reversal changed cash balance=%d want=%d", got, beforeBalance)
	}
	if got := scalar(
		`SELECT outstanding FROM credit_sale_balances WHERE id=$1`,
		rollbackSaleID,
	); got != beforeOutstanding {
		t.Fatalf("failed reversal changed debt=%d want=%d", got, beforeOutstanding)
	}
	if got := scalar(
		`SELECT COUNT(*) FROM customer_payment_reversals WHERE payment_id=$1`,
		rollbackPaymentID,
	); got != 0 {
		t.Fatalf("failed reversal persisted reversal record=%d", got)
	}

	if _, err := db.Exec(
		`DROP TRIGGER reject_test_cash_reversal_trigger ON financial_ledger_entries`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP FUNCTION reject_test_cash_reversal()`); err != nil {
		t.Fatal(err)
	}

	// The same request ID must remain usable after rollback.
	request(adminToken, "POST", rollbackPath, rollbackBody, 201)
	if got := balance(); got != beforeBalance-2000 {
		t.Fatalf("retry after rollback cash balance=%d want=%d", got, beforeBalance-2000)
	}

}
