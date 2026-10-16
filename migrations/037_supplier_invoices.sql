-- Stable supplier invoice identities and invoice-level financial transactions.
-- Historical purchases are grouped only when their trimmed supplier and invoice
-- text match exactly, so pre-existing case variants are never silently merged.

CREATE TABLE supplier_invoices (
    id BIGSERIAL PRIMARY KEY,
    supplier TEXT NOT NULL,
    invoice_number TEXT NOT NULL,
    supplier_key TEXT NOT NULL,
    invoice_key TEXT NOT NULL,
    currency TEXT NOT NULL,
    legacy_purchase_id BIGINT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (legacy_purchase_id IS NULL OR invoice_number='')
);

CREATE UNIQUE INDEX supplier_invoices_exact_unique
ON supplier_invoices(supplier,invoice_number,currency)
WHERE legacy_purchase_id IS NULL;

ALTER TABLE purchases ADD COLUMN invoice_id BIGINT REFERENCES supplier_invoices(id);

INSERT INTO supplier_invoices(supplier,invoice_number,supplier_key,invoice_key,currency,legacy_purchase_id,created_at)
SELECT trim(supplier),trim(invoice_number),lower(trim(supplier)),lower(trim(invoice_number)),currency,NULL,min(created_at)
FROM purchases
WHERE trim(invoice_number)<>''
GROUP BY trim(supplier),trim(invoice_number),currency;

INSERT INTO supplier_invoices(supplier,invoice_number,supplier_key,invoice_key,currency,legacy_purchase_id,created_at)
SELECT trim(supplier),'',lower(trim(supplier)),'',currency,id,created_at
FROM purchases
WHERE trim(invoice_number)='';

UPDATE purchases p
SET invoice_id=i.id
FROM supplier_invoices i
WHERE i.supplier=trim(p.supplier)
  AND i.invoice_number=trim(p.invoice_number)
  AND i.currency=p.currency
  AND ((trim(p.invoice_number)<>'' AND i.legacy_purchase_id IS NULL)
       OR i.legacy_purchase_id=p.id);

CREATE INDEX purchases_invoice_idx ON purchases(invoice_id,id);
CREATE INDEX supplier_invoices_lookup_idx ON supplier_invoices(supplier_key,invoice_key,currency);

CREATE TABLE supplier_invoice_transactions (
    id BIGSERIAL PRIMARY KEY,
    invoice_id BIGINT NOT NULL REFERENCES supplier_invoices(id),
    transaction_type TEXT NOT NULL CHECK(transaction_type IN ('payment','refund')),
    financial_account_id BIGINT NOT NULL REFERENCES financial_accounts(id),
    amount BIGINT NOT NULL CHECK(amount > 0),
    currency TEXT NOT NULL,
    funding_type TEXT CHECK(funding_type IN ('superadmin_capital','reinvested_revenue','other')),
    request_id TEXT NOT NULL UNIQUE,
    reference TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((transaction_type='payment' AND funding_type IS NOT NULL AND reason='') OR
           (transaction_type='refund' AND funding_type IS NULL AND reason<>''))
);

ALTER TABLE purchase_payments
ADD COLUMN invoice_transaction_id BIGINT REFERENCES supplier_invoice_transactions(id);

CREATE INDEX supplier_invoice_transactions_invoice_idx
ON supplier_invoice_transactions(invoice_id,created_at,id);

CREATE TRIGGER supplier_invoices_audit
AFTER INSERT OR UPDATE ON supplier_invoices
FOR EACH ROW EXECUTE FUNCTION audit_change();

CREATE TRIGGER supplier_invoice_transactions_audit
AFTER INSERT ON supplier_invoice_transactions
FOR EACH ROW EXECUTE FUNCTION audit_change();
