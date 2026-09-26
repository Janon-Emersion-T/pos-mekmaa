CREATE TABLE customers (
 id BIGSERIAL PRIMARY KEY,
 name TEXT NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 200),
 phone TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '',
 address TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customers_name_idx ON customers(lower(name));
ALTER TABLE sales ADD COLUMN customer_id BIGINT REFERENCES customers(id),
 ADD COLUMN customer_name TEXT NOT NULL DEFAULT '';
ALTER TABLE sales DROP CONSTRAINT sales_payment_check;
ALTER TABLE sales ADD CONSTRAINT sales_payment_check CHECK(payment IN('cash','card','credit'));
ALTER TABLE sales ADD CONSTRAINT credit_customer_required CHECK(payment<>'credit' OR customer_id IS NOT NULL);
CREATE INDEX sales_customer_idx ON sales(customer_id,id DESC);
CREATE TABLE customer_payments (
 id BIGSERIAL PRIMARY KEY,
 sale_id BIGINT NOT NULL REFERENCES sales(id),
 session_id BIGINT NOT NULL REFERENCES register_sessions(id),
 amount INTEGER NOT NULL CHECK(amount>0),
 payment TEXT NOT NULL CHECK(payment IN('cash','card')),
 currency TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 created_by BIGINT NOT NULL REFERENCES users(id),
 cashier_email TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX customer_payments_sale_idx ON customer_payments(sale_id);
CREATE INDEX customer_payments_session_idx ON customer_payments(session_id);
ALTER TABLE refunds DROP CONSTRAINT refunds_payment_check;
ALTER TABLE refunds ADD CONSTRAINT refunds_payment_check CHECK(payment IN('cash','card','credit'));
ALTER TABLE refunds ADD COLUMN cash_returned INTEGER NOT NULL DEFAULT 0 CHECK(cash_returned>=0),
 ADD COLUMN card_returned INTEGER NOT NULL DEFAULT 0 CHECK(card_returned>=0);
ALTER TABLE refunds ADD CONSTRAINT credit_refund_amounts CHECK(cash_returned+card_returned<=total AND (payment='credit' OR cash_returned+card_returned=0));
-- Credit returns first cancel unpaid debt, then refund any excess already paid.
CREATE VIEW credit_sale_balances AS
 SELECT s.id,s.customer_id,s.currency,s.deleted_at,
 COALESCE(p.paid,0)-COALESCE(r.returned,0) AS paid_amount,
 CASE WHEN s.deleted_at IS NULL THEN s.total-COALESCE(r.refunded,0)-COALESCE(p.paid,0)+COALESCE(r.returned,0) ELSE 0 END AS outstanding
 FROM sales s
 LEFT JOIN (SELECT sale_id,SUM(amount) AS paid FROM customer_payments GROUP BY sale_id) p ON p.sale_id=s.id
 LEFT JOIN (SELECT sale_id,SUM(total) AS refunded,SUM(cash_returned+card_returned) AS returned FROM refunds GROUP BY sale_id) r ON r.sale_id=s.id
 WHERE s.payment='credit';
CREATE TRIGGER customers_audit AFTER INSERT OR UPDATE ON customers FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER customer_payments_audit AFTER INSERT ON customer_payments FOR EACH ROW EXECUTE FUNCTION audit_change();
