package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CapitalEntry struct {
	ID               int64     `json:"id"`
	EntryType        string    `json:"entryType"`
	Amount           int64     `json:"amount"`
	Currency         string    `json:"currency"`
	Reference        string    `json:"reference"`
	Notes            string    `json:"notes"`
	EffectiveDate    time.Time `json:"effectiveDate"`
	CreatedAt        time.Time `json:"createdAt"`
	CreatedBy        int64     `json:"createdBy"`
	CorrectedEntryID *int64    `json:"correctedEntryId,omitempty"`
}

func (s *Server) capitalRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/capital",
		requireRoles(s.listCapital, "superadmin"))
	mux.Handle("POST /api/capital",
		requireRoles(s.createCapital, "superadmin"))
	mux.Handle("POST /api/capital/{id}/correct",
		requireRoles(s.correctCapital, "superadmin"))
}

func (s *Server) listCapital(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id,entry_type,amount,currency,reference,notes,
		       effective_date,created_at,created_by,corrected_entry_id
		FROM capital_entries
		ORDER BY id DESC
		LIMIT 200
	`)
	if err != nil {
		problem(w, 500, "Could not load capital entries")
		return
	}
	defer rows.Close()

	entries := []CapitalEntry{}
	for rows.Next() {
		var entry CapitalEntry
		var corrected sql.NullInt64
		if err := rows.Scan(
			&entry.ID, &entry.EntryType, &entry.Amount,
			&entry.Currency, &entry.Reference, &entry.Notes,
			&entry.EffectiveDate, &entry.CreatedAt,
			&entry.CreatedBy, &corrected,
		); err != nil {
			problem(w, 500, "Could not read capital entries")
			return
		}
		if corrected.Valid {
			id := corrected.Int64
			entry.CorrectedEntryID = &id
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		problem(w, 500, "Could not read capital entries")
		return
	}
	respond(w, 200, entries)
}

// lockCapitalBalance serializes changes to invested capital across accounts.
// Call it before reading the aggregate balance or inserting capital entries.
func lockCapitalBalance(ctx context.Context, tx *sql.Tx, currency string) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(20261015, hashtext($1))`,
		currency,
	); err != nil {
		return 0, err
	}

	var balance int64
	err := tx.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(
            CASE
                WHEN entry_type IN ('contribution','withdrawal') THEN amount
                WHEN entry_type = 'correction'
                     AND corrected_entry_id IN (
                         SELECT id FROM capital_entries
                         WHERE entry_type IN ('contribution','withdrawal')
                     ) THEN amount
                ELSE 0
            END
        ),0)
        FROM capital_entries
        WHERE currency=$1
    `, currency).Scan(&balance)

	return balance, err
}

func (s *Server) createCapital(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID        string `json:"requestId"`
		EntryType        string `json:"entryType"`
		Amount           int64  `json:"amount"`
		FundingAccountID *int64 `json:"fundingAccountId"`
		Reference        string `json:"reference"`
		Notes            string `json:"notes"`
	}
	if !decode(w, r, &req) {
		return
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	if !requestIDPattern.MatchString(req.RequestID) {
		problem(w, 400, "A valid unique requestId is required")
		return
	}

	switch req.EntryType {
	case "contribution", "withdrawal", "reinvestment":
	default:
		problem(w, 400, "Invalid capital entry type")
		return
	}
	if req.Amount <= 0 {
		problem(w, 400, "Amount must be positive")
		return
	}
	if req.EntryType != "reinvestment" &&
		(req.FundingAccountID == nil || *req.FundingAccountID <= 0) {
		problem(w, 400, "Funding account is required")
		return
	}
	if req.EntryType == "reinvestment" && req.FundingAccountID != nil {
		problem(w, 400, "Reinvestment must not credit a funding account")
		return
	}

	if len(req.Reference) > 200 || len(req.Notes) > 2000 {
		problem(w, 400, "Reference or notes too long")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start capital transaction")
		return
	}
	defer tx.Rollback()

	var currency string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT currency FROM store_settings WHERE id=1 FOR SHARE`,
	).Scan(&currency); err != nil {
		problem(w, 500, "Could not load currency")
		return
	}
	replay, requestHash, err := requestReplay(
		r.Context(), tx, req.RequestID,
		"capital-create", principal(r).ID, req,
	)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	if replay != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(replay)
		return
	}

	capitalBalance, err := lockCapitalBalance(r.Context(), tx, currency)
	if err != nil {
		problem(w, 500, "Could not calculate remaining capital")
		return
	}
	if req.EntryType == "withdrawal" && req.Amount > capitalBalance {
		problem(w, 409, "Withdrawal exceeds remaining invested capital")
		return
	}

	if err := auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record audit actor")
		return
	}

	amount := req.Amount
	if req.EntryType == "withdrawal" {
		amount = -amount
	}

	var id int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO capital_entries(
			entry_type,amount,currency,funding_account_id,
			reference,notes,created_by
		)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id
	`, req.EntryType, amount, currency, req.FundingAccountID,
		strings.TrimSpace(req.Reference),
		strings.TrimSpace(req.Notes),
		principal(r).ID,
	).Scan(&id)
	if err != nil {
		problem(w, 500, "Could not record capital entry")
		return
	}
	if req.FundingAccountID != nil {
		var accountType, accountCurrency string
		var accountOwner sql.NullInt64
		var accountBalance int64

		err = tx.QueryRowContext(r.Context(), `
			SELECT account_type,currency,user_id,balance
			FROM financial_accounts
			WHERE id=$1
			FOR UPDATE
		`, *req.FundingAccountID).Scan(
			&accountType, &accountCurrency,
			&accountOwner, &accountBalance,
		)
		if err != nil {
			problem(w, 400, "Invalid funding account")
			return
		}

		allowed := accountType == "bank" ||
			accountType == "central_cash" ||
			accountType == "investment_cash" ||
			(accountType == "superadmin_cash" &&
				accountOwner.Valid &&
				accountOwner.Int64 == principal(r).ID)

		if !allowed || accountCurrency != currency {
			problem(w, 400, "Funding account is not eligible")
			return
		}

		if amount < 0 && accountBalance < -amount {
			problem(w, 409, "Insufficient account balance")
			return
		}

		entryType := "capital_contribution"
		if amount < 0 {
			entryType = "capital_withdrawal"
		}

		if err := postFinancialCashMovement(
			r.Context(), tx, *req.FundingAccountID,
			amount, entryType, "capital_entry",
			id, principal(r).ID,
			strings.TrimSpace(req.Notes),
		); err != nil {
			problem(w, 500, "Could not post capital movement")
			return
		}
	}

	result := map[string]any{"id": id}
	if err := saveRequest(
		r.Context(), tx, req.RequestID,
		"capital-create", requestHash,
		principal(r).ID, result,
	); err != nil {
		problem(w, 500, "Could not save capital request")
		return
	}

	if err := tx.Commit(); err != nil {
		problem(w, 500, "Could not commit capital entry")
		return
	}
	respond(w, 201, map[string]any{"id": id})
}

func (s *Server) correctCapital(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		problem(w, 400, "Invalid capital entry")
		return
	}

	var req struct {
		Notes string `json:"notes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Notes) == "" || len(req.Notes) > 2000 {
		problem(w, 400, "Correction reason is required")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start correction")
		return
	}
	defer tx.Rollback()

	// Serialize all capital mutations before locking individual entries.
	// The store currency is the same currency used for capital accounting.
	var storeCurrency string
	if err := tx.QueryRowContext(r.Context(),
		`SELECT currency FROM store_settings WHERE id=1 FOR SHARE`,
	).Scan(&storeCurrency); err != nil {
		problem(w, 500, "Could not load currency")
		return
	}

	capitalBalance, err := lockCapitalBalance(r.Context(), tx, storeCurrency)
	if err != nil {
		problem(w, 500, "Could not calculate remaining capital")
		return
	}

	var amount int64
	var currency string
	var entryType string
	var fundingAccountID sql.NullInt64

	err = tx.QueryRowContext(r.Context(), `
		SELECT entry_type,amount,currency,funding_account_id
		FROM capital_entries
		WHERE id=$1
		FOR UPDATE
	`, id).Scan(
		&entryType, &amount, &currency, &fundingAccountID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Capital entry not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load capital entry")
		return
	}
	if (entryType == "contribution" || entryType == "withdrawal") &&
		amount > 0 && capitalBalance < amount {
		problem(w, 409, "Correction would exceed remaining invested capital")
		return
	}

	if currency != storeCurrency {
		problem(w, 409, "Capital entry currency differs from store currency")
		return
	}

	if entryType == "correction" {
		problem(w, 409, "A correction cannot be corrected")
		return
	}

	var alreadyCorrected bool
	err = tx.QueryRowContext(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM capital_entries
			WHERE corrected_entry_id=$1
		)
	`, id).Scan(&alreadyCorrected)
	if err != nil {
		problem(w, 500, "Could not check correction")
		return
	}
	if alreadyCorrected {
		problem(w, 409, "This entry has already been corrected")
		return
	}
	if err := auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record audit actor")
		return
	}

	var correctionID int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO capital_entries(
			entry_type,amount,currency,reference,notes,
			created_by,corrected_entry_id
		)
		VALUES('correction',$1,$2,$3,$4,$5,$6)
		RETURNING id
	`, -amount, currency, "Correction",
		strings.TrimSpace(req.Notes), principal(r).ID, id,
	).Scan(&correctionID)
	if err != nil {
		problem(w, 500, "Could not record correction")
		return
	}
	if fundingAccountID.Valid {
		var accountBalance int64
		var accountCurrency string

		err = tx.QueryRowContext(r.Context(), `
			SELECT balance,currency
			FROM financial_accounts
			WHERE id=$1
			FOR UPDATE
		`, fundingAccountID.Int64).Scan(
			&accountBalance, &accountCurrency,
		)
		if err != nil || accountCurrency != currency {
			problem(w, 409, "Original funding account is unavailable")
			return
		}

		reversalAmount := -amount

		if reversalAmount < 0 && accountBalance < -reversalAmount {
			problem(w, 409, "Insufficient funds to reverse contribution")
			return
		}

		if err := postFinancialCashMovement(
			r.Context(), tx, fundingAccountID.Int64,
			reversalAmount, "capital_correction",
			"capital_entry", correctionID,
			principal(r).ID,
			strings.TrimSpace(req.Notes),
		); err != nil {
			problem(w, 500, "Could not reverse capital movement")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		problem(w, 500, "Could not commit correction")
		return
	}
	respond(w, 201, map[string]any{"id": correctionID})
}
