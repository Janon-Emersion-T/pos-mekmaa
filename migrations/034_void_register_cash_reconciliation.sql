-- Permit audited voiding of accidentally verified reconciliations.
ALTER TABLE register_cash_reconciliations
DROP CONSTRAINT IF EXISTS register_cash_reconciliations_status_check;

ALTER TABLE register_cash_reconciliations
ADD CONSTRAINT register_cash_reconciliations_status_check
CHECK (status IN ('pending', 'verified', 'rejected', 'voided'));

ALTER TABLE register_cash_reconciliations
ADD COLUMN voided_by BIGINT REFERENCES users(id),
ADD COLUMN voided_at TIMESTAMPTZ,
ADD COLUMN void_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE register_cash_reconciliations
ADD CONSTRAINT register_cash_reconciliation_void_metadata_check
CHECK (
    (status = 'voided'
     AND voided_by IS NOT NULL
     AND voided_at IS NOT NULL
     AND length(trim(void_reason)) >= 10)
    OR
    (status <> 'voided'
     AND voided_by IS NULL
     AND voided_at IS NULL
     AND void_reason = '')
);
