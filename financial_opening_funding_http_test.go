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
	"testing"
	"time"
)

func TestRegisterOpeningFundingHTTPPostgres(t *testing.T) {
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
		"opening_http_test_%d",
		time.Now().UnixNano(),
	)

	if _, err := root.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec(
			"DROP SCHEMA " + schema + " CASCADE",
		); err != nil {
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

	var adminID int64
	if err := db.QueryRow(`
		SELECT id FROM users
		WHERE role='superadmin' AND active
		LIMIT 1
	`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}

	var currency string
	if err := db.QueryRow(`
		SELECT currency FROM store_settings WHERE id=1
	`).Scan(&currency); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name          string
		sourceBalance int64
		wantStatus    int
	}{
		{"funded opening", 5000, 201},
		{"insufficient funding", 500, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := fmt.Sprintf(
				"opening-http-%d@example.invalid",
				tc.sourceBalance,
			)

			var cashierID int64
			if err := db.QueryRow(`
				INSERT INTO users(
					email,password_hash,role,active
				)
				VALUES($1,'unused','cashier',true)
				RETURNING id
			`, email).Scan(&cashierID); err != nil {
				t.Fatal(err)
			}

			// One central cash account exists per currency.
			// Reuse it while setting each scenario's starting balance.
			var sourceID int64
			if err := db.QueryRow(`
				INSERT INTO financial_accounts(
					account_type,currency,balance
				)
				VALUES('central_cash',$1,$2)
				ON CONFLICT DO NOTHING
				RETURNING id
			`, currency, tc.sourceBalance).
				Scan(&sourceID); err != nil {
				if err != sql.ErrNoRows {
					t.Fatal(err)
				}

				if err := db.QueryRow(`
					SELECT id FROM financial_accounts
					WHERE account_type='central_cash'
					  AND currency=$1
				`, currency).Scan(&sourceID); err != nil {
					t.Fatal(err)
				}

				if _, err := db.Exec(`
					UPDATE financial_accounts
					SET balance=$1,updated_at=now()
					WHERE id=$2
				`, tc.sourceBalance, sourceID); err != nil {
					t.Fatal(err)
				}
			}

			var allocationID int64
			if err := db.QueryRow(`
				INSERT INTO register_cash_allocations(
					user_id,amount,currency,
					assigned_by,assigned_by_email,
					funding_type,funding_account_id
				)
				VALUES(
					$1,1500,$2,
					$3,'admin@example.invalid',
					'new_funds',$4
				)
				RETURNING id
			`, cashierID, currency, adminID, sourceID).
				Scan(&allocationID); err != nil {
				t.Fatal(err)
			}

			cashier := &User{
				ID:    cashierID,
				Email: email,
				Role:  "cashier",
			}

			open := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(
					http.MethodPost,
					"/api/session/open",
					strings.NewReader(`{"openingCash":1500}`),
				)

				r = r.WithContext(context.WithValue(
					r.Context(), userContextKey, cashier,
				))

				w := httptest.NewRecorder()
				s.sessions(w, r)
				return w
			}

			w := open()
			if w.Code != tc.wantStatus {
				t.Fatalf(
					"opening response: got %d want %d: %s",
					w.Code, tc.wantStatus, w.Body.String(),
				)
			}

			var balance int64
			if err := db.QueryRow(`
				SELECT balance FROM financial_accounts
				WHERE id=$1
			`, sourceID).Scan(&balance); err != nil {
				t.Fatal(err)
			}

			var ledgerCount int
			if err := db.QueryRow(`
				SELECT COUNT(*)
				FROM financial_ledger_entries
				WHERE reference_type='register_cash_allocation'
				  AND reference_id=$1
			`, allocationID).Scan(&ledgerCount); err != nil {
				t.Fatal(err)
			}

			var allocationStatus string
			var posted bool
			if err := db.QueryRow(`
				SELECT status,
				       funding_posted_at IS NOT NULL
				FROM register_cash_allocations
				WHERE id=$1
			`, allocationID).Scan(
				&allocationStatus, &posted,
			); err != nil {
				t.Fatal(err)
			}

			var sessionCount int
			if err := db.QueryRow(`
				SELECT COUNT(*)
				FROM register_sessions
				WHERE opened_by=$1 AND status='open'
			`, cashierID).Scan(&sessionCount); err != nil {
				t.Fatal(err)
			}

			if tc.wantStatus == 201 {
				var cashierBalance int64
				if err := db.QueryRow(`
					SELECT balance
					FROM financial_accounts
					WHERE account_type='staff_cash'
					  AND user_id=$1 AND currency=$2
				`, cashierID, currency).Scan(&cashierBalance); err != nil {
					t.Fatal(err)
				}

				var outgoing, incoming int64
				var authorizedEntries int
				if err := db.QueryRow(`
					SELECT
						COALESCE(SUM(amount) FILTER (
							WHERE entry_type='transfer_out'
						),0),
						COALESCE(SUM(amount) FILTER (
							WHERE entry_type='transfer_in'
						),0),
						COUNT(*) FILTER (WHERE created_by=$2)
					FROM financial_ledger_entries
					WHERE reference_type='register_cash_allocation'
					  AND reference_id=$1
				`, allocationID, adminID).Scan(
					&outgoing, &incoming, &authorizedEntries,
				); err != nil {
					t.Fatal(err)
				}

				if cashierBalance != 1500 ||
					outgoing != -1500 ||
					incoming != 1500 ||
					authorizedEntries != 2 {
					t.Fatalf(
						"incorrect financial posting: cashier=%d "+
							"outgoing=%d incoming=%d adminEntries=%d",
						cashierBalance, outgoing,
						incoming, authorizedEntries,
					)
				}

				if balance != tc.sourceBalance-1500 ||
					ledgerCount != 2 ||
					allocationStatus != "used" ||
					!posted ||
					sessionCount != 1 {
					t.Fatalf(
						"funded opening incorrect: balance=%d "+
							"ledger=%d allocation=%s posted=%v sessions=%d",
						balance, ledgerCount,
						allocationStatus, posted, sessionCount,
					)
				}

				// A second opening must not transfer cash again.
				again := open()
				if again.Code == 201 {
					t.Fatal("duplicate opening succeeded")
				}

				var after int64
				if err := db.QueryRow(`
					SELECT balance FROM financial_accounts
					WHERE id=$1
				`, sourceID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if after != balance {
					t.Fatal("duplicate opening changed source balance")
				}
			} else {
				if balance != tc.sourceBalance ||
					ledgerCount != 0 ||
					allocationStatus != "pending" ||
					posted ||
					sessionCount != 0 {
					t.Fatalf(
						"failed opening was not rolled back: "+
							"balance=%d ledger=%d allocation=%s "+
							"posted=%v sessions=%d",
						balance, ledgerCount,
						allocationStatus, posted, sessionCount,
					)
				}
			}
		})
	}
}
