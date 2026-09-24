# Counter POS

A café point-of-sale application built with Go, PostgreSQL, and compiled Tailwind CSS. It includes register sessions, petty cash, purchases, inventory movements and adjustments, stock-aware checkout, receipts, and sales history. Inventory is intentionally simple: there is no expiry-date or batch control.

## Run

```sh
docker compose up -d postgres
go run .
```

Open http://localhost:8080. The PostgreSQL schema and starter products are created on first launch. The default connection is `postgres://counter:counter@localhost:5433/counter?sslmode=disable`; override it with `DATABASE_URL`. Set `ADDR` to change the listening address.

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

Prices use integer cents and USD. Open a register session before checkout. Cash sales and petty-cash movements feed its expected closing cash. Purchases increase inventory; sales reduce it; adjustments require a reason. These operations are transactional and leave an inventory movement trail. Card payments remain manual records and no money is charged. Add authentication, authorization, backups, refunds, and a payment processor before public or production use.
