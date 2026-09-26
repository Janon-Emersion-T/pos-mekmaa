-- Historical debt is not a new sale and does not affect stock or register cash.
CREATE TABLE customer_opening_balances (
 id BIGSERIAL PRIMARY KEY,
 customer_id BIGINT NOT NULL UNIQUE REFERENCES customers(id),
 amount INTEGER NOT NULL CHECK(amount>0),
 currency TEXT NOT NULL,
 created_by BIGINT NOT NULL REFERENCES users(id),
 cashier_email TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customer_payments ALTER COLUMN sale_id DROP NOT NULL;
ALTER TABLE customer_payments ADD COLUMN opening_balance_id BIGINT REFERENCES customer_opening_balances(id);
ALTER TABLE customer_payments ADD CONSTRAINT customer_payment_target CHECK((sale_id IS NOT NULL)::int+(opening_balance_id IS NOT NULL)::int=1);
CREATE INDEX customer_payments_opening_idx ON customer_payments(opening_balance_id);
CREATE VIEW customer_opening_balance_totals AS
 SELECT b.*,COALESCE(p.paid,0) AS paid_amount,b.amount-COALESCE(p.paid,0) AS outstanding
 FROM customer_opening_balances b
 LEFT JOIN (SELECT opening_balance_id,SUM(amount) AS paid FROM customer_payments GROUP BY opening_balance_id) p ON p.opening_balance_id=b.id;
CREATE TRIGGER customer_opening_balances_audit AFTER INSERT ON customer_opening_balances FOR EACH ROW EXECUTE FUNCTION audit_change();
