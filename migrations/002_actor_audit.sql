ALTER TABLE register_sessions ADD COLUMN IF NOT EXISTS opened_by BIGINT REFERENCES users(id), ADD COLUMN IF NOT EXISTS closed_by BIGINT REFERENCES users(id);
ALTER TABLE sales ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id);
ALTER TABLE petty_cash_entries ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id);
ALTER TABLE purchases ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id);
ALTER TABLE inventory_movements ADD COLUMN IF NOT EXISTS created_by BIGINT REFERENCES users(id);

CREATE INDEX IF NOT EXISTS sales_created_by_idx ON sales(created_by);
CREATE INDEX IF NOT EXISTS register_sessions_opened_by_idx ON register_sessions(opened_by);
CREATE INDEX IF NOT EXISTS petty_cash_created_by_idx ON petty_cash_entries(created_by);
CREATE INDEX IF NOT EXISTS purchases_created_by_idx ON purchases(created_by);
CREATE INDEX IF NOT EXISTS inventory_movements_created_by_idx ON inventory_movements(created_by);
