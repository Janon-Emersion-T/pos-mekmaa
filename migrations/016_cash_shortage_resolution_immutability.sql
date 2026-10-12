CREATE OR REPLACE FUNCTION prevent_cash_shortage_resolution_changes()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION
        'Cash shortage resolution history is append-only';
END;
$$;

DROP TRIGGER IF EXISTS
    cash_shortage_resolutions_immutable
ON cash_shortage_resolutions;

CREATE TRIGGER cash_shortage_resolutions_immutable
BEFORE UPDATE OR DELETE
ON cash_shortage_resolutions
FOR EACH ROW
EXECUTE FUNCTION prevent_cash_shortage_resolution_changes();
