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

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id,user_id,account_type,currency,balance
		FROM financial_accounts
		WHERE account_type='central_cash'
		   OR (
				account_type='superadmin_cash'
				AND user_id=$1
		   )
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
