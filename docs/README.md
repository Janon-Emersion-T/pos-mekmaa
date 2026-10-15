# Mekmaa POS — Documentation

This documentation describes the Mekmaa POS application deployed at
https://fridge.mekmaa.com/.

The application uses Go, PostgreSQL, and Tailwind CSS.

## User guides

| Guide | Purpose |
|---|---|
| [Cashier Guide](CASHIER_GUIDE.md) | Daily register operation, checkout, collections and closing |
| [Inventory and Purchases](INVENTORY_AND_PURCHASES.md) | Products, categories, stock and purchases |
| [Customer Credit and Debts](CUSTOMER_CREDIT_AND_DEBTS.md) | Customers, credit sales, opening debts and collections |
| [Sales and Returns](SALES_AND_RETURNS.md) | Checkout, receipts, refunds and corrections |
| [Cash Sessions and Transfers](CASH_SESSIONS_AND_TRANSFERS.md) | Register cash, petty cash and reconciliation |

## Existing technical documentation

- [Main README](../README.md)
- [Backup and recovery](../ops/README.md)

## User roles

**Cashier:** Sales, customers, register sessions, petty cash and permitted customer collections.

**Admin:** Cashier operations plus product management, inventory, purchases, reports and permitted refunds.

**Superadmin:** Administrative operations plus staff management, audit history, database reset and restricted financial controls.

Access is enforced by the backend. Individual operations may have additional restrictions.

## Important financial principles

- Monetary values are stored internally in minor units.
- Credit sales create customer debt.
- Cash collections affect register cash.
- Card transactions are recorded manually; the application does not process card payments.
- Inventory adjustments require a reason.
- Optimized Debts uses provisional inventory-margin capacity, not available cash or realized profit.
- The Optimized Debts policy should remain disabled until production figures are reconciled.

## Documentation status

This is the first documentation batch. Additional manuals covering customer
credit, sales and returns, superadmin controls, finance, deployment, security
and troubleshooting will follow.

The application source code and tests remain the authoritative reference for
implemented behavior.
