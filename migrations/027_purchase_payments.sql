-- Purchase payment accounting.
-- Existing purchases remain unpaid until a payment is explicitly recorded.
-- Funding classification does not create additional invested capital.

CREATE TABLE purchase_payments (
    id BIGSERIAL PRIMARY KEY,

    purchase_id BIGINT NOT NULL
        REFERENCES purchases(id),

    financial_account_id BIGINT NOT NULL
        REFERENCES financial_accounts(id),

    amount BIGINT NOT NULL
        CHECK (amount > 0),

    currency TEXT NOT NULL,

    funding_type TEXT NOT NULL
        CHECK (
            funding_type IN (
                'superadmin_capital',
                'reinvested_revenue',
                'other'
            )
        ),

    request_id TEXT NOT NULL UNIQUE,

    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',

    created_by BIGINT NOT NULL
        REFERENCES users(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX purchase_payments_purchase_idx
ON purchase_payments(purchase_id);

CREATE INDEX purchase_payments_account_idx
ON purchase_payments(financial_account_id);

CREATE TRIGGER purchase_payments_audit
AFTER INSERT ON purchase_payments
FOR EACH ROW
EXECUTE FUNCTION audit_change();

-- Derived purchase payment status.
-- No historical purchase is assumed to be paid.

CREATE VIEW purchase_payment_balances AS
SELECT
    p.id AS purchase_id,
    p.total,
    p.currency,

    COALESCE(
        SUM(pp.amount),
        0
    ) AS paid_amount,

    p.total - COALESCE(
        SUM(pp.amount),
        0
    ) AS outstanding_amount,

    CASE
        WHEN COALESCE(SUM(pp.amount), 0) = 0
            THEN 'unpaid'

        WHEN COALESCE(SUM(pp.amount), 0) >= p.total
            THEN 'paid'

        ELSE 'partially_paid'
    END AS payment_status

FROM purchases p

LEFT JOIN purchase_payments pp
    ON pp.purchase_id = p.id

GROUP BY
    p.id,
    p.total,
    p.currency;
