CREATE TABLE categories (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 100)
);
CREATE UNIQUE INDEX categories_name_unique ON categories(lower(name));
ALTER TABLE products ADD COLUMN category_id BIGINT REFERENCES categories(id) ON DELETE SET NULL;
INSERT INTO categories(name)
SELECT min(COALESCE(NULLIF(btrim(category),''),'Uncategorized'))
FROM products GROUP BY lower(COALESCE(NULLIF(btrim(category),''),'Uncategorized'));
UPDATE products p SET category_id=c.id,category=c.name
FROM categories c WHERE lower(COALESCE(NULLIF(btrim(p.category),''),'Uncategorized'))=lower(c.name);
CREATE INDEX products_category_id_idx ON products(category_id);
