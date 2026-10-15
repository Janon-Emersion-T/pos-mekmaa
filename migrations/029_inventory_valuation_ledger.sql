-- Exact inventory valuation accounting.
-- Amounts are stored in the application's existing monetary units.
-- NULL valuation means the cost is not verified.

ALTER TABLE sale_items
ADD COLUMN total_cost_snapshot BIGINT;

ALTER TABLE sale_items
ADD CONSTRAINT sale_items_total_cost_snapshot_nonnegative
CHECK (
    total_cost_snapshot IS NULL
    OR total_cost_snapshot >= 0
);

CREATE TABLE inventory_valuation_entries (
    id BIGSERIAL PRIMARY KEY,

    product_id BIGINT NOT NULL
        REFERENCES products(id),

    quantity_change INTEGER NOT NULL
        CHECK (quantity_change <> 0),

    value_change BIGINT,

    currency TEXT NOT NULL,

    movement_type TEXT NOT NULL
        CHECK (
            movement_type IN (
                'opening',
                'purchase',
                'sale',
                'refund',
                'sale_reversal',
                'adjustment',
                'guest_refreshment',
                'correction'
            )
        ),

    reference_type TEXT NOT NULL,
    reference_id BIGINT NOT NULL,

    notes TEXT NOT NULL DEFAULT '',

    created_by BIGINT NOT NULL
        REFERENCES users(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX inventory_valuation_product_idx
ON inventory_valuation_entries(product_id, id DESC);

CREATE INDEX inventory_valuation_reference_idx
ON inventory_valuation_entries(reference_type, reference_id);

CREATE TRIGGER inventory_valuation_entries_audit
AFTER INSERT ON inventory_valuation_entries
FOR EACH ROW
EXECUTE FUNCTION audit_change();

-- Exact verified COGS takes precedence over rounded unit costs.
CREATE OR REPLACE VIEW verified_sale_costs AS
SELECT
    si.sale_id,

    COUNT(*) AS item_lines,

    COUNT(si.total_cost_snapshot) AS verified_cost_lines,

    CASE
        WHEN COUNT(*) = COUNT(si.total_cost_snapshot)
        THEN SUM(si.total_cost_snapshot)
        ELSE NULL
    END AS total_cogs

FROM sale_items si
GROUP BY si.sale_id;

-- Historical cost snapshots cannot be rewritten.
CREATE OR REPLACE FUNCTION prevent_sale_cost_snapshot_changes()
RETURNS trigger AS $$
BEGIN
    IF OLD.unit_cost_snapshot IS DISTINCT FROM NEW.unit_cost_snapshot
       OR OLD.total_cost_snapshot IS DISTINCT FROM NEW.total_cost_snapshot
       OR OLD.cost_method IS DISTINCT FROM NEW.cost_method THEN
        RAISE EXCEPTION
            'Historical sale cost snapshots cannot be modified';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
