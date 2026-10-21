package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

func (s *Server) superadminCashTransfer(w http.ResponseWriter, r *http.Request) {
	actor := principal(r)
	var req struct {
		RequestID            string `json:"requestId"`
		SourceAccountID      int64  `json:"sourceAccountId"`
		DestinationAccountID int64  `json:"destinationAccountId"`
		Amount               int64  `json:"amount"`
		Remarks              string `json:"remarks"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Remarks = strings.TrimSpace(req.Remarks)
	if !requestIDPattern.MatchString(req.RequestID) ||
		req.SourceAccountID <= 0 ||
		req.DestinationAccountID <= 0 ||
		req.SourceAccountID == req.DestinationAccountID ||
		req.Amount <= 0 || req.Amount > 1000000000 ||
		len(req.Remarks) > 500 {
		problem(w, 400, "Invalid cash transfer request")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start transfer")
		return
	}
	defer tx.Rollback()

	replay, hash, err := requestReplay(
		r.Context(), tx, req.RequestID,
		"superadmin-cash-transfer", actor.ID, req,
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

	var role string
	var active bool
	err = tx.QueryRowContext(r.Context(),
		`SELECT role,active FROM users WHERE id=$1 FOR SHARE`,
		actor.ID,
	).Scan(&role, &active)
	if err != nil || role != "superadmin" || !active {
		problem(w, 403, "Active superadmin required")
		return
	}

	if err = auditActor(r.Context(), tx, actor); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}

	var sourceType, destinationType string
	var sourceCurrency, destinationCurrency string
	var sourceOwner, destinationOwner sql.NullInt64

	err = tx.QueryRowContext(r.Context(), `
        SELECT account_type,currency,user_id
        FROM financial_accounts WHERE id=$1
    `, req.SourceAccountID).Scan(
		&sourceType, &sourceCurrency, &sourceOwner,
	)
	if err != nil {
		problem(w, 404, "Source account not found")
		return
	}

	err = tx.QueryRowContext(r.Context(), `
        SELECT account_type,currency,user_id
        FROM financial_accounts WHERE id=$1
    `, req.DestinationAccountID).Scan(
		&destinationType, &destinationCurrency, &destinationOwner,
	)
	if err != nil {
		problem(w, 404, "Destination account not found")
		return
	}

	if sourceType != "superadmin_cash" ||
		destinationType != "superadmin_cash" ||
		!sourceOwner.Valid || !destinationOwner.Valid ||
		sourceOwner.Int64 == destinationOwner.Int64 ||
		sourceCurrency != destinationCurrency {
		problem(w, 409, "Only matching-currency superadmin cash accounts can transfer")
		return
	}

	// Both account owners must remain active superadmins.
	var validOwners int
	err = tx.QueryRowContext(r.Context(), `
        SELECT COUNT(*) FROM users
        WHERE id IN ($1,$2) AND role='superadmin' AND active=true
    `, sourceOwner.Int64, destinationOwner.Int64).Scan(&validOwners)
	if err != nil || validOwners != 2 {
		problem(w, 409, "Both account owners must be active superadmins")
		return
	}

	var transferID int64
	err = tx.QueryRowContext(r.Context(), `
        INSERT INTO financial_transfers (
            source_account_id,destination_account_id,
            requested_amount,approved_amount,status,
            remarks,decision_remarks,requested_by,
            reviewed_by,reviewed_at,request_id
        )
        VALUES ($1,$2,$3,$3,'approved',$4,$4,$5,$5,now(),$6)
        RETURNING id
    `, req.SourceAccountID, req.DestinationAccountID,
		req.Amount, req.Remarks, actor.ID, req.RequestID,
	).Scan(&transferID)
	if err != nil {
		problem(w, 409, "Could not record transfer")
		return
	}

	err = transferFinancialBalance(
		r.Context(), tx,
		req.SourceAccountID, req.DestinationAccountID,
		req.Amount, actor.ID, "financial_transfer", transferID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "insufficient") {
			problem(w, 409, "Insufficient source account balance")
		} else if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "Financial account not found")
		} else {
			problem(w, 409, "Transfer could not be completed")
		}
		return
	}

	result := map[string]any{
		"id":     transferID,
		"status": "approved",
		"amount": req.Amount,
	}
	if err = saveRequest(
		r.Context(), tx, req.RequestID,
		"superadmin-cash-transfer", hash, actor.ID, result,
	); err != nil {
		problem(w, 500, "Could not save transfer result")
		return
	}
	if err = tx.Commit(); err != nil {
		problem(w, 500, "Could not commit transfer")
		return
	}
	respond(w, 200, result)
}
