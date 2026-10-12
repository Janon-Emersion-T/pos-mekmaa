-- Each operational cash event may be posted only once.
-- Transfer and shortage recovery protections already exist.

CREATE UNIQUE INDEX IF NOT EXISTS
financial_ledger_unique_operational_posting
ON financial_ledger_entries (
    reference_type,
    reference_id,
    entry_type
)
WHERE entry_type IN (
    'cash_sale',
    'cash_collection',
    'petty_cash_in',
    'petty_cash_out',
    'cash_refund',
    'opening_allocation'
);
