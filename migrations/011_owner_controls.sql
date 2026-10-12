CREATE TABLE register_cash_allocations(
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 amount INTEGER NOT NULL CHECK(amount>=0),
 currency TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN('pending','used','cancelled')),
 assigned_by BIGINT NOT NULL REFERENCES users(id),
 assigned_by_email TEXT NOT NULL,
 used_session_id BIGINT REFERENCES register_sessions(id),
 note TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 used_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX register_cash_one_pending_per_user ON register_cash_allocations(user_id) WHERE status='pending';
CREATE INDEX register_cash_allocations_user_idx ON register_cash_allocations(user_id,id DESC);
CREATE TRIGGER register_cash_allocations_audit AFTER INSERT OR UPDATE ON register_cash_allocations FOR EACH ROW EXECUTE FUNCTION audit_change();
