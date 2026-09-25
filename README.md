# Counter POS

A café point-of-sale application built with Go, PostgreSQL, and compiled Tailwind CSS. It includes register sessions, petty cash, purchases, inventory movements and adjustments, stock-aware checkout, receipts, and sales history. Inventory is intentionally simple: there is no expiry-date or batch control.

## Run

Create a PostgreSQL database and user using your PostgreSQL administration method:

```sql
CREATE USER counter WITH PASSWORD 'counter';
CREATE DATABASE counter OWNER counter;
```

Then start the application:

```sh
DATABASE_URL='postgres://counter:counter@localhost:5432/counter?sslmode=disable' go run .
```

Open http://localhost:8886. The PostgreSQL schema and starter products are created on first launch. The default connection is `postgres://counter:counter@localhost:5432/counter?sslmode=disable`; override it with `DATABASE_URL`. Set `ADDR` to change the listening address, for example `ADDR=:9000`.

`GET /healthz` is the unauthenticated readiness endpoint. It returns `200` only while PostgreSQL is reachable. Session cookies are marked secure when Nginx sends `X-Forwarded-Proto: https`; `COOKIE_SECURE=true` can enforce secure cookies independently of proxy headers.

The initial superadmin is seeded automatically on first launch. There is no public registration route; additional staff accounts should be created through an administrator workflow.

Role access is enforced by the API: cashiers can operate sales, register sessions, sales history, and petty cash; admins also manage inventory and purchases; superadmins additionally create and manage staff users.

Database changes live in numbered files under `migrations/` and are recorded in `schema_migrations`. Financial and inventory mutations record the responsible user for audit purposes.

## Develop styles

```sh
npm ci
npm run css
```

Static assets are embedded in the Go binary. Rebuild/restart Go after editing them.

```sh
go test ./...
go build -o counter .
```

Prices use integer hundredths. Admins and superadmins can choose the store currency in Settings (USD by default; includes LKR, INR, EUR, GBP, AUD, CAD, SGD, AED, and SAR). The selection is saved in PostgreSQL and shared by all staff. It applies to all displayed amounts, including history, without converting numeric values. Supported currencies use two decimal places. Open a register session before checkout. Cash sales and petty-cash movements feed its expected closing cash. Purchases increase inventory; sales reduce it; adjustments require a reason. These operations are transactional and leave an inventory movement trail. Card payments remain manual records and no money is charged. Add authentication, authorization, backups, refunds, and a payment processor before public or production use.
