package main

import (
	"net/http"
)

func (s *Server) financialFundingSources(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor := principal(r)
	if actor == nil || actor.Role != "superadmin" {
		problem(w, http.StatusForbidden, "Superadmin access required")
		return
	}
	// Self-heal installations that were reset by an older release, which
	// truncated financial_accounts without recreating the funding sources.
	if err := ensureFinancialAccounts(r.Context(), s.db); err != nil {
		problem(w, 500, "Could not initialize funding sources")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not initialize cash account")
		return
	}
	var currency string
	if err = tx.QueryRowContext(r.Context(), `SELECT currency FROM store_settings WHERE id=1 FOR SHARE`).Scan(&currency); err == nil {
		_, err = getOrCreateFinancialAccount(r.Context(), tx, "superadmin_cash", &actor.ID, currency)
	}
	if err != nil || tx.Commit() != nil {
		tx.Rollback()
		problem(w, 500, "Could not initialize cash account")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id,user_id,account_type,currency,balance
		FROM financial_accounts
		WHERE account_type IN (
            'investment_cash',
            'central_cash',
            'bank'
		) OR (account_type='superadmin_cash' AND user_id=$1)
		ORDER BY currency,account_type,id
	`, actor.ID)
	if err != nil {
		problem(w, 500, "Could not load funding sources")
		return
	}
	defer rows.Close()

	accounts := []FinancialAccount{}

	for rows.Next() {
		var account FinancialAccount

		if err := rows.Scan(
			&account.ID,
			&account.UserID,
			&account.AccountType,
			&account.Currency,
			&account.Balance,
		); err != nil {
			problem(w, 500, "Could not read funding sources")
			return
		}

		accounts = append(accounts, account)
	}

	if err := rows.Err(); err != nil {
		problem(w, 500, "Could not read funding sources")
		return
	}

	respond(w, 200, accounts)
}
