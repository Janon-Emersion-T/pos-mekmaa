package main

import (
	"context"
	"database/sql"
	"net/http"
)

func (s *Server) updateOptimizedDebts(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor := principal(r)
	if actor == nil || actor.Role != "superadmin" {
		problem(w, http.StatusForbidden, "Superadmin access required")
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}

	if !decode(w, r, &req) {
		return
	}

	if req.Enabled == nil {
		problem(w, 400, "Enabled must be true or false")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not update Optimized Debts")
		return
	}
	defer tx.Rollback()

	if err := auditActor(r.Context(), tx, actor); err != nil {
		problem(w, 500, "Could not record actor")
		return
	}

	var previous bool
	if err := tx.QueryRowContext(r.Context(), `
        SELECT optimized_debts_enabled
        FROM store_settings
        WHERE id=1
        FOR UPDATE
    `).Scan(&previous); err != nil {
		problem(w, 500, "Could not lock Optimized Debts settings")
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
        UPDATE store_settings
        SET optimized_debts_enabled=$1
        WHERE id=1
    `, *req.Enabled); err != nil {
		problem(w, 500, "Could not save Optimized Debts setting")
		return
	}

	if previous != *req.Enabled {
		if _, err := tx.ExecContext(r.Context(), `
            INSERT INTO optimized_debts_setting_changes(
                previous_enabled,new_enabled,changed_by
            )
            VALUES($1,$2,$3)
        `, previous, *req.Enabled, actor.ID); err != nil {
			problem(w, 500, "Could not audit Optimized Debts setting")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		problem(w, 500, "Could not commit Optimized Debts setting")
		return
	}

	respond(w, 200, map[string]any{
		"enabled":  *req.Enabled,
		"previous": previous,
	})
}

type OptimizedDebtCapacity struct {
	Enabled         bool  `json:"enabled"`
	VerifiedMargin  int64 `json:"verifiedMargin"`
	OutstandingDebt int64 `json:"outstandingDebt"`
	AvailableCredit int64 `json:"availableCredit"`
	UnverifiedItems int64 `json:"unverifiedItems"`
}

// optimizedDebtCapacity must run within the checkout transaction.
// Caller locks store_settings before invoking this function.
func optimizedDebtCapacity(
	ctx context.Context,
	tx *sql.Tx,
	currency string,
) (OptimizedDebtCapacity, error) {
	var result OptimizedDebtCapacity

	err := tx.QueryRowContext(ctx, `
        SELECT
            COALESCE(SUM(
                CASE
                    WHEN inventory_cost_balance IS NOT NULL
                     AND stock > 0
                     AND price::BIGINT * stock > inventory_cost_balance
                    THEN price::BIGINT * stock - inventory_cost_balance
                    ELSE 0
                END
            ),0),
            COUNT(*) FILTER (
                WHERE stock > 0
                  AND inventory_cost_balance IS NULL
            )
        FROM products
    `).Scan(&result.VerifiedMargin, &result.UnverifiedItems)

	if err != nil {
		return result, err
	}

	err = tx.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(outstanding),0)
        FROM (
            SELECT GREATEST(b.outstanding,0) AS outstanding
            FROM credit_sale_balances b
            JOIN customers c ON c.id=b.customer_id
            WHERE b.currency=$1
              AND b.deleted_at IS NULL
              AND NOT c.is_owner

            UNION ALL

            SELECT GREATEST(b.outstanding,0)
            FROM customer_opening_balance_totals b
            JOIN customers c ON c.id=b.customer_id
            WHERE b.currency=$1
              AND NOT c.is_owner
        ) debts
    `, currency).Scan(&result.OutstandingDebt)

	if err != nil {
		return result, err
	}

	// Unknown stock valuation makes global credit capacity unreliable.
	// Fail closed until inventory valuation has been reconciled.
	if result.UnverifiedItems == 0 &&
		result.VerifiedMargin > result.OutstandingDebt {
		result.AvailableCredit =
			result.VerifiedMargin - result.OutstandingDebt
	}

	return result, nil
}

func (s *Server) getOptimizedDebtCapacity(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor := principal(r)
	if actor == nil || actor.Role != "superadmin" {
		problem(w, http.StatusForbidden, "Superadmin access required")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		problem(w, 500, "Could not read debt capacity")
		return
	}
	defer tx.Rollback()

	var currency string
	var enabled bool

	err = tx.QueryRowContext(r.Context(), `
        SELECT currency,optimized_debts_enabled
        FROM store_settings
        WHERE id=1
        FOR SHARE
    `).Scan(&currency, &enabled)

	if err != nil {
		problem(w, 500, "Could not read Optimized Debts settings")
		return
	}

	capacity, err := optimizedDebtCapacity(r.Context(), tx, currency)
	if err != nil {
		problem(w, 500, "Could not calculate debt capacity")
		return
	}

	capacity.Enabled = enabled
	respond(w, 200, capacity)
}
