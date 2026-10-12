package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type FinancialTransferRequest struct {
	ID                   int64
	SourceAccountID      int64
	DestinationAccountID int64
	RequestedAmount      int64
	ApprovedAmount       *int64
	Status               string
	Remarks              string
}

func requestFinancialTransfer(
	ctx context.Context,
	db *sql.DB,
	sourceID, destinationID, amount, requesterID int64,
	remarks string,
) (int64, error) {
	if sourceID <= 0 || destinationID <= 0 ||
		sourceID == destinationID ||
		amount <= 0 || requesterID <= 0 {
		return 0, errors.New("invalid transfer request")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var sourceType, destinationType string
	var sourceOwner sql.NullInt64
	var sourceCurrency, destinationCurrency string

	err = tx.QueryRowContext(ctx, `
		SELECT account_type,user_id,currency
		FROM financial_accounts
		WHERE id=$1
		FOR UPDATE
	`, sourceID).Scan(
		&sourceType, &sourceOwner, &sourceCurrency,
	)
	if err != nil {
		return 0, err
	}

	err = tx.QueryRowContext(ctx, `
		SELECT account_type,currency
		FROM financial_accounts
		WHERE id=$1
	`, destinationID).Scan(
		&destinationType, &destinationCurrency,
	)
	if err != nil {
		return 0, err
	}

	if sourceType != "staff_cash" ||
		!sourceOwner.Valid ||
		sourceOwner.Int64 != requesterID {
		return 0, errors.New("requester does not own source cash account")
	}

	if destinationType != "superadmin_cash" {
		return 0, errors.New("destination must be a superadmin cash account")
	}

	if sourceCurrency != destinationCurrency {
		return 0, errors.New("transfer currency mismatch")
	}

	var id int64

	err = tx.QueryRowContext(ctx, `
		INSERT INTO financial_transfers(
			source_account_id,
			destination_account_id,
			requested_amount,
			status,
			remarks,
			requested_by
		)
		VALUES($1,$2,$3,'pending',$4,$5)
		RETURNING id
	`, sourceID, destinationID, amount,
		remarks, requesterID).Scan(&id)

	if err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}

	return id, nil
}

func reviewFinancialTransfer(
	ctx context.Context,
	db *sql.DB,
	transferID, reviewerID, approvedAmount int64,
	approve bool,
	decisionRemarks string,
) error {
	if transferID <= 0 || reviewerID <= 0 ||
		approvedAmount < 0 {
		return errors.New("invalid transfer review")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Authorization is enforced inside the same transaction
	// that performs the financial movement.
	var reviewerRole string
	var reviewerActive bool

	err = tx.QueryRowContext(ctx, `
		SELECT role, active
		FROM users
		WHERE id=$1
		FOR UPDATE
	`, reviewerID).Scan(&reviewerRole, &reviewerActive)

	if err != nil {
		return errors.New("financial reviewer not found")
	}

	if reviewerRole != "superadmin" || !reviewerActive {
		return errors.New("only active superadmins can review financial transfers")
	}

	var req FinancialTransferRequest

	err = tx.QueryRowContext(ctx, `
		SELECT id,source_account_id,
		       destination_account_id,
		       requested_amount,status
		FROM financial_transfers
		WHERE id=$1
		FOR UPDATE
	`, transferID).Scan(
		&req.ID,
		&req.SourceAccountID,
		&req.DestinationAccountID,
		&req.RequestedAmount,
		&req.Status,
	)

	if err != nil {
		return err
	}

	if req.Status != "pending" {
		return errors.New("transfer request already reviewed")
	}

	if approve {
		if approvedAmount <= 0 ||
			approvedAmount > req.RequestedAmount {
			return errors.New("invalid approved amount")
		}

		err = transferFinancialBalance(
			ctx, tx,
			req.SourceAccountID,
			req.DestinationAccountID,
			approvedAmount,
			reviewerID,
			"financial_transfer",
			transferID,
		)
		if err != nil {
			return fmt.Errorf("post approved transfer: %w", err)
		}
	} else if approvedAmount != 0 {
		return errors.New("rejected transfer must have zero approved amount")
	}

	status := "rejected"

	if approve {
		status = "approved"
		if approvedAmount < req.RequestedAmount {
			status = "partially_approved"
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE financial_transfers
		SET status=$1,
		    approved_amount=$2,
		    reviewed_by=$3,
		    reviewed_at=now(),
		    decision_remarks=$4
		WHERE id=$5
		  AND status='pending'
	`, status, approvedAmount,
		reviewerID, decisionRemarks, transferID)

	if err != nil {
		return err
	}

	return tx.Commit()
}
