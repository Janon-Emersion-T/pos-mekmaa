package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func resolveCashShortage(
	ctx context.Context,
	db *sql.DB,
	shortageID, reviewerID, amount int64,
	action, remarks string,
) error {
	if shortageID <= 0 || reviewerID <= 0 || amount <= 0 {
		return errors.New("invalid shortage resolution details")
	}

	if action != "recover" && action != "waive" {
		return errors.New("invalid shortage resolution action")
	}

	// Cash recovery requires an actual receipt posted to the ledger.
	// Until that workflow is implemented, recovery is not permitted.
	if action == "recover" {
		return errors.New("cash recovery requires verified ledger posting")
	}

	if strings.TrimSpace(remarks) == "" {
		return errors.New("resolution remarks are required")
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
		return errors.New("only active superadmins can resolve shortages")
	}

	var original, recovered, waived int64
	var status string

	err = tx.QueryRowContext(ctx, `
		SELECT original_amount,
		       recovered_amount,
		       waived_amount,
		       status
		FROM cash_shortages
		WHERE id=$1
		FOR UPDATE
	`, shortageID).Scan(
		&original,
		&recovered,
		&waived,
		&status,
	)

	if err != nil {
		return fmt.Errorf("load shortage: %w", err)
	}

	if status == "resolved" {
		return errors.New("shortage already resolved")
	}

	outstanding := original - recovered - waived

	if amount > outstanding {
		return errors.New("resolution exceeds outstanding shortage")
	}

	if action == "recover" {
		recovered += amount
	} else {
		waived += amount
	}

	newStatus := "unresolved"

	if recovered+waived == original {
		newStatus = "resolved"
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO cash_shortage_resolutions(
			shortage_id, action, amount, remarks, resolved_by
		)
		VALUES($1,$2,$3,$4,$5)
	`, shortageID, action, amount, strings.TrimSpace(remarks), reviewerID)
	if err != nil {
		return fmt.Errorf("record shortage resolution: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE cash_shortages
		SET recovered_amount=$1,
		    waived_amount=$2,
		    status=$3
		WHERE id=$4
	`, recovered, waived, newStatus, shortageID)
	if err != nil {
		return fmt.Errorf("update shortage balance: %w", err)
	}

	return tx.Commit()
}
