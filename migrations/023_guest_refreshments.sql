-- Owner-sponsored complimentary refreshments are inventory consumption,
-- not sales, receivables or register cash movements.
CREATE TABLE guest_refreshments (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    product_id BIGINT NOT NULL REFERENCES products(id),
    product_name TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK(quantity > 0),
    unit_cost BIGINT NOT NULL CHECK(unit_cost >= 0),
    unit_price BIGINT NOT NULL CHECK(unit_price >= 0),
    currency TEXT NOT NULL,
    guest_name TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_by_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK(length(trim(guest_name)) BETWEEN 1 AND 150),
    CHECK(length(trim(reason)) BETWEEN 1 AND 500)
);
CREATE INDEX guest_refreshments_created_idx
ON guest_refreshments(created_at DESC);
CREATE TRIGGER guest_refreshments_audit
AFTER INSERT ON guest_refreshments
FOR EACH ROW EXECUTE FUNCTION audit_change();

ALTER TABLE inventory_movements
DROP CONSTRAINT IF EXISTS inventory_movements_kind_check;
ALTER TABLE inventory_movements
ADD CONSTRAINT inventory_movements_kind_check
CHECK(kind IN ('opening','sale','purchase','adjustment','guest_refreshment'));
