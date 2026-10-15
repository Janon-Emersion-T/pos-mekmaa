# Mekmaa POS — Customer Credit and Debt Management

## Overview

Mekmaa POS supports customer contact records, opening debts,
credit sales, partial payments, customer statements, and
payment collections.

Customers do not require application login accounts.

## 1. Create a customer

1. Open **Customers**.
2. Select the option to add a customer.
3. Enter the customer's contact information.
4. If the customer already owes money from before the POS
   was introduced, enter **Already owed / opening balance**.
5. Save the customer.

An opening balance represents an existing debt. Creating it
does not create a new sale, add stock, or receive cash.

## 2. Record a credit sale

1. Open the POS.
2. Add products to the cart.
3. Select the customer.
4. Choose **Buy now, pay later**.
5. Enter any initial payment, or zero for a fully unpaid sale.
6. Review the transaction.
7. Confirm checkout.

Example:

Sale total: LKR 5,000
Initial payment: LKR 2,000
Remaining debt: LKR 3,000

The customer owes the unpaid amount.

## 3. Collect an outstanding payment

1. Open **Customers** or the POS debtors section.
2. Select the customer.
3. Review the unpaid sales and opening balances.
4. Select the relevant outstanding balance.
5. Enter the amount received.
6. Choose cash or card.
7. Confirm the collection.

Partial and full collections are supported.

Cash collections increase the current register's expected cash.
Card collections are recorded separately.

## 4. Collect an opening balance

Opening balances are separate from ordinary sales.

Select **Opening balance** when recording a payment against
debt entered during customer creation.

Do not create a fictional sale to collect old debt.

## 5. Customer statements

Customer account information can include:

- Sales and outstanding balances
- Payments and payment history
- Opening balances
- Amounts still owed

Review the statement before making corrections.

## 6. Credit restrictions

Customer credit policies can restrict new credit exposure.

The application also contains an optional Optimized Debts
policy that considers verified potential inventory margin
and existing non-owner debt.

Do not enable Optimized Debts until production inventory
costs and outstanding balances have been reconciled.

## 7. Incorrect collections

Do not record a second payment to compensate for an error.

Authorized administrators can use supported payment-reversal
workflows. Review the original payment and its associated
customer balance before reversing it.

## 8. Common problems

**Customer cannot buy on credit:** Review customer credit
restrictions, outstanding balances, and any enabled
Optimized Debts policy.

**Payment exceeds the outstanding amount:** Correct the
payment amount before submitting.

**Collection request interrupted:** Check whether the
original payment was recorded before attempting another.

**Incorrect opening debt:** Request an authorized opening
balance correction instead of editing contact details.

## Daily debt review

Management should reconcile customer collections against
receipts and the relevant cash or card records.

Outstanding debt is not the same as cash received.
