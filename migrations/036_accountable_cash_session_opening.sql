-- Track the register session currently accounting for cash in a pending
-- handover. The original session_id remains the immutable request context.

ALTER TABLE financial_transfers
ADD COLUMN cash_session_id BIGINT REFERENCES register_sessions(id);

ALTER TABLE financial_transfers
DROP CONSTRAINT financial_transfer_session_effect;

ALTER TABLE financial_transfers
ADD CONSTRAINT financial_transfer_session_effect
CHECK (NOT affects_session_cash OR cash_session_id IS NOT NULL);

UPDATE financial_transfers
SET cash_session_id=session_id
WHERE session_id IS NOT NULL;

CREATE INDEX financial_transfers_cash_session_idx
ON financial_transfers(cash_session_id)
WHERE cash_session_id IS NOT NULL;
