# Mekmaa POS — Sales, Returns and Corrections

## 1. Complete a sale

1. Open your own register session.
2. Select products and quantities in the POS.
3. Review the cart.
4. Select the payment method.
5. Choose **Complete order**.
6. Review the confirmation.
7. Confirm the sale.
8. Provide the receipt.

Checkout validates stock, the reviewed total, and the
authenticated user's active register session.

## 2. Cash sales

Enter the amount received when requested.

The POS calculates change. Confirm the physical cash
received before completing the transaction.

Cash sales affect expected register cash.

## 3. Card sales

Card transactions are recorded manually.

The POS does not charge a payment card or communicate
with a payment processor.

Confirm the external card-terminal payment before
recording the sale as paid.

## 4. Credit sales

Select a customer and **Buy now, pay later**.

The sale may include an initial payment. Any unpaid
balance becomes customer debt.

## 5. Find a previous sale

Open **Sales history**.

Use available filters to find the relevant receipt.
Sales can be associated with cashier and register-session
information.

Open a receipt to review the original transaction.

## 6. Returns and refunds

Authorized admins and superadmins can perform refunds
through the supported sale-refund workflow.

For credit sales, a return can first cancel unpaid debt.
Any refundable amount beyond that must be handled
according to the recorded payments.

Example:

Original sale: LKR 4,000
Already paid: LKR 1,000
Outstanding: LKR 3,000

A full return may cancel the LKR 3,000 outstanding debt
and require handling the LKR 1,000 already received.

The exact refund outcome must be reviewed in the
application before confirmation.

Check the returned stock and financial records afterward.

## 7. Delete an incorrect sale

Only superadmins can delete sales.

Deletion requires confirmation and a reason.

The application records the deletion for audit and
restores the applicable product stock.

**Deletion is not a customer refund.**

Do not use deletion to conceal a genuine sale or to
replace the authorized refund process.

Sales with recorded payments have additional deletion
restrictions to protect payment history.

## 8. Interrupted checkout or refund

The application includes request-recovery safeguards
designed to prevent duplicate processing.

If a connection fails:

1. Check Sales history or the original receipt.
2. Determine whether the operation was recorded.
3. Use the application's recovery or retry behavior.
4. Do not manually submit a duplicate transaction.

## 9. Daily sales checks

- Review cash sales.
- Review manually recorded card sales.
- Review customer credit sales.
- Verify collections separately from new sales.
- Review refunds and corrections.
- Investigate unexpected inventory differences.
- Reconcile register cash before closing.
