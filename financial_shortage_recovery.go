package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func recoverCashShortage(
	ctx context.Context,
	db *sql.DB,
	shortageID, reviewerID, amount int64,
	requestKey, remarks string,
) error {
	requestKey = strings.TrimSpace(requestKey)
	remarks = strings.TrimSpace(remarks)

	if shortageID <= 0 || reviewerID <= 0 || amount <= 0 {
		return errors.New("invalid cash recovery details")
	}
	if requestKey == "" || remarks == "" {
		return errors.New("request key and remarks are required")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var role string
	var active bool

	err = tx.QueryRowContext(ctx, `
		SELECT role,active
		FROM users
		WHERE id=$1
		FOR UPDATE
	`, reviewerID).Scan(&role, &active)
	if err != nil {
		return fmt.Errorf("load reviewer: %w", err)
	}
	if role != "superadmin" || !active {
		return errors.New("only active superadmins can recover shortages")
	}

	var original, recovered, waived int64
	var currency, status string

	err = tx.QueryRowContext(ctx, `
		SELECT original_amount,recovered_amount,
		       waived_amount,currency,status
		FROM cash_shortages
		WHERE id=$1
		FOR UPDATE
	`, shortageID).Scan(
		&original, &recovered, &waived,
		&currency, &status,
	)
	if err != nil {
		return fmt.Errorf("load shortage: %w", err)
	}

	if status == "resolved" {
		return errors.New("shortage already resolved")
	}
	if amount > original-recovered-waived {
		return errors.New("recovery exceeds outstanding shortage")
	}

	account, err := getOrCreateFinancialAccount(
		ctx, tx, "central_cash", nil, currency,
	)
	if err != nil {
		return fmt.Errorf("load receiving account: %w", err)
	}

	var resolutionID int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO cash_shortage_resolutions(
			shortage_id,action,amount,remarks,
			resolved_by,request_key
		)
		VALUES($1,'recover',$2,$3,$4,$5)
		RETURNING id
	`, shortageID, amount, remarks,
		reviewerID, requestKey,
	).Scan(&resolutionID)
	if err != nil {
		return fmt.Errorf("record recovery: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE financial_accounts
		SET balance=balance+$1,
		    updated_at=now()
		WHERE id=$2
	`, amount, account.ID)
	if err != nil {
		return fmt.Errorf("credit receiving account: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO financial_ledger_entries(
			account_id,amount,entry_type,
			reference_type,reference_id,
			remarks,created_by
		)
		VALUES(
			$1,$2,'shortage_recovery',
			'cash_shortage_resolution',$3,$4,$5
		)
	`, account.ID, amount, resolutionID,
		remarks, reviewerID)
	if err != nil {
		return fmt.Errorf("post recovery ledger entry: %w", err)
	}

	recovered += amount
	newStatus := "unresolved"

	if recovered+waived == original {
		newStatus = "resolved"
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE cash_shortages
		SET recovered_amount=$1,status=$2
		WHERE id=$3
	`, recovered, newStatus, shortageID)
	if err != nil {
		return fmt.Errorf("update shortage: %w", err)
	}

	return tx.Commit()
}
