-- Preserve exact historical cost allocations for partial refunds.
-- NULL means the original sale cost is not verified.

ALTER TABLE refund_items
ADD COLUMN historical_cost_allocated BIGINT;

ALTER TABLE refund_items
ADD CONSTRAINT refund_items_historical_cost_nonnegative
CHECK (
    historical_cost_allocated IS NULL
    OR historical_cost_allocated >= 0
);

-- A refund's historical cost allocation is immutable once recorded.
CREATE FUNCTION prevent_refund_cost_changes()
RETURNS trigger AS $$
BEGIN
    IF OLD.historical_cost_allocated
       IS DISTINCT FROM NEW.historical_cost_allocated THEN
        RAISE EXCEPTION
            'Historical refund cost allocations cannot be modified';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER prevent_refund_cost_changes_trigger
BEFORE UPDATE ON refund_items
FOR EACH ROW
EXECUTE FUNCTION prevent_refund_cost_changes();

CREATE INDEX refund_items_sale_item_cost_idx
ON refund_items(sale_item_id);
