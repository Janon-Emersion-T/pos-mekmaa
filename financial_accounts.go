package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type FinancialAccount struct {
	ID          int64  `json:"id"`
	UserID      *int64 `json:"userId"`
	AccountType string `json:"accountType"`
	Currency    string `json:"currency"`
	Balance     int64  `json:"balance"`
}

type FinancialAccountBalance struct {
	ID          int64     `json:"id"`
	UserID      *int64    `json:"userId"`
	OwnerEmail  string    `json:"ownerEmail"`
	OwnerRole   string    `json:"ownerRole"`
	AccountType string    `json:"accountType"`
	Currency    string    `json:"currency"`
	Balance     int64     `json:"balance"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (s *Server) financialAccountBalances(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT a.id,a.user_id,COALESCE(u.email,''),COALESCE(u.role,''),
		       a.account_type,a.currency,a.balance,a.updated_at
		FROM financial_accounts a
		LEFT JOIN users u ON u.id=a.user_id
		ORDER BY CASE a.account_type
		  WHEN 'central_cash' THEN 1 WHEN 'investment_cash' THEN 2
		  WHEN 'bank' THEN 3 WHEN 'superadmin_cash' THEN 4 ELSE 5 END,
		  lower(COALESCE(u.email,'')),a.currency,a.id`)
	if err != nil {
		problem(w, http.StatusInternalServerError, "Could not load account balances")
		return
	}
	defer rows.Close()
	out := []FinancialAccountBalance{}
	for rows.Next() {
		var balance FinancialAccountBalance
		if err = rows.Scan(&balance.ID, &balance.UserID, &balance.OwnerEmail,
			&balance.OwnerRole, &balance.AccountType, &balance.Currency,
			&balance.Balance, &balance.UpdatedAt); err != nil {
			problem(w, http.StatusInternalServerError, "Could not read account balances")
			return
		}
		out = append(out, balance)
	}
	if err = rows.Err(); err != nil {
		problem(w, http.StatusInternalServerError, "Could not read account balances")
		return
	}
	respond(w, http.StatusOK, out)
}

func getOrCreateFinancialAccount(
	ctx context.Context,
	tx *sql.Tx,
	accountType string,
	userID *int64,
	currency string,
) (FinancialAccount, error) {
	var a FinancialAccount

	if accountType != "staff_cash" &&
		accountType != "superadmin_cash" &&
		accountType != "central_cash" &&
		accountType != "investment_cash" &&
		accountType != "bank" {
		return a, errors.New("invalid financial account type")
	}

	if currency == "" {
		return a, errors.New("currency is required")
	}

	requiresUser := accountType == "staff_cash" ||
		accountType == "superadmin_cash"

	if requiresUser != (userID != nil) {
		return a, errors.New("invalid financial account owner")
	}

	err := tx.QueryRowContext(ctx, `
		INSERT INTO financial_accounts(
			user_id, account_type, currency
		)
		VALUES($1,$2,$3)
		ON CONFLICT DO NOTHING
		RETURNING id,user_id,account_type,currency,balance
	`, userID, accountType, currency).Scan(
		&a.ID, &a.UserID, &a.AccountType,
		&a.Currency, &a.Balance,
	)

	if err == nil {
		return a, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}

	err = tx.QueryRowContext(ctx, `
		SELECT id,user_id,account_type,currency,balance
		FROM financial_accounts
		WHERE account_type=$1
		  AND currency=$2
		  AND user_id IS NOT DISTINCT FROM $3
	`, accountType, currency, userID).Scan(
		&a.ID, &a.UserID, &a.AccountType,
		&a.Currency, &a.Balance,
	)

	return a, err
}

func transferFinancialBalance(
	ctx context.Context,
	tx *sql.Tx,
	sourceID, destinationID int64,
	amount int64,
	actorID int64,
	referenceType string,
	referenceID int64,
) error {
	if sourceID <= 0 || destinationID <= 0 ||
		sourceID == destinationID || amount <= 0 ||
		actorID <= 0 || referenceType == "" {
		return errors.New("invalid financial transfer")
	}

	// Lock both accounts in stable ID order to avoid deadlocks.
	rows, err := tx.QueryContext(ctx, `
		SELECT id,balance,currency
		FROM financial_accounts
		WHERE id IN ($1,$2)
		ORDER BY id
		FOR UPDATE
	`, sourceID, destinationID)

	if err != nil {
		return err
	}

	type balanceRow struct {
		id       int64
		balance  int64
		currency string
	}

	accounts := make(map[int64]balanceRow)

	for rows.Next() {
		var a balanceRow
		if err = rows.Scan(&a.id, &a.balance, &a.currency); err != nil {
			rows.Close()
			return err
		}
		accounts[a.id] = a
	}

	err = rows.Err()
	rows.Close()

	if err != nil {
		return err
	}

	source, sourceOK := accounts[sourceID]
	destination, destinationOK := accounts[destinationID]

	if !sourceOK || !destinationOK {
		return errors.New("financial account not found")
	}

	if source.currency != destination.currency {
		return errors.New("financial account currency mismatch")
	}

	if source.balance < amount {
		return fmt.Errorf("insufficient account balance")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE financial_accounts
		SET balance=balance-$1,updated_at=now()
		WHERE id=$2
	`, amount, sourceID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE financial_accounts
		SET balance=balance+$1,updated_at=now()
		WHERE id=$2
	`, amount, destinationID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO financial_ledger_entries(
			account_id,amount,entry_type,
			reference_type,reference_id,created_by
		)
		VALUES
			($1,$3,'transfer_out',$5,$6,$4),
			($2,$3 * -1,'transfer_in',$5,$6,$4)
	`, sourceID, destinationID, -amount,
		actorID, referenceType, referenceID)

	return err
}
