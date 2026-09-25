ALTER TABLE sales ADD COLUMN currency TEXT, ADD COLUMN currency_inferred BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN cash_received INTEGER, ADD COLUMN change_due INTEGER;
UPDATE sales SET currency=(SELECT currency FROM store_settings WHERE id=1),currency_inferred=true;
ALTER TABLE sales ALTER COLUMN currency SET NOT NULL;
ALTER TABLE register_sessions ADD COLUMN currency TEXT;
UPDATE register_sessions SET currency=(SELECT currency FROM store_settings WHERE id=1);
ALTER TABLE register_sessions ALTER COLUMN currency SET NOT NULL;
ALTER TABLE purchases ADD COLUMN currency TEXT;
UPDATE purchases SET currency=(SELECT currency FROM store_settings WHERE id=1);
ALTER TABLE purchases ALTER COLUMN currency SET NOT NULL;
ALTER TABLE petty_cash_entries ADD COLUMN currency TEXT;
UPDATE petty_cash_entries p SET currency=r.currency FROM register_sessions r WHERE p.session_id=r.id;
ALTER TABLE petty_cash_entries ALTER COLUMN currency SET NOT NULL;

-- Defaults keep older clients and maintenance inserts compatible, while snapshotting at insert time.
CREATE FUNCTION set_transaction_currency() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.currency IS NULL THEN
   SELECT currency INTO NEW.currency FROM store_settings WHERE id=1;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER sale_currency BEFORE INSERT ON sales FOR EACH ROW EXECUTE FUNCTION set_transaction_currency();
CREATE TRIGGER session_currency BEFORE INSERT ON register_sessions FOR EACH ROW EXECUTE FUNCTION set_transaction_currency();
CREATE TRIGGER purchase_currency BEFORE INSERT ON purchases FOR EACH ROW EXECUTE FUNCTION set_transaction_currency();
CREATE TRIGGER petty_currency BEFORE INSERT ON petty_cash_entries FOR EACH ROW EXECUTE FUNCTION set_transaction_currency();

CREATE TABLE request_results(request_id TEXT PRIMARY KEY,actor_id BIGINT NOT NULL REFERENCES users(id),kind TEXT NOT NULL,request_hash TEXT NOT NULL,response JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE refunds(id BIGSERIAL PRIMARY KEY,sale_id BIGINT NOT NULL REFERENCES sales(id),session_id BIGINT NOT NULL REFERENCES register_sessions(id),created_at TIMESTAMPTZ NOT NULL DEFAULT now(),created_by BIGINT NOT NULL REFERENCES users(id),cashier_email TEXT NOT NULL,reason TEXT NOT NULL,total INTEGER NOT NULL CHECK(total>=0),payment TEXT NOT NULL CHECK(payment IN('cash','card')),currency TEXT NOT NULL);
CREATE TABLE refund_items(id BIGSERIAL PRIMARY KEY,refund_id BIGINT NOT NULL REFERENCES refunds(id),sale_item_id BIGINT NOT NULL REFERENCES sale_items(id),quantity INTEGER NOT NULL CHECK(quantity>0),restock BOOLEAN NOT NULL DEFAULT false,amount INTEGER NOT NULL CHECK(amount>=0));
CREATE INDEX refund_sale_idx ON refunds(sale_id);
CREATE INDEX refund_session_idx ON refunds(session_id);
CREATE INDEX refund_items_sale_item_idx ON refund_items(sale_item_id);
ALTER TABLE products ADD COLUMN sku TEXT NOT NULL DEFAULT '', ADD COLUMN barcode TEXT NOT NULL DEFAULT '', ADD COLUMN image_id BIGINT;
CREATE UNIQUE INDEX product_sku_unique ON products(lower(sku)) WHERE sku<>'';
CREATE UNIQUE INDEX product_barcode_unique ON products(barcode) WHERE barcode<>'';
CREATE TABLE product_images(id BIGSERIAL PRIMARY KEY,mime_type TEXT NOT NULL,data BYTEA NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
ALTER TABLE products ADD FOREIGN KEY(image_id) REFERENCES product_images(id);
CREATE TABLE audit_events(id BIGSERIAL PRIMARY KEY,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),actor_id BIGINT,actor_email TEXT,entity TEXT NOT NULL,entity_id BIGINT NOT NULL,action TEXT NOT NULL,before_data JSONB,after_data JSONB);
CREATE INDEX audit_created_idx ON audit_events(id DESC);
CREATE FUNCTION audit_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE before_value JSONB; after_value JSONB; actor BIGINT; email TEXT;
BEGIN
 IF TG_OP<>'INSERT' THEN before_value:=to_jsonb(OLD)-'password_hash'; END IF;
 IF TG_OP<>'DELETE' THEN after_value:=to_jsonb(NEW)-'password_hash'; END IF;
 actor:=NULLIF(current_setting('app.actor_id',true),'')::bigint;
 email:=NULLIF(current_setting('app.actor_email',true),'');
 INSERT INTO audit_events(actor_id,actor_email,entity,entity_id,action,before_data,after_data)
 VALUES(actor,email,TG_TABLE_NAME,COALESCE((after_value->>'id')::bigint,(before_value->>'id')::bigint),TG_OP,before_value,after_value);
 RETURN COALESCE(NEW,OLD);
END $$;
CREATE TRIGGER products_audit AFTER INSERT OR UPDATE OR DELETE ON products FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER categories_audit AFTER INSERT OR UPDATE OR DELETE ON categories FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER sales_audit AFTER INSERT OR UPDATE OR DELETE ON sales FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER refunds_audit AFTER INSERT ON refunds FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER users_audit AFTER INSERT OR UPDATE OR DELETE ON users FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER settings_audit AFTER UPDATE ON store_settings FOR EACH ROW EXECUTE FUNCTION audit_change();
CREATE TRIGGER register_audit AFTER INSERT OR UPDATE ON register_sessions FOR EACH ROW EXECUTE FUNCTION audit_change();
ALTER TABLE users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false;
UPDATE users SET must_change_password=true WHERE password_hash='$2a$10$kBHVZlCmocrXwUFAy7B3leIkgjZkXJNmFSStd.vbtxZHPGE0FcZVG';
CREATE TABLE login_limits(key TEXT PRIMARY KEY,attempts INTEGER NOT NULL,started_at TIMESTAMPTZ NOT NULL);
