-- Immutable history of Optimized Debts setting changes.

CREATE TABLE optimized_debts_setting_changes (
    id BIGSERIAL PRIMARY KEY,
    previous_enabled BOOLEAN NOT NULL,
    new_enabled BOOLEAN NOT NULL,
    changed_by BIGINT NOT NULL REFERENCES users(id),
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX optimized_debts_setting_changes_recent_idx
ON optimized_debts_setting_changes(changed_at DESC, id DESC);

CREATE TRIGGER optimized_debts_setting_changes_audit
AFTER INSERT ON optimized_debts_setting_changes
FOR EACH ROW EXECUTE FUNCTION audit_change();
