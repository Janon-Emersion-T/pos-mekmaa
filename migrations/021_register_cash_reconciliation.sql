-- Verified register cash reconciliation.
-- Carry-forward remains disabled at the API until verification is implemented.

CREATE TABLE register_cash_reconciliations (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL UNIQUE
        REFERENCES register_sessions(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    currency TEXT NOT NULL,
    expected_cash BIGINT NOT NULL,
    counted_cash BIGINT NOT NULL CHECK (counted_cash >= 0),
    verified_available_cash BIGINT
        CHECK (verified_available_cash >= 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'verified', 'rejected')),
    verified_by BIGINT REFERENCES users(id),
    verified_at TIMESTAMPTZ,
    remarks TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (status = 'verified'
            AND verified_by IS NOT NULL
            AND verified_at IS NOT NULL
            AND verified_available_cash IS NOT NULL)
        OR
        (status <> 'verified'
            AND verified_available_cash IS NULL)
    )
);

CREATE INDEX register_cash_reconciliations_user_idx
ON register_cash_reconciliations(user_id, created_at DESC);

ALTER TABLE register_cash_allocations
ADD COLUMN reconciliation_id BIGINT
REFERENCES register_cash_reconciliations(id);

CREATE UNIQUE INDEX register_cash_one_allocation_per_reconciliation
ON register_cash_allocations(reconciliation_id)
WHERE reconciliation_id IS NOT NULL;

ALTER TABLE register_cash_allocations
ADD CONSTRAINT register_cash_carry_forward_reference
CHECK (
    (funding_type = 'carry_forward'
        AND reconciliation_id IS NOT NULL)
    OR
    (funding_type <> 'carry_forward'
        AND reconciliation_id IS NULL)
);

CREATE TRIGGER register_cash_reconciliations_audit
AFTER INSERT OR UPDATE ON register_cash_reconciliations
FOR EACH ROW EXECUTE FUNCTION audit_change();
