-- Prevent duplicate shortage records for the same session.
CREATE UNIQUE INDEX IF NOT EXISTS
    cash_shortages_one_per_session
ON cash_shortages(session_id)
WHERE session_id IS NOT NULL;

-- A shortage can be resolved only when fully recovered
-- or waived.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cash_shortages_resolution_consistency'
          AND conrelid = 'cash_shortages'::regclass
    ) THEN
        ALTER TABLE cash_shortages
        ADD CONSTRAINT cash_shortages_resolution_consistency
        CHECK (
            (status = 'resolved' AND
             recovered_amount + waived_amount = original_amount)
            OR
            (status = 'unresolved' AND
             recovered_amount + waived_amount < original_amount)
        );
    END IF;
END
$$;
