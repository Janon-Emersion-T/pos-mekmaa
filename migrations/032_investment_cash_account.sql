-- Introduce a dedicated business-owned investment cash account.
-- Existing balances are preserved; no historical data is migrated.

ALTER TABLE financial_accounts
DROP CONSTRAINT IF EXISTS financial_accounts_account_type_check;

ALTER TABLE financial_accounts
ADD CONSTRAINT financial_accounts_account_type_check
CHECK (
    account_type IN (
        'staff_cash',
        'superadmin_cash',
        'central_cash',
        'bank',
        'investment_cash'
    )
);

ALTER TABLE financial_accounts
DROP CONSTRAINT IF EXISTS financial_accounts_check;

ALTER TABLE financial_accounts
ADD CONSTRAINT financial_accounts_check
CHECK (
    (
        account_type IN ('staff_cash','superadmin_cash')
        AND user_id IS NOT NULL
    )
    OR
    (
        account_type IN ('central_cash','bank','investment_cash')
        AND user_id IS NULL
    )
);
