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

Open http://localhost:8886. The PostgreSQL schema is created on first launch. New installations start with an empty product catalog. The default connection is `postgres://counter:counter@localhost:5432/counter?sslmode=disable`; override it with `DATABASE_URL`. Set `ADDR` to change the listening address, for example `ADDR=:9000`.

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

## Products and navigation

Each menu has a bookmarkable URL (for example, `/products`, `/inventory`, and `/settings`), with browser Back/Forward and direct reload support.

Admins and superadmins can use **Products** to add and edit names, categories, prices, costs, reorder levels, and appearance. Opening stock is recorded when creating a product; subsequent stock changes use Inventory or Purchases. Category filters are generated from active products. Enter monetary amounts in the selected currency (for example, `125.50`); the API stores integer hundredths.

Use **Delete** to remove one product or **Delete all products** to clear the active catalog, including existing starter items. Bulk removal requires typing `DELETE ALL PRODUCTS`. Removal preserves historical sales, purchases, and inventory records. Deleted products stay removed after restarting; startup does not seed demo products.

## Sale confirmations and deletion

Complete order opens a review dialog showing line items, payment method, and total. Cancel or Escape returns to the cart without creating a sale. Confirm sale records the payment; checkout rejects a total that changed after review.

Only superadmins see **Delete sale** in Sales history, and the API enforces the same restriction. Deletion requires confirmation and a reason. It removes the sale from normal history, restores product stock once, and excludes the sale from its original register's expected cash. Closed-register counted cash remains unchanged. The sale, line items, deleting user, timestamp, reason, and stock reversal remain recorded for audit. This corrects an erroneous sale; it does not issue a cash or card refund.

## Register sessions and staff tracking

The store uses one shared open register session. Every sale is linked to that session and the authenticated staff member who completes it. The POS blocks checkout until a session is open and shows the session, opener, and current cashier. Confirmation and receipts include the cashier and session; Sales history shows staff IDs and offers session links to filter sales.

Register session history shows opening/closing staff and times, sale counts and totals, counted cash, expected cash at close, and differences. New sales and session actions save staff email snapshots alongside user IDs. Legacy records without staff attribution display “Not recorded”; attribution is not invented. Closing cash expectations from before this feature are also shown as not recorded.

Checkout, petty cash, sale deletion, and session closing lock the register row during their transactions. A concurrent sale either commits before closing and is included in closing totals, or is rejected after the session closes. The browser also sends the reviewed session ID to reject checkout against a replaced session. Expected cash at close is preserved even if a sale is subsequently deleted.

## Categories

Open **Products → Manage categories** to create, view, rename, and delete categories. Admins and superadmins can manage categories; cashiers can read them. Product forms select a saved category. Renames update assigned products and POS filters, and duplicate names are rejected without regard to case.

Existing category names are imported on upgrade, including categories on archived products. A category with active products cannot be deleted: reassign or delete those products first. Category deletion requires confirmation in the UI and keeps archived products and sales records intact. Category changes preserve any unfinished product form.
