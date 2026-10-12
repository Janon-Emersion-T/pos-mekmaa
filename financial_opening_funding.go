package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func postRegisterOpeningFunding(
	ctx context.Context,
	tx *sql.Tx,
	allocationID int64,
	sessionID int64,
	cashierID int64,
	actorID int64,
) error {
	if allocationID <= 0 || sessionID <= 0 ||
		cashierID <= 0 || actorID <= 0 {
		return errors.New("invalid opening funding request")
	}

	var ownerID, assignedBy, amount int64
	var currency, fundingType, status string
	var fundingAccountID, reconciliationID sql.NullInt64
	var postedAt sql.NullTime

	err := tx.QueryRowContext(ctx, `
		SELECT user_id,assigned_by,amount,currency,
		       funding_type,funding_account_id,
		       funding_posted_at,status,reconciliation_id
		FROM register_cash_allocations
		WHERE id=$1
		FOR UPDATE
	`, allocationID).Scan(
		&ownerID, &assignedBy, &amount, &currency,
		&fundingType, &fundingAccountID,
		&postedAt, &status, &reconciliationID,
	)
	if err != nil {
		return err
	}

	if ownerID != cashierID || status != "pending" {
		return errors.New("invalid or already used cash allocation")
	}

	if postedAt.Valid {
		return errors.New("opening cash allocation already posted")
	}

	var sessionOwner int64
	var sessionCurrency, sessionStatus string
	var sessionOpeningCash int64

	err = tx.QueryRowContext(ctx, `
		SELECT opened_by,currency,status,opening_cash
		FROM register_sessions
		WHERE id=$1
		FOR UPDATE
	`, sessionID).Scan(
		&sessionOwner, &sessionCurrency,
		&sessionStatus, &sessionOpeningCash,
	)
	if err != nil {
		return err
	}

	if sessionOwner != cashierID ||
		sessionStatus != "open" ||
		sessionCurrency != currency ||
		sessionOpeningCash != amount {
		return errors.New("opening funding does not match register session")
	}

	switch fundingType {
	case "legacy_unclassified":
		if fundingAccountID.Valid {
			return errors.New("non-funded allocation cannot specify a funding account")
		}
		return nil

	case "carry_forward":
		if fundingAccountID.Valid || !reconciliationID.Valid {
			return errors.New("carry-forward allocation requires a reconciliation only")
		}

		var recUserID int64
		var recCurrency, recStatus, sourceSessionStatus string
		err = tx.QueryRowContext(ctx, `
			SELECT r.user_id,r.currency,r.status,s.status
			FROM register_cash_reconciliations r
			JOIN register_sessions s ON s.id=r.session_id
			WHERE r.id=$1
			FOR UPDATE OF r
		`, reconciliationID.Int64).Scan(
			&recUserID, &recCurrency, &recStatus, &sourceSessionStatus,
		)
		if err != nil {
			return err
		}
		if recStatus != "verified" ||
			sourceSessionStatus != "closed" ||
			recUserID != cashierID ||
			recCurrency != currency {
			return errors.New("carry-forward reconciliation is no longer eligible")
		}

		var newerReconciliationExists bool
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM register_cash_reconciliations newer
				JOIN register_cash_reconciliations source
				  ON source.id=$1
				WHERE newer.user_id=source.user_id
				  AND newer.currency=source.currency
				  AND newer.session_id>source.session_id
			)
		`, reconciliationID.Int64).Scan(&newerReconciliationExists)
		if err != nil {
			return err
		}
		if newerReconciliationExists {
			return errors.New("carry-forward reconciliation is superseded")
		}
		available, err := currentCarryForwardAvailable(
			ctx, tx, reconciliationID.Int64, &allocationID,
		)
		if err != nil {
			return err
		}
		if available < amount {
			return errors.New("carry-forward cash is no longer available")
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE register_cash_allocations
			SET funding_posted_at=now()
			WHERE id=$1
		`, allocationID)
		return err

	case "new_funds":
		var assignerRole string
		var assignerActive bool

		err = tx.QueryRowContext(ctx, `
			SELECT role,active
			FROM users
			WHERE id=$1
			FOR SHARE
		`, assignedBy).Scan(&assignerRole, &assignerActive)
		if err != nil {
			return err
		}

		if assignerRole != "superadmin" || !assignerActive {
			return errors.New("only an active superadmin can authorize new opening funds")
		}

		if !fundingAccountID.Valid {
			return errors.New("new opening funds require a source account")
		}

		var sourceType, sourceCurrency string
		var sourceOwner sql.NullInt64

		err = tx.QueryRowContext(ctx, `
			SELECT account_type,user_id,currency
			FROM financial_accounts
			WHERE id=$1
		`, fundingAccountID.Int64).Scan(
			&sourceType, &sourceOwner, &sourceCurrency,
		)
		if err != nil {
			return err
		}

		if sourceType != "central_cash" &&
			sourceType != "superadmin_cash" {
			return errors.New("invalid opening funding source")
		}

		if sourceType == "superadmin_cash" &&
			(!sourceOwner.Valid || sourceOwner.Int64 != assignedBy) {
			return errors.New("superadmin cash source owner mismatch")
		}

		if sourceCurrency != currency {
			return errors.New("opening funding currency mismatch")
		}

		if amount == 0 {
			return errors.New("new opening funding amount must be positive")
		}

		account, err := getOrCreateFinancialAccount(
			ctx, tx, "staff_cash", &cashierID, currency,
		)
		if err != nil {
			return fmt.Errorf("create cashier cash account: %w", err)
		}

		if err := transferFinancialBalance(
			ctx, tx,
			fundingAccountID.Int64,
			account.ID,
			amount,
			assignedBy,
			"register_cash_allocation",
			allocationID,
		); err != nil {
			return fmt.Errorf("transfer opening cash: %w", err)
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE register_cash_allocations
			SET funding_posted_at=now()
			WHERE id=$1
		`, allocationID)

		return err

	default:
		return errors.New("unknown opening funding type")
	}
}
