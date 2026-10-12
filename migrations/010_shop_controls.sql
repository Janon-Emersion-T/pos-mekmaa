ALTER TABLE customers ADD COLUMN payment_terms_days INTEGER NOT NULL DEFAULT 30 CHECK(payment_terms_days BETWEEN 0 AND 365),
 ADD COLUMN credit_limit INTEGER CHECK(credit_limit>=0), ADD COLUMN credit_currency TEXT NOT NULL DEFAULT '';
ALTER TABLE sales ADD COLUMN due_date DATE;
ALTER TABLE customer_opening_balances ADD COLUMN due_date DATE;
ALTER TABLE register_sessions ADD COLUMN closing_reason TEXT NOT NULL DEFAULT '';
CREATE TABLE customer_balance_adjustments(
 id BIGSERIAL PRIMARY KEY,opening_balance_id BIGINT NOT NULL REFERENCES customer_opening_balances(id),
 amount INTEGER NOT NULL CHECK(amount<>0),reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 created_by BIGINT NOT NULL REFERENCES users(id),cashier_email TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE customer_payment_reversals(
 id BIGSERIAL PRIMARY KEY,payment_id BIGINT NOT NULL UNIQUE REFERENCES customer_payments(id),
 reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 created_by BIGINT NOT NULL REFERENCES users(id),cashier_email TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER customer_adjustment_audit AFTER INSERT ON customer_balance_adjustments FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER customer_reversal_audit AFTER INSERT ON customer_payment_reversals FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE OR REPLACE VIEW credit_sale_balances AS
 SELECT s.id,s.customer_id,s.currency,s.deleted_at,
 COALESCE(p.paid,0)-COALESCE(r.returned,0) AS paid_amount,
 CASE WHEN s.deleted_at IS NULL THEN s.total-COALESCE(r.refunded,0)-COALESCE(p.paid,0)+COALESCE(r.returned,0) ELSE 0 END AS outstanding
 FROM sales s
 LEFT JOIN (SELECT sale_id,SUM(amount) AS paid FROM customer_payments p WHERE NOT EXISTS(SELECT 1 FROM customer_payment_reversals WHERE payment_id=p.id) GROUP BY sale_id) p ON p.sale_id=s.id
 LEFT JOIN (SELECT sale_id,SUM(total) AS refunded,SUM(cash_returned+card_returned) AS returned FROM refunds GROUP BY sale_id) r ON r.sale_id=s.id
 WHERE s.payment='credit';
-- Preserve the existing view's column order; append new fields for upgrades.
CREATE OR REPLACE VIEW customer_opening_balance_totals AS
 SELECT b.id,b.customer_id,b.amount,b.currency,b.created_by,b.cashier_email,b.created_at,
 COALESCE(p.paid,0) AS paid_amount,b.amount+COALESCE(a.adjustment,0)-COALESCE(p.paid,0) AS outstanding,
 COALESCE(a.adjustment,0) AS adjustment_total,b.due_date
 FROM customer_opening_balances b
 LEFT JOIN (SELECT opening_balance_id,SUM(amount) AS paid FROM customer_payments p WHERE NOT EXISTS(SELECT 1 FROM customer_payment_reversals WHERE payment_id=p.id) GROUP BY opening_balance_id) p ON p.opening_balance_id=b.id
 LEFT JOIN (SELECT opening_balance_id,SUM(amount) AS adjustment FROM customer_balance_adjustments GROUP BY opening_balance_id) a ON a.opening_balance_id=b.id;

DROP TRIGGER customer_opening_balances_audit ON customer_opening_balances;
CREATE TRIGGER customer_opening_balances_audit AFTER INSERT OR UPDATE ON customer_opening_balances FOR EACH ROW EXECUTE FUNCTION audit_change();
