package main

import (
	"context"
	"database/sql"
	"fmt"
)

// initializeFinancialAccounts ensures the store's empty business cash accounts
// exist inside the caller's transaction. It never changes an existing balance
// and never creates ledger entries, capital, or register allocations.
func initializeFinancialAccounts(ctx context.Context, tx *sql.Tx) error {
	var currency string
	if err := tx.QueryRowContext(ctx,
		`SELECT currency FROM store_settings WHERE id=1 FOR SHARE`,
	).Scan(&currency); err != nil {
		return fmt.Errorf("read store currency: %w", err)
	}

	for _, accountType := range []string{"central_cash", "investment_cash", "bank"} {
		if _, err := getOrCreateFinancialAccount(
			ctx, tx, accountType, nil, currency,
		); err != nil {
			return fmt.Errorf("create %s account: %w", accountType, err)
		}
	}

	return nil
}

func ensureFinancialAccounts(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := initializeFinancialAccounts(ctx, tx); err != nil {
		return err
	}

	return tx.Commit()
}
