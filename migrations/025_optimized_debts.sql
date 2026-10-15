-- Optimized Debts: audited capital contributions and purchase funding.
-- Disabled by default. No historical capital is inferred.

ALTER TABLE store_settings
ADD COLUMN optimized_debts_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE capital_entries (
    id BIGSERIAL PRIMARY KEY,
    entry_type TEXT NOT NULL CHECK (
        entry_type IN (
            'contribution',
            'withdrawal',
            'reinvestment',
            'correction'
        )
    ),
    amount BIGINT NOT NULL CHECK (amount <> 0),
    currency TEXT NOT NULL,
    funding_account_id BIGINT REFERENCES financial_accounts(id),
    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    effective_date DATE NOT NULL DEFAULT CURRENT_DATE,
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    corrected_entry_id BIGINT REFERENCES capital_entries(id),
    CHECK (
        (entry_type = 'correction' AND corrected_entry_id IS NOT NULL)
        OR
        (entry_type <> 'correction' AND corrected_entry_id IS NULL)
    )
);

CREATE INDEX capital_entries_currency_date_idx
ON capital_entries(currency, effective_date DESC, id DESC);

-- One correction per original capital entry, enforced under concurrency.
CREATE UNIQUE INDEX capital_entries_single_correction_idx
ON capital_entries(corrected_entry_id)
WHERE corrected_entry_id IS NOT NULL;

CREATE TABLE purchase_funding_allocations (
    id BIGSERIAL PRIMARY KEY,
    purchase_id BIGINT NOT NULL REFERENCES purchases(id),
    capital_entry_id BIGINT REFERENCES capital_entries(id),
    funding_type TEXT NOT NULL CHECK (
        funding_type IN (
            'superadmin_capital',
            'reinvested_revenue',
            'other'
        )
    ),
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX purchase_funding_purchase_idx
ON purchase_funding_allocations(purchase_id);

CREATE TRIGGER capital_entries_audit
AFTER INSERT ON capital_entries
FOR EACH ROW EXECUTE FUNCTION audit_change();

CREATE TRIGGER purchase_funding_allocations_audit
AFTER INSERT ON purchase_funding_allocations
FOR EACH ROW EXECUTE FUNCTION audit_change();

