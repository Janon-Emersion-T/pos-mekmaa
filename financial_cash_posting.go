package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var financialCashEntryTypes = map[string]bool{
	"capital_contribution":     true,
	"capital_withdrawal":       true,
	"capital_correction":       true,
	"stock_purchase_payment":   true,
	"cash_sale":                true,
	"cash_collection":          true,
	"cash_collection_reversal": true,
	"petty_cash_in":            true,
	"petty_cash_out":           true,
	"cash_refund":              true,
	"opening_allocation":       true,
}

func postFinancialCashMovement(
	ctx context.Context,
	tx *sql.Tx,
	accountID int64,
	amount int64,
	entryType string,
	referenceType string,
	referenceID int64,
	actorID int64,
	remarks string,
) error {
	if accountID <= 0 || amount == 0 ||
		referenceID <= 0 || actorID <= 0 ||
		!financialCashEntryTypes[entryType] ||
		strings.TrimSpace(referenceType) == "" {
		return errors.New("invalid financial cash posting")
	}

	var currentBalance int64

	err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM financial_accounts
		WHERE id=$1
		FOR UPDATE
	`, accountID).Scan(&currentBalance)

	if err != nil {
		return fmt.Errorf("lock financial account: %w", err)
	}

	// Operational cash balances may become negative.
	// A negative position must be visible for reconciliation.
	if amount > 0 && currentBalance > int64(^uint64(0)>>1)-amount {
		return errors.New("financial balance overflow")
	}
	if amount < 0 && currentBalance < -int64(^uint64(0)>>1)-1-amount {
		return errors.New("financial balance underflow")
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO financial_ledger_entries(
			account_id,amount,entry_type,
			reference_type,reference_id,
			remarks,created_by
		)
		VALUES($1,$2,$3,$4,$5,$6,$7)
	`, accountID, amount, entryType,
		referenceType, referenceID,
		strings.TrimSpace(remarks), actorID)

	if err != nil {
		return fmt.Errorf("insert financial cash ledger: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE financial_accounts
		SET balance=balance+$1,
		    updated_at=now()
		WHERE id=$2
	`, amount, accountID)

	if err != nil {
		return fmt.Errorf("update financial cash balance: %w", err)
	}

	return nil
}
