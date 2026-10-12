package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestOpeningFundingSecurityPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx := context.Background()
	setTestBootstrapAdmin(t)
	db, err := openDB(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cases := []struct {
		name        string
		fundingType string
		sourceType  string
		balance     int64
		amount      int64
		deactivate  bool
		wantError   string
	}{
		{
			name:        "insufficient funds",
			fundingType: "new_funds",
			sourceType:  "central_cash",
			balance:     500,
			amount:      1500,
			wantError:   "insufficient",
		},
		{
			name:        "invalid bank source",
			fundingType: "new_funds",
			sourceType:  "bank",
			balance:     5000,
			amount:      1500,
			wantError:   "invalid opening funding source",
		},
		{
			name:        "inactive administrator",
			fundingType: "new_funds",
			sourceType:  "central_cash",
			balance:     5000,
			amount:      1500,
			deactivate:  true,
			wantError:   "active superadmin",
		},
		{
			name:        "zero new funding rejected",
			fundingType: "new_funds",
			sourceType:  "central_cash",
			balance:     5000,
			amount:      0,
			wantError:   "must be positive",
		},
		{
			name:        "carry forward does not create money",
			fundingType: "carry_forward",
			sourceType:  "central_cash",
			balance:     5000,
			amount:      1500,
		},
	}

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()

			var adminID int64
			err = tx.QueryRowContext(ctx, `
				SELECT id FROM users
				WHERE role='superadmin' AND active
				ORDER BY id LIMIT 1
			`).Scan(&adminID)
			if err != nil {
				t.Fatal(err)
			}

			if tc.deactivate {
				_, err = tx.ExecContext(ctx,
					`UPDATE users SET active=false WHERE id=$1`,
					adminID,
				)
				if err != nil {
					t.Fatal(err)
				}
			}

			email := fmt.Sprintf(
				"opening-security-%d@example.invalid",
				index,
			)

			var cashierID int64
			err = tx.QueryRowContext(ctx, `
				INSERT INTO users(email,password_hash,role,active)
				VALUES($1,'test-only-hash','cashier',true)
				RETURNING id
			`, email).Scan(&cashierID)
			if err != nil {
				t.Fatal(err)
			}

			currency := fmt.Sprintf("FUNDSEC%d", index)

			var sourceID int64
			err = tx.QueryRowContext(ctx, `
				INSERT INTO financial_accounts(
					account_type,currency,balance
				)
				VALUES($1,$2,$3)
				RETURNING id
			`, tc.sourceType, currency, tc.balance).
				Scan(&sourceID)
			if err != nil {
				t.Fatal(err)
			}

			var allocationID int64
			var fundingAccount any

			if tc.fundingType == "new_funds" {
				fundingAccount = sourceID
			}

			err = tx.QueryRowContext(ctx, `
				INSERT INTO register_cash_allocations(
					user_id,amount,currency,
					assigned_by,assigned_by_email,
					funding_type,funding_account_id
				)
				VALUES($1,$2,$3,$4,'admin@example.invalid',$5,$6)
				RETURNING id
			`, cashierID, tc.amount, currency,
				adminID, tc.fundingType, fundingAccount).
				Scan(&allocationID)
			if err != nil {
				t.Fatal(err)
			}

			var sessionID int64
			err = tx.QueryRowContext(ctx, `
				INSERT INTO register_sessions(
					opening_cash,opened_by,
					opened_by_email,currency
				)
				VALUES($1,$2,$3,$4)
				RETURNING id
			`, tc.amount, cashierID, email, currency).
				Scan(&sessionID)
			if err != nil {
				t.Fatal(err)
			}

			err = postRegisterOpeningFunding(
				ctx, tx,
				allocationID, sessionID,
				cashierID, cashierID,
			)

			if tc.wantError != "" {
				if err == nil ||
					!strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf(
						"expected error containing %q, got %v",
						tc.wantError, err,
					)
				}
			} else if err != nil {
				t.Fatalf("unexpected funding error: %v", err)
			}

			var balance int64
			err = tx.QueryRowContext(ctx, `
				SELECT balance FROM financial_accounts
				WHERE id=$1
			`, sourceID).Scan(&balance)
			if err != nil {
				t.Fatal(err)
			}

			if balance != tc.balance {
				t.Fatalf(
					"source balance changed: got %d, want %d",
					balance, tc.balance,
				)
			}

			var ledgerCount int
			err = tx.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM financial_ledger_entries
				WHERE reference_type='register_cash_allocation'
				  AND reference_id=$1
			`, allocationID).Scan(&ledgerCount)
			if err != nil {
				t.Fatal(err)
			}

			if ledgerCount != 0 {
				t.Fatalf(
					"unexpected funding ledger entries: %d",
					ledgerCount,
				)
			}
		})
	}
}
