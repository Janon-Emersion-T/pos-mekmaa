package main

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureInvestmentCashAccount creates an empty account for the store currency.
// It never transfers or reconstructs historical capital.
func ensureInvestmentCashAccount(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currency string
	if err := tx.QueryRowContext(ctx,
		`SELECT currency FROM store_settings WHERE id=1 FOR SHARE`,
	).Scan(&currency); err != nil {
		return fmt.Errorf("read store currency: %w", err)
	}

	if _, err := getOrCreateFinancialAccount(
		ctx, tx, "investment_cash", nil, currency,
	); err != nil {
		return fmt.Errorf("create investment cash account: %w", err)
	}

	return tx.Commit()
}
