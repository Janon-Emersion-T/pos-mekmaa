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

func TestRegisterCloseAutomaticallyRecordsShortage(t *testing.T) {
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

	schema := fmt.Sprintf(
		"shortage_http_test_%d",
		time.Now().UnixNano(),
	)

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("cleanup test schema: %v", err)
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

	s := &Server{db: db}

	var cashierID int64
	err = db.QueryRow(`
		INSERT INTO users(email,password_hash,role)
		VALUES('shortage-cashier@example.test','unused','cashier')
		RETURNING id
	`).Scan(&cashierID)
	if err != nil {
		t.Fatal(err)
	}

	cashier := &User{
		ID:    cashierID,
		Email: "shortage-cashier@example.test",
		Role:  "cashier",
	}

	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(
			method, path, strings.NewReader(body),
		)
		r = r.WithContext(context.WithValue(
			r.Context(), userContextKey, cashier,
		))

		w := httptest.NewRecorder()
		s.sessions(w, r)
		return w
	}

	_, err = db.Exec(`
		INSERT INTO register_cash_allocations(
			user_id,amount,currency,
			assigned_by,assigned_by_email,note
		)
		VALUES(
			$1,1500,
			(SELECT currency FROM store_settings WHERE id=1),
			$1,$2,'HTTP shortage test allocation'
		)
	`, cashierID, cashier.Email)
	if err != nil {
		t.Fatal(err)
	}

	w := call(
		http.MethodPost,
		"/api/session/open",
		`{"openingCash":1500}`,
	)
	if w.Code != 201 {
		t.Fatalf(
			"opening failed: %d %s",
			w.Code, w.Body.String(),
		)
	}

	var opened struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.ID <= 0 {
		t.Fatal("missing session ID")
	}

	// Simulate Rs. 5,000 of legitimate cash sales.
	// This test focuses on closing; existing checkout tests
	// independently verify sales posting and attribution.
	_, err = db.Exec(`
		INSERT INTO sales(
			session_id,total,payment,currency,
			created_by,cashier_email
		)
		VALUES($1,5000,'cash',
			(SELECT currency FROM store_settings WHERE id=1),
			$2,$3)
	`, opened.ID, cashierID, cashier.Email)
	if err != nil {
		t.Fatal(err)
	}

	// A stale expected amount must not close the register.
	stale := fmt.Sprintf(
		`{"sessionId":%d,"closingCash":6000,`+
			`"expectedCash":1500,"closingReason":"Cash shortage of 500"}`,
		opened.ID,
	)

	w = call(http.MethodPost, "/api/session/close", stale)
	if w.Code != 409 {
		t.Fatalf(
			"stale close: %d %s",
			w.Code, w.Body.String(),
		)
	}

	var count int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM cash_shortages
		WHERE session_id=$1
	`, opened.ID).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf(
			"stale close created shortage: count=%d err=%v",
			count, err,
		)
	}

	// The correct close should record exactly Rs. 500.
	body := fmt.Sprintf(
		`{"sessionId":%d,"closingCash":6000,`+
			`"expectedCash":6500,"closingReason":"Cash shortage of 500"}`,
		opened.ID,
	)

	w = call(http.MethodPost, "/api/session/close", body)
	if w.Code != 200 {
		t.Fatalf(
			"close failed: %d %s",
			w.Code, w.Body.String(),
		)
	}

	var ownerID, amount int64
	var currency, status, remarks string

	err = db.QueryRow(`
		SELECT user_id,original_amount,
		       currency,status,remarks
		FROM cash_shortages
		WHERE session_id=$1
	`, opened.ID).Scan(
		&ownerID, &amount, &currency,
		&status, &remarks,
	)
	if err != nil {
		t.Fatal(err)
	}

	var sessionCurrency, sessionStatus string
	var expected, counted int64

	err = db.QueryRow(`
		SELECT currency,status,
		       closing_expected_cash,closing_cash
		FROM register_sessions
		WHERE id=$1
	`, opened.ID).Scan(
		&sessionCurrency, &sessionStatus,
		&expected, &counted,
	)
	if err != nil {
		t.Fatal(err)
	}

	if ownerID != cashierID ||
		amount != 500 ||
		currency != sessionCurrency ||
		status != "unresolved" ||
		remarks != "Cash shortage of 500" ||
		sessionStatus != "closed" ||
		expected != 6500 ||
		counted != 6000 {
		t.Fatalf(
			"incorrect closure: owner=%d amount=%d currency=%s "+
				"shortageStatus=%s sessionStatus=%s expected=%d counted=%d",
			ownerID, amount, currency,
			status, sessionStatus, expected, counted,
		)
	}

	// Repeating the close must not create another shortage.
	w = call(http.MethodPost, "/api/session/close", body)
	if w.Code != 409 {
		t.Fatalf(
			"duplicate close: %d %s",
			w.Code, w.Body.String(),
		)
	}

	err = db.QueryRow(`
		SELECT COUNT(*) FROM cash_shortages
		WHERE session_id=$1
	`, opened.ID).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one shortage, got %d", count)
	}
}
