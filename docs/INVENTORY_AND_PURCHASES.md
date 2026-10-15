# Mekmaa POS — Inventory and Purchases

## 1. Product management

Admins and superadmins can manage products.

Open **Products** to:

- Add products.
- Assign product categories.
- Set selling prices and unit costs.
- Configure reorder levels.
- Edit existing product information.
- Review products with low stock.

New installations begin with an empty product catalog.

## 2. Categories

Open **Products → Manage categories**.

Create a category before assigning products to it.

Category names must be unique regardless of capitalization.

A category containing active products cannot be deleted until those
products are reassigned or removed.

## 3. Opening stock

Opening stock is entered when a product is created.

Subsequent stock movements should be recorded through purchases or
inventory adjustments.

Avoid changing stock without a documented reason.

## 4. Receive purchases

1. Open **Purchases**.
2. Enter the supplier.
3. Enter the supplier invoice reference when available.
4. Select the product.
5. Enter the quantity.
6. Enter the unit cost.
7. Select **Receive purchase**.
8. Confirm the purchase appears in the purchase history.

Purchases increase product stock.

The application also contains inventory-cost accounting logic. Financial
cost reporting should be reconciled against actual supplier documents.

## 5. Record daily stock adjustments

1. Open **Inventory**.
2. Select the product.
3. Enter the adjustment quantity:
   - Positive number to add stock.
   - Negative number to remove stock.
4. Enter the reason.
5. Select **Record daily adjustment**.

Examples of reasons include damaged goods, physical stock correction,
and verified stock discrepancies.

## 6. Review inventory

The Inventory page displays:

- Current stock.
- Product costs and selling prices.
- Reorder levels.
- Low-stock indicators.
- Daily stock adjustments.
- Recent stock movements.

Use the date filter to review movements for a selected day.

## 7. Product deletion

Admins and superadmins can delete products.

Historical sales, purchases and inventory records are retained.

The **Delete all products** operation requires confirmation with the
phrase `DELETE ALL PRODUCTS`.

This operation is destructive and should not be used for routine stock
adjustments.

## 8. Inventory limitations

The application does not currently provide expiry-date or batch-level
inventory tracking.

Products with unverified inventory costs can prevent new credit
transactions when Optimized Debts is enabled.

Do not interpret inventory margin as realized profit or available cash.
