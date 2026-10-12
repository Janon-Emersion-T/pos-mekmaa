package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type CashShortageBalance struct {
	UserID      int64  `json:"userId"`
	Currency    string `json:"currency"`
	Original    int64  `json:"original"`
	Recovered   int64  `json:"recovered"`
	Waived      int64  `json:"waived"`
	Outstanding int64  `json:"outstanding"`
}

func recordCashShortage(
	ctx context.Context,
	tx *sql.Tx,
	userID, sessionID, expectedCash, countedCash int64,
	currency, remarks string,
) (int64, error) {
	if userID <= 0 || sessionID <= 0 ||
		expectedCash < 0 || countedCash < 0 ||
		strings.TrimSpace(currency) == "" {
		return 0, errors.New("invalid cash shortage details")
	}

	if countedCash >= expectedCash {
		return 0, errors.New("no cash shortage exists")
	}

	amount := expectedCash - countedCash

	var sessionOwner int64
	var sessionCurrency string

	err := tx.QueryRowContext(ctx, `
		SELECT opened_by,currency
		FROM register_sessions
		WHERE id=$1
		FOR UPDATE
	`, sessionID).Scan(&sessionOwner, &sessionCurrency)

	if err != nil {
		return 0, fmt.Errorf("load shortage session: %w", err)
	}

	if sessionOwner != userID || sessionCurrency != currency {
		return 0, errors.New("cash shortage session ownership or currency mismatch")
	}

	var existing bool

	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM cash_shortages
			WHERE session_id=$1
		)
	`, sessionID).Scan(&existing)

	if err != nil {
		return 0, err
	}

	if existing {
		return 0, errors.New("cash shortage already recorded for this session")
	}

	var shortageID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO cash_shortages(
			user_id,
			session_id,
			currency,
			original_amount,
			remarks
		)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id
	`, userID, sessionID, currency, amount,
		strings.TrimSpace(remarks)).Scan(&shortageID)

	if err != nil {
		return 0, err
	}

	return shortageID, nil
}

func getCashShortageBalance(
	ctx context.Context,
	db *sql.DB,
	userID int64,
	currency string,
) (CashShortageBalance, error) {
	if userID <= 0 || strings.TrimSpace(currency) == "" {
		return CashShortageBalance{}, errors.New("invalid shortage balance request")
	}

	result := CashShortageBalance{
		UserID:   userID,
		Currency: currency,
	}

	err := db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(original_amount),0),
			COALESCE(SUM(recovered_amount),0),
			COALESCE(SUM(waived_amount),0),
			COALESCE(SUM(
				original_amount-recovered_amount-waived_amount
			),0)
		FROM cash_shortages
		WHERE user_id=$1 AND currency=$2
	`, userID, currency).Scan(
		&result.Original,
		&result.Recovered,
		&result.Waived,
		&result.Outstanding,
	)

	if err != nil {
		return CashShortageBalance{}, err
	}

	return result, nil
}
