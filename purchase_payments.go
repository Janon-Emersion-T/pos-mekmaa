package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type purchasePaymentRequest struct {
	RequestID          string `json:"requestId"`
	FinancialAccountID int64  `json:"financialAccountId"`
	Amount             int64  `json:"amount"`
	FundingType        string `json:"fundingType"`
	Reference          string `json:"reference"`
	Notes              string `json:"notes"`
}

func (s *Server) purchasePaymentRoutes(mux *http.ServeMux) {
	mux.Handle(
		"POST /api/purchases/{id}/payments",
		requireRoles(s.createPurchasePayment, "superadmin", "admin"),
	)
	mux.Handle(
		"GET /api/purchases/{id}/payments",
		requireRoles(s.listPurchasePayments, "superadmin", "admin"),
	)
}

func (s *Server) createPurchasePayment(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || purchaseID <= 0 {
		problem(w, 400, "Invalid purchase")
		return
	}

	var req purchasePaymentRequest
	if !decode(w, r, &req) {
		return
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	req.FundingType = strings.TrimSpace(req.FundingType)

	if req.RequestID == "" ||
		req.Amount <= 0 ||
		req.FinancialAccountID <= 0 {
		problem(w, 400, "Request ID, amount and financial account are required")
		return
	}

	switch req.FundingType {
	case "superadmin_capital", "reinvested_revenue", "other":
	default:
		problem(w, 400, "Invalid funding type")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not begin purchase payment")
		return
	}
	defer tx.Rollback()

	if err := auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}

	// Serialize payment attempts for the same purchase.
	var total int64
	var currency string

	err = tx.QueryRowContext(r.Context(), `
		SELECT total,currency
		FROM purchases
		WHERE id=$1
		FOR UPDATE
	`, purchaseID).Scan(&total, &currency)

	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Purchase not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load purchase")
		return
	}

	// Check an existing request before creating another payment.
	var existingID, existingPurchaseID, existingAccountID int64
	var existingAmount, existingActorID int64
	var existingFundingType, existingReference, existingNotes string

	err = tx.QueryRowContext(r.Context(), `
		SELECT id,purchase_id,financial_account_id,
		       amount,funding_type,reference,notes,created_by
		FROM purchase_payments
		WHERE request_id=$1
	`, req.RequestID).Scan(
		&existingID,
		&existingPurchaseID,
		&existingAccountID,
		&existingAmount,
		&existingFundingType,
		&existingReference,
		&existingNotes,
		&existingActorID,
	)

	if err == nil {
		if existingPurchaseID != purchaseID ||
			existingAccountID != req.FinancialAccountID ||
			existingAmount != req.Amount ||
			existingFundingType != req.FundingType ||
			existingReference != strings.TrimSpace(req.Reference) ||
			existingNotes != strings.TrimSpace(req.Notes) ||
			existingActorID != principal(r).ID {
			problem(w, 409, "Request ID already used for another payment")
			return
		}
		respond(w, 200, map[string]any{
			"id":        existingID,
			"duplicate": true,
		})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		problem(w, 500, "Could not check payment request")
		return
	}

	var paid int64

	if err := tx.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(amount),0)
		FROM purchase_payments
		WHERE purchase_id=$1
	`, purchaseID).Scan(&paid); err != nil {
		problem(w, 500, "Could not calculate paid amount")
		return
	}

	if req.Amount > total-paid {
		problem(w, 409, "Payment exceeds outstanding purchase balance")
		return
	}

	var accountCurrency, accountType string
	var accountBalance int64
	var accountUserID sql.NullInt64

	err = tx.QueryRowContext(r.Context(), `
		SELECT currency,account_type,balance,user_id
		FROM financial_accounts
		WHERE id=$1
		FOR UPDATE
	`, req.FinancialAccountID).Scan(
		&accountCurrency,
		&accountType,
		&accountBalance,
		&accountUserID,
	)

	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Financial account not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not load financial account")
		return
	}

	if accountCurrency != currency {
		problem(w, 409, "Financial account currency mismatch")
		return
	}

	if accountType != "bank" &&
		accountType != "central_cash" &&
		accountType != "superadmin_cash" {
		problem(w, 403, "Account cannot fund supplier payments")
		return
	}

	if accountType == "superadmin_cash" &&
		(!accountUserID.Valid ||
			accountUserID.Int64 != principal(r).ID) {
		problem(w, 403, "Financial account belongs to another user")
		return
	}

	if accountBalance < req.Amount {
		problem(w, 409, "Insufficient financial account balance")
		return
	}

	var paymentID int64

	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO purchase_payments(
			purchase_id,financial_account_id,amount,
			currency,funding_type,request_id,
			reference,notes,created_by
		)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id
	`,
		purchaseID,
		req.FinancialAccountID,
		req.Amount,
		currency,
		req.FundingType,
		req.RequestID,
		strings.TrimSpace(req.Reference),
		strings.TrimSpace(req.Notes),
		principal(r).ID,
	).Scan(&paymentID)

	if err != nil {
		problem(w, 500, "Could not save purchase payment")
		return
	}

	if err := postFinancialCashMovement(
		r.Context(),
		tx,
		req.FinancialAccountID,
		-req.Amount,
		"stock_purchase_payment",
		"purchase_payment",
		paymentID,
		principal(r).ID,
		req.Notes,
	); err != nil {
		problem(w, 500, "Could not post purchase payment")
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO purchase_funding_allocations(
			purchase_id,funding_type,amount,
			currency,created_by
		)
		VALUES($1,$2,$3,$4,$5)
	`,
		purchaseID,
		req.FundingType,
		req.Amount,
		currency,
		principal(r).ID,
	)

	if err != nil {
		problem(w, 500, "Could not record purchase funding")
		return
	}

	if err := tx.Commit(); err != nil {
		problem(w, 500, "Could not commit purchase payment")
		return
	}

	respond(w, 201, map[string]any{
		"id":                paymentID,
		"purchaseId":        purchaseID,
		"amount":            req.Amount,
		"currency":          currency,
		"outstandingAmount": total - paid - req.Amount,
	})
}

func (s *Server) listPurchasePayments(w http.ResponseWriter, r *http.Request) {
	purchaseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || purchaseID <= 0 {
		problem(w, 400, "Invalid purchase")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id,amount,currency,funding_type,
		       financial_account_id,reference,notes,created_at
		FROM purchase_payments
		WHERE purchase_id=$1
		ORDER BY id DESC
	`, purchaseID)

	if err != nil {
		problem(w, 500, "Could not load purchase payments")
		return
	}
	defer rows.Close()

	out := []map[string]any{}

	for rows.Next() {
		var id, amount, accountID int64
		var currency, funding, reference, notes string
		var createdAt any

		if err := rows.Scan(
			&id, &amount, &currency, &funding,
			&accountID, &reference, &notes, &createdAt,
		); err != nil {
			problem(w, 500, "Could not read purchase payment")
			return
		}

		out = append(out, map[string]any{
			"id":                 id,
			"amount":             amount,
			"currency":           currency,
			"fundingType":        funding,
			"financialAccountId": accountID,
			"reference":          reference,
			"notes":              notes,
			"createdAt":          createdAt,
		})
	}

	if err := rows.Err(); err != nil {
		problem(w, 500, "Could not read purchase payments")
		return
	}

	respond(w, 200, out)
}
