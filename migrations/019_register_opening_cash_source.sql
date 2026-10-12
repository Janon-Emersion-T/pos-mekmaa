-- Distinguish new cash funding from cash already held.
-- Existing allocations retain legacy classification until reviewed.

ALTER TABLE register_cash_allocations
ADD COLUMN IF NOT EXISTS funding_type TEXT
NOT NULL DEFAULT 'legacy_unclassified'
CHECK (
    funding_type IN (
        'legacy_unclassified',
        'new_funds',
        'carry_forward'
    )
);

ALTER TABLE register_cash_allocations
ADD COLUMN IF NOT EXISTS funding_account_id BIGINT
REFERENCES financial_accounts(id);

ALTER TABLE register_cash_allocations
ADD COLUMN IF NOT EXISTS funding_posted_at TIMESTAMPTZ;

ALTER TABLE register_cash_allocations
ADD CONSTRAINT register_cash_funding_consistency
CHECK (
    funding_type <> 'carry_forward'
    OR funding_account_id IS NULL
);

CREATE INDEX IF NOT EXISTS register_cash_allocations_funding_idx
ON register_cash_allocations(funding_type, funding_posted_at);
