-- Reconciliation safety snapshots for verified carry-forward cash.

ALTER TABLE register_cash_reconciliations
ADD COLUMN IF NOT EXISTS cash_difference BIGINT,
ADD COLUMN IF NOT EXISTS shortage_cash BIGINT,
ADD COLUMN IF NOT EXISTS closed_ledger_entry_id BIGINT,
ADD COLUMN IF NOT EXISTS verified_ledger_entry_id BIGINT;

UPDATE register_cash_reconciliations
SET cash_difference = counted_cash - expected_cash,
    shortage_cash = GREATEST(expected_cash - counted_cash, 0),
    closed_ledger_entry_id = COALESCE(closed_ledger_entry_id, 0)
WHERE cash_difference IS NULL
   OR shortage_cash IS NULL
   OR closed_ledger_entry_id IS NULL;

ALTER TABLE register_cash_reconciliations
ALTER COLUMN cash_difference SET NOT NULL,
ALTER COLUMN shortage_cash SET NOT NULL,
ALTER COLUMN closed_ledger_entry_id SET NOT NULL;

ALTER TABLE register_cash_reconciliations
ADD CONSTRAINT register_cash_reconciliation_amounts
CHECK (
    cash_difference = counted_cash - expected_cash
    AND shortage_cash = GREATEST(expected_cash - counted_cash, 0)
);

ALTER TABLE register_cash_reconciliations
ADD CONSTRAINT register_cash_reconciliation_verified_ledger
CHECK (
    (status = 'verified' AND verified_ledger_entry_id IS NOT NULL)
    OR
    (status <> 'verified' AND verified_ledger_entry_id IS NULL)
);

CREATE INDEX IF NOT EXISTS register_cash_reconciliations_status_idx
ON register_cash_reconciliations(status, created_at DESC);
