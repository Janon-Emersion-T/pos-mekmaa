ALTER TABLE sales ADD COLUMN IF NOT EXISTS cashier_email TEXT;
UPDATE sales s SET cashier_email=u.email FROM users u WHERE s.created_by=u.id AND s.cashier_email IS NULL;
ALTER TABLE register_sessions ADD COLUMN IF NOT EXISTS opened_by_email TEXT,
  ADD COLUMN IF NOT EXISTS closed_by_email TEXT,
  ADD COLUMN IF NOT EXISTS closing_expected_cash INTEGER;
UPDATE register_sessions r SET opened_by_email=u.email FROM users u WHERE r.opened_by=u.id AND r.opened_by_email IS NULL;
UPDATE register_sessions r SET closed_by_email=u.email FROM users u WHERE r.closed_by=u.id AND r.closed_by_email IS NULL;
CREATE INDEX IF NOT EXISTS sales_session_id_idx ON sales(session_id);
