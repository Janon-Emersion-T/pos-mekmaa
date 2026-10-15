-- Profit drawings are separate from returns of invested capital.
-- No cash movement is permitted until profit eligibility is verified.

CREATE TABLE profit_drawings (
    id BIGSERIAL PRIMARY KEY,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    funding_account_id BIGINT NOT NULL
        REFERENCES financial_accounts(id),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN (
            'pending',
            'approved',
            'rejected',
            'cancelled',
            'paid'
        )),
    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_by BIGINT REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    CHECK (
        (status <> 'paid' OR paid_at IS NOT NULL)
    )
);

CREATE INDEX profit_drawings_status_idx
ON profit_drawings(status, created_at DESC);

CREATE INDEX profit_drawings_account_idx
ON profit_drawings(funding_account_id);

CREATE TRIGGER profit_drawings_audit
AFTER INSERT OR UPDATE ON profit_drawings
FOR EACH ROW
EXECUTE FUNCTION audit_change();
