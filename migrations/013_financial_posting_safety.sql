-- Prevent duplicate debit or credit postings for the same reference.
-- Each financial transfer reference may have one transfer_out and
-- one transfer_in entry.

CREATE UNIQUE INDEX IF NOT EXISTS
financial_ledger_unique_transfer_posting
ON financial_ledger_entries (
    reference_type,
    reference_id,
    entry_type
)
WHERE entry_type IN ('transfer_out', 'transfer_in');
