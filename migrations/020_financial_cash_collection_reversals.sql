-- Preserve original cash collections and record reversals separately.
-- One reversal record may affect the financial ledger only once.

CREATE UNIQUE INDEX IF NOT EXISTS
financial_ledger_unique_cash_collection_reversal
ON financial_ledger_entries(reference_type, reference_id, entry_type)
WHERE entry_type = 'cash_collection_reversal';
