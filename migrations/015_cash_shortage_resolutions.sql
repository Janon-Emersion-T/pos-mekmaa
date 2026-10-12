CREATE TABLE IF NOT EXISTS cash_shortage_resolutions (
    id BIGSERIAL PRIMARY KEY,

    shortage_id BIGINT NOT NULL
        REFERENCES cash_shortages(id),

    action TEXT NOT NULL
        CHECK (action IN ('recover','waive')),

    amount BIGINT NOT NULL
        CHECK (amount > 0),

    remarks TEXT NOT NULL
        CHECK (length(trim(remarks)) > 0),

    resolved_by BIGINT NOT NULL
        REFERENCES users(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS
    cash_shortage_resolutions_shortage_idx
ON cash_shortage_resolutions(shortage_id, created_at);

DROP TRIGGER IF EXISTS
    cash_shortage_resolutions_audit
ON cash_shortage_resolutions;

CREATE TRIGGER cash_shortage_resolutions_audit
AFTER INSERT OR UPDATE OR DELETE
ON cash_shortage_resolutions
FOR EACH ROW
EXECUTE FUNCTION audit_change();
