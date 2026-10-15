-- Bind cashier cash handovers to the session in which they were requested and
-- make browser retries idempotent without changing historical transfers.

ALTER TABLE financial_transfers
ADD COLUMN session_id BIGINT REFERENCES register_sessions(id),
ADD COLUMN request_id TEXT,
ADD COLUMN affects_session_cash BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE financial_transfers
ADD CONSTRAINT financial_transfer_session_effect
CHECK (NOT affects_session_cash OR session_id IS NOT NULL);

CREATE UNIQUE INDEX financial_transfers_request_id_unique
ON financial_transfers(requested_by, request_id)
WHERE request_id IS NOT NULL;

CREATE INDEX financial_transfers_requester_idx
ON financial_transfers(requested_by, created_at DESC);

CREATE UNIQUE INDEX financial_ledger_transfer_reference_unique
ON financial_ledger_entries(account_id, entry_type, reference_type, reference_id)
WHERE reference_type='financial_transfer';
