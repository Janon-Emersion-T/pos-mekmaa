ALTER TABLE customers
ADD COLUMN IF NOT EXISTS is_owner BOOLEAN NOT NULL DEFAULT false;

-- Reuse one existing exact-name Owner customer if present.
UPDATE customers SET is_owner=true
WHERE id=(
    SELECT id FROM customers
    WHERE lower(trim(name))='owner'
    ORDER BY id LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM customers WHERE is_owner);

INSERT INTO customers(name,notes,is_owner)
SELECT 'Owner','Owner account: cost-price credit checkout',true
WHERE NOT EXISTS (SELECT 1 FROM customers WHERE is_owner);

CREATE UNIQUE INDEX IF NOT EXISTS customers_one_owner
ON customers(is_owner) WHERE is_owner;
