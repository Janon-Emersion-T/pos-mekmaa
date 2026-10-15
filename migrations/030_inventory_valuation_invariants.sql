-- Inventory valuation consistency constraints.
-- Unknown historical values remain NULL.

ALTER TABLE products
ADD CONSTRAINT products_verified_inventory_consistency
CHECK (
    inventory_cost_balance IS NULL
    OR (
        inventory_cost_balance >= 0
        AND (
            stock > 0
            OR inventory_cost_balance = 0
        )
    )
);

ALTER TABLE sale_items
ADD CONSTRAINT sale_items_verified_cost_consistency
CHECK (
    (
        total_cost_snapshot IS NULL
        AND unit_cost_snapshot IS NULL
        AND cost_method IS NULL
    )
    OR (
        total_cost_snapshot IS NOT NULL
        AND unit_cost_snapshot IS NOT NULL
        AND cost_method IS NOT NULL
    )
);
