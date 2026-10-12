-- Existing historical resolution records remain unchanged.
-- New service requests will provide a unique request_key.

ALTER TABLE cash_shortage_resolutions
ADD COLUMN IF NOT EXISTS request_key TEXT;

DO $$
BEGIN
    -- Allow existing historical records without request keys.
    ALTER TABLE cash_shortage_resolutions
        ALTER COLUMN request_key DROP NOT NULL;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cash_shortage_resolution_request_key_nonempty'
          AND conrelid = 'cash_shortage_resolutions'::regclass
    ) THEN
        ALTER TABLE cash_shortage_resolutions
        ADD CONSTRAINT cash_shortage_resolution_request_key_nonempty
        CHECK (
            request_key IS NULL
            OR length(trim(request_key)) > 0
        );
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS
    cash_shortage_resolution_request_key_unique
ON cash_shortage_resolutions(request_key);

-- A recovery resolution may have only one corresponding ledger credit.
CREATE UNIQUE INDEX IF NOT EXISTS
    financial_ledger_unique_shortage_recovery
ON financial_ledger_entries(reference_type, reference_id)
WHERE entry_type = 'shortage_recovery';
