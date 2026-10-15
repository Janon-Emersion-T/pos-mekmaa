-- Historical cost accounting foundation.
-- Do not backfill old sales with today's product cost.

ALTER TABLE sale_items
ADD COLUMN unit_cost_snapshot BIGINT;

ALTER TABLE sale_items
ADD CONSTRAINT sale_items_unit_cost_snapshot_nonnegative
CHECK (
    unit_cost_snapshot IS NULL
    OR unit_cost_snapshot >= 0
);

-- Inventory cost balance in minor currency units.
-- NULL indicates that historical inventory valuation is not yet verified.

ALTER TABLE products
ADD COLUMN inventory_cost_balance BIGINT;

ALTER TABLE products
ADD CONSTRAINT products_inventory_cost_balance_nonnegative
CHECK (
    inventory_cost_balance IS NULL
    OR inventory_cost_balance >= 0
);

-- Record the accounting method used for each sale.
ALTER TABLE sale_items
ADD COLUMN cost_method TEXT
CHECK (
    cost_method IS NULL
    OR cost_method IN (
        'weighted_average',
        'verified_opening',
        'manual_verified'
    )
);

-- Cost snapshots are immutable once recorded.
CREATE FUNCTION prevent_sale_cost_snapshot_changes()
RETURNS trigger AS $$
BEGIN
    IF OLD.unit_cost_snapshot IS DISTINCT FROM NEW.unit_cost_snapshot
       OR OLD.cost_method IS DISTINCT FROM NEW.cost_method THEN
        RAISE EXCEPTION
            'Historical sale cost snapshots cannot be modified';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER prevent_sale_cost_snapshot_changes_trigger
BEFORE UPDATE ON sale_items
FOR EACH ROW
EXECUTE FUNCTION prevent_sale_cost_snapshot_changes();

CREATE VIEW verified_sale_costs AS
SELECT
    si.sale_id,

    COUNT(*) AS item_lines,

    COUNT(si.unit_cost_snapshot) AS verified_cost_lines,

    CASE
        WHEN COUNT(*) = COUNT(si.unit_cost_snapshot)
        THEN SUM(
            si.quantity::BIGINT *
            si.unit_cost_snapshot
        )
        ELSE NULL
    END AS total_cogs

FROM sale_items si
GROUP BY si.sale_id;
