package main

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func (s *Server) refundSale(w http.ResponseWriter, r *http.Request) {
	saleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || saleID < 1 {
		problem(w, 400, "Invalid sale")
		return
	}
	var req struct {
		RequestID           string `json:"requestId"`
		Reason              string `json:"reason"`
		Confirmed           bool   `json:"confirmed"`
		CreditRefundPayment string `json:"creditRefundPayment,omitempty"`
		ExpectedOutstanding *int   `json:"expectedOutstanding,omitempty"`
		Items               []struct {
			SaleItemID int64 `json:"saleItemId"`
			Quantity   int   `json:"quantity"`
			Restock    bool  `json:"restock"`
		} `json:"items"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.CreditRefundPayment != "" && req.CreditRefundPayment != "cash" && req.CreditRefundPayment != "card" {
		problem(w, 400, "Choose cash or card for the paid portion of the return")
		return
	}
	if !req.Confirmed || req.Reason == "" || len(req.Reason) > 500 || len(req.Items) < 1 || len(req.Items) > 100 {
		problem(w, 400, "Select return quantities, enter a reason, and confirm payment has been refunded")
		return
	}
	sort.Slice(req.Items, func(i, j int) bool { return req.Items[i].SaleItemID < req.Items[j].SaleItemID })
	for i, x := range req.Items {
		if x.Quantity < 1 || x.Quantity > 100 || x.SaleItemID < 1 || (i > 0 && req.Items[i-1].SaleItemID == x.SaleItemID) {
			problem(w, 400, "Invalid or duplicate refund item")
			return
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not start refund")
		return
	}
	defer tx.Rollback()
	replay, hash, err := requestReplay(r.Context(), tx, req.RequestID, "refund:"+fmtID(saleID), principal(r).ID, req)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	if replay != nil {
		respond(w, 200, replay)
		return
	}
	if auditActor(r.Context(), tx, principal(r)) != nil {
		problem(w, 500, "Could not record actor")
		return
	}
	session, err := s.lockedSession(r.Context(), tx, principal(r).ID)
	if err != nil {
		problem(w, 409, "Open your own register session before recording a refund")
		return
	}
	var currency, payment string
	var deleted sql.NullTime
	if err = tx.QueryRowContext(r.Context(), `SELECT currency,payment,deleted_at FROM sales WHERE id=$1 FOR UPDATE`, saleID).Scan(&currency, &payment, &deleted); err != nil {
		problem(w, 404, "Sale not found")
		return
	}
	if deleted.Valid {
		problem(w, 409, "A deleted sale cannot be refunded")
		return
	}
	if currency != session.Currency {
		problem(w, 409, "The register currency must match the original sale currency")
		return
	}
	type returned struct {
		id               int64
		product          sql.NullInt64
		quantity, amount int
		restock          bool
		historicalCost   sql.NullInt64
	}
	var lines []returned
	total := 0
	for _, x := range req.Items {
		var product sql.NullInt64
		var quantity, price, refunded int
		var originalCost sql.NullInt64
		var previouslyAllocated sql.NullInt64

		err = tx.QueryRowContext(
			r.Context(),
			`SELECT i.product_id,i.quantity,i.unit_price,
			        i.total_cost_snapshot,
			        COALESCE((
			            SELECT SUM(ri.quantity)
			            FROM refund_items ri
			            WHERE ri.sale_item_id=i.id
			        ),0),
			        (
			            SELECT SUM(ri.historical_cost_allocated)
			            FROM refund_items ri
			            WHERE ri.sale_item_id=i.id
			        )
			 FROM sale_items i
			 WHERE i.id=$1 AND i.sale_id=$2`,
			x.SaleItemID,
			saleID,
		).Scan(
			&product,
			&quantity,
			&price,
			&originalCost,
			&refunded,
			&previouslyAllocated,
		)
		if err != nil || x.Quantity > quantity-refunded {
			problem(w, 409, "Return quantity exceeds the remaining quantity for this sale")
			return
		}
		if x.Restock && !product.Valid {
			problem(w, 409, "This item can no longer be restocked")
			return
		}
		var allocatedCost sql.NullInt64

		if originalCost.Valid {
			if refunded > 0 && !previouslyAllocated.Valid {
				problem(w, 409, "Historical refund costs require reconciliation")
				return
			}

			previousCost := int64(0)
			if previouslyAllocated.Valid {
				previousCost = previouslyAllocated.Int64
			}

			cost, costErr := proportionalRefundCost(
				int64(quantity),
				originalCost.Int64,
				int64(refunded),
				previousCost,
				int64(x.Quantity),
			)

			if costErr != nil {
				problem(w, 409, "Could not allocate historical refund cost")
				return
			}

			allocatedCost = sql.NullInt64{
				Int64: cost,
				Valid: true,
			}
		}

		amount := price * x.Quantity
		total += amount

		lines = append(lines, returned{
			x.SaleItemID,
			product,
			x.Quantity,
			amount,
			x.Restock,
			allocatedCost,
		})
	}
	var id int64
	var cashReturned, cardReturned, debtReduction int
	if payment == "credit" {
		var outstanding int
		if tx.QueryRowContext(r.Context(), `SELECT outstanding FROM credit_sale_balances WHERE id=$1`, saleID).Scan(&outstanding) != nil {
			problem(w, 500, "Could not read customer balance")
			return
		}
		if req.ExpectedOutstanding == nil || *req.ExpectedOutstanding != outstanding {
			problem(w, 409, "The customer balance has changed. Reopen the return and review the amount to refund")
			return
		}
		debtReduction = min(total, outstanding)
		if req.CreditRefundPayment == "card" {
			cardReturned = total - debtReduction
		} else {
			cashReturned = total - debtReduction
		}
	}
	err = tx.QueryRowContext(r.Context(), `INSERT INTO refunds(sale_id,session_id,created_by,cashier_email,reason,total,payment,currency,cash_returned,card_returned) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, saleID, session.ID, principal(r).ID, principal(r).Email, req.Reason, total, payment, currency, cashReturned, cardReturned).Scan(&id)
	if err != nil {
		problem(w, 500, "Could not record refund")
		return
	}
	for _, x := range lines {
		_, err = tx.ExecContext(
			r.Context(),
			`INSERT INTO refund_items(
				refund_id,sale_item_id,quantity,restock,
				amount,historical_cost_allocated
			) VALUES($1,$2,$3,$4,$5,$6)`,
			id,
			x.id,
			x.quantity,
			x.restock,
			x.amount,
			x.historicalCost,
		)

		if err != nil {
			problem(w, 500, "Could not record refunded item cost")
			return
		}

		if !x.restock {
			continue
		}

		var stock int64
		var balance sql.NullInt64

		err = tx.QueryRowContext(
			r.Context(),
			`SELECT stock,inventory_cost_balance
			 FROM products WHERE id=$1 FOR UPDATE`,
			x.product.Int64,
		).Scan(&stock, &balance)

		if err != nil {
			problem(w, 500, "Could not lock returned product")
			return
		}

		var updatedBalance any
		var restoredValue any

		if x.historicalCost.Valid && balance.Valid {
			if balance.Int64 > int64(^uint64(0)>>1)-x.historicalCost.Int64 {
				problem(w, 409, "Refund inventory valuation exceeds supported range")
				return
			}

			updatedBalance = balance.Int64 + x.historicalCost.Int64
			restoredValue = x.historicalCost.Int64
		}

		_, err = tx.ExecContext(
			r.Context(),
			`UPDATE products
			 SET stock=stock+$1,inventory_cost_balance=$2
			 WHERE id=$3`,
			x.quantity,
			updatedBalance,
			x.product.Int64,
		)

		if err == nil {
			_, err = tx.ExecContext(
				r.Context(),
				`INSERT INTO inventory_movements(
					product_id,kind,quantity,reference_type,
					reference_id,note,created_by
				) VALUES($1,'adjustment',$2,'refund',$3,$4,$5)`,
				x.product.Int64,
				x.quantity,
				id,
				req.Reason,
				principal(r).ID,
			)
		}

		if err == nil {
			_, err = tx.ExecContext(
				r.Context(),
				`INSERT INTO inventory_valuation_entries(
					product_id,quantity_change,value_change,
					currency,movement_type,reference_type,
					reference_id,notes,created_by
				) VALUES($1,$2,$3,$4,'refund','refund',$5,$6,$7)`,
				x.product.Int64,
				x.quantity,
				restoredValue,
				currency,
				id,
				req.Reason,
				principal(r).ID,
			)
		}

		if err != nil {
			problem(w, 500, "Could not restore refunded inventory valuation")
			return
		}
	}
	// Only physical cash returned to the customer affects
	// the cashier's persistent cash account.
	cashMovement := int64(0)
	if payment == "cash" {
		cashMovement = int64(total)
	} else if payment == "credit" {
		cashMovement = int64(cashReturned)
	}

	if cashMovement > 0 {
		cashierID := principal(r).ID

		account, accountErr := getOrCreateFinancialAccount(
			r.Context(), tx, "staff_cash", &cashierID, currency,
		)
		if accountErr != nil {
			problem(w, 500, "Could not load refund cashier account")
			return
		}

		if err = postFinancialCashMovement(
			r.Context(), tx,
			account.ID, -cashMovement,
			"cash_refund", "refund", id,
			cashierID, req.Reason,
		); err != nil {
			problem(w, 500, "Could not post cash refund")
			return
		}
	}

	result := map[string]any{"id": id, "saleId": saleID, "total": total, "currency": currency, "payment": payment, "debtReduction": debtReduction, "cashReturned": cashReturned, "cardReturned": cardReturned}
	if saveRequest(r.Context(), tx, req.RequestID, "refund:"+fmtID(saleID), hash, principal(r).ID, result) != nil || tx.Commit() != nil {
		problem(w, 500, "Could not complete refund")
		return
	}
	respond(w, 201, result)
}
func (s *Server) refunds(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, 400, "Invalid sale")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,created_at,session_id,cashier_email,reason,total,payment,currency FROM refunds WHERE sale_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		problem(w, 500, "Could not load refunds")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, sid, total int
		var date, payment, currency, email, reason string
		if rows.Scan(&id, &date, &sid, &email, &reason, &total, &payment, &currency) != nil {
			problem(w, 500, "Could not read refunds")
			return
		}
		out = append(out, map[string]any{"id": id, "created": date, "sessionId": sid, "cashierEmail": email, "reason": reason, "total": total, "payment": payment, "currency": currency})
	}
	if rows.Err() != nil {
		problem(w, 500, "Could not read refunds")
		return
	}
	respond(w, 200, out)
}
