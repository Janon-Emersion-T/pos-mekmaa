package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

type guestRefreshmentRequest struct {
	RequestID string `json:"requestId"`
	ProductID int64  `json:"productId"`
	Quantity  int64  `json:"quantity"`
	GuestName string `json:"guestName"`
	Reason    string `json:"reason"`
}

func validGuestRefreshment(x guestRefreshmentRequest) bool {
	return len(x.RequestID) >= 16 && len(x.RequestID) <= 100 &&
		x.ProductID > 0 && x.Quantity > 0 && x.Quantity <= 1000 &&
		len(strings.TrimSpace(x.GuestName)) > 0 &&
		len(strings.TrimSpace(x.GuestName)) <= 150 &&
		len(strings.TrimSpace(x.Reason)) > 0 &&
		len(strings.TrimSpace(x.Reason)) <= 500
}

func (s *Server) guestRefreshments(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := s.db.QueryContext(r.Context(), `
            SELECT id,product_id,product_name,quantity,unit_cost,unit_price,
                   currency,guest_name,reason,created_by_email,created_at
            FROM guest_refreshments ORDER BY id DESC LIMIT 200
        `)
		if err != nil {
			problem(w, 500, "Could not load guest refreshments")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, pid, qty, cost, price int64
			var product, currency, guest, reason, email string
			var createdAt any
			if err := rows.Scan(&id, &pid, &product, &qty, &cost, &price,
				&currency, &guest, &reason, &email, &createdAt); err != nil {
				problem(w, 500, "Could not read guest refreshments")
				return
			}
			out = append(out, map[string]any{
				"id": id, "productId": pid, "product": product,
				"quantity": qty, "unitCost": cost, "unitPrice": price,
				"totalCost": cost * qty, "currency": currency,
				"guestName": guest, "reason": reason,
				"createdByEmail": email, "createdAt": createdAt,
			})
		}
		if rows.Err() != nil {
			problem(w, 500, "Could not read guest refreshments")
			return
		}
		respond(w, 200, out)
		return
	}

	var req guestRefreshmentRequest
	if !decode(w, r, &req) {
		return
	}
	req.GuestName = strings.TrimSpace(req.GuestName)
	req.Reason = strings.TrimSpace(req.Reason)
	req.RequestID = strings.TrimSpace(req.RequestID)
	if !validGuestRefreshment(req) {
		problem(w, 400, "Enter a valid product, quantity, guest, reason and request ID")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not begin guest refreshment")
		return
	}
	defer tx.Rollback()

	if err = auditActor(r.Context(), tx, principal(r)); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}

	// Serialize identical request IDs before locking inventory.
	var existingID int64
	err = tx.QueryRowContext(r.Context(), `
        SELECT id FROM guest_refreshments WHERE request_id=$1
    `, req.RequestID).Scan(&existingID)
	if err == nil {
		respond(w, 200, map[string]any{"id": existingID, "duplicate": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		problem(w, 500, "Could not check request")
		return
	}

	var name, currency string
	var stock int64
	var cost, price int64

	err = tx.QueryRowContext(r.Context(), `
        SELECT name,stock,cost,price
        FROM products WHERE id=$1 AND active FOR UPDATE
    `, req.ProductID).Scan(&name, &stock, &cost, &price)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "Product not found")
		return
	}
	if err != nil {
		problem(w, 500, "Could not lock product")
		return
	}
	if stock < req.Quantity {
		problem(w, 409, "Insufficient stock for complimentary refreshments")
		return
	}
	if err = tx.QueryRowContext(r.Context(), `
        SELECT currency FROM store_settings WHERE id=1
    `).Scan(&currency); err != nil {
		problem(w, 500, "Could not load currency")
		return
	}

	var id int64
	err = tx.QueryRowContext(r.Context(), `
        INSERT INTO guest_refreshments(
            request_id,product_id,product_name,quantity,unit_cost,unit_price,
            currency,guest_name,reason,created_by,created_by_email
        ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
        ON CONFLICT(request_id) DO NOTHING
        RETURNING id
    `, req.RequestID, req.ProductID, name, req.Quantity, cost, price,
		currency, req.GuestName, req.Reason, principal(r).ID,
		principal(r).Email).Scan(&id)

	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.QueryRowContext(r.Context(), `
            SELECT id FROM guest_refreshments WHERE request_id=$1
        `, req.RequestID).Scan(&id); err != nil {
			problem(w, 500, "Could not resolve duplicate request")
			return
		}
		respond(w, 200, map[string]any{"id": id, "duplicate": true})
		return
	}
	if err != nil {
		problem(w, 500, "Could not record complimentary refreshments")
		return
	}

	_, err = tx.ExecContext(r.Context(), `
        UPDATE products SET stock=stock-$1 WHERE id=$2
    `, req.Quantity, req.ProductID)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `
            INSERT INTO inventory_movements(
                product_id,kind,quantity,reference_type,reference_id,note,created_by
            ) VALUES($1,'guest_refreshment',$2,'guest_refreshment',$3,$4,$5)
        `, req.ProductID, -req.Quantity, id,
			"Owner-sponsored: "+req.GuestName+" — "+req.Reason,
			principal(r).ID)
	}
	if err != nil || tx.Commit() != nil {
		problem(w, 500, "Could not complete complimentary refreshment")
		return
	}
	respond(w, 201, map[string]any{"id": id, "totalCost": cost * req.Quantity})
}
