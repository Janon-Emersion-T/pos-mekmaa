-- Persistent financial accounts.
-- This migration introduces structures only.
-- Existing cash balances are not automatically migrated.

CREATE TABLE financial_accounts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id),
    account_type TEXT NOT NULL
        CHECK (account_type IN (
            'staff_cash',
            'superadmin_cash',
            'central_cash',
            'bank'
        )),
    currency TEXT NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (account_type IN ('staff_cash','superadmin_cash') AND user_id IS NOT NULL)
        OR
        (account_type IN ('central_cash','bank') AND user_id IS NULL)
    )
);

CREATE UNIQUE INDEX financial_user_account_unique
ON financial_accounts(user_id, account_type, currency)
WHERE user_id IS NOT NULL;

CREATE UNIQUE INDEX financial_business_account_unique
ON financial_accounts(account_type, currency)
WHERE user_id IS NULL;

CREATE TABLE financial_transfers (
    id BIGSERIAL PRIMARY KEY,
    source_account_id BIGINT NOT NULL REFERENCES financial_accounts(id),
    destination_account_id BIGINT NOT NULL REFERENCES financial_accounts(id),
    requested_amount BIGINT NOT NULL CHECK (requested_amount > 0),
    approved_amount BIGINT CHECK (approved_amount >= 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN (
            'pending',
            'approved',
            'partially_approved',
            'rejected'
        )),
    remarks TEXT NOT NULL DEFAULT '',
    decision_remarks TEXT NOT NULL DEFAULT '',
    requested_by BIGINT NOT NULL REFERENCES users(id),
    reviewed_by BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    CHECK (source_account_id <> destination_account_id),
    CHECK (approved_amount IS NULL OR approved_amount <= requested_amount)
);

CREATE INDEX financial_transfers_status_idx
ON financial_transfers(status, created_at DESC);

CREATE TABLE financial_ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES financial_accounts(id),
    amount BIGINT NOT NULL CHECK (amount <> 0),
    entry_type TEXT NOT NULL,
    reference_type TEXT NOT NULL,
    reference_id BIGINT,
    remarks TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX financial_ledger_account_idx
ON financial_ledger_entries(account_id, id DESC);

CREATE TABLE cash_shortages (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    session_id BIGINT REFERENCES register_sessions(id),
    currency TEXT NOT NULL,
    original_amount BIGINT NOT NULL CHECK (original_amount > 0),
    recovered_amount BIGINT NOT NULL DEFAULT 0,
    waived_amount BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'unresolved'
        CHECK (status IN ('unresolved','resolved')),
    remarks TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (recovered_amount >= 0),
    CHECK (waived_amount >= 0),
    CHECK (recovered_amount + waived_amount <= original_amount)
);

CREATE INDEX cash_shortages_user_idx
ON cash_shortages(user_id, status);

-- Reuse the existing database audit mechanism.
CREATE TRIGGER financial_accounts_audit
AFTER INSERT OR UPDATE ON financial_accounts
FOR EACH ROW EXECUTE FUNCTION audit_change();

CREATE TRIGGER financial_transfers_audit
AFTER INSERT OR UPDATE ON financial_transfers
FOR EACH ROW EXECUTE FUNCTION audit_change();

CREATE TRIGGER financial_ledger_entries_audit
AFTER INSERT ON financial_ledger_entries
FOR EACH ROW EXECUTE FUNCTION audit_change();

CREATE TRIGGER cash_shortages_audit
AFTER INSERT OR UPDATE ON cash_shortages
FOR EACH ROW EXECUTE FUNCTION audit_change();
