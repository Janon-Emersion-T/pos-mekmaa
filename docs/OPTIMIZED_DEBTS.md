# Mekmaa POS — Optimized Debts

## Overview

Optimized Debts is an optional credit-control policy.

It restricts available customer credit using verified
potential inventory margin and existing non-owner debt.

The policy is disabled by default.

## 1. Access

Only superadmins can change the setting or access
the capacity endpoint.

Open **Settings** and locate the Optimized Debts panel.

## 2. Verified inventory margin

For stocked products with verified inventory costs,
the calculation considers positive potential margin.

For each eligible product:

Potential margin =
    selling price × current stock
    − recorded inventory cost balance

Negative product margins do not contribute positive
capacity.

Products with zero stock do not contribute margin.

## 3. Outstanding customer debt

The calculation includes outstanding balances from:

- Credit sales
- Customer opening balances

Owner accounts are excluded.

Debt is evaluated for the selected store currency.

## 4. Available credit

When all stocked inventory costs are verified:

Available credit =
    max(0, verified margin − outstanding debt)

Example:

Verified inventory margin: LKR 120,000
Outstanding non-owner debt: LKR 45,000

Available credit: LKR 75,000

This example assumes no stocked products have
unverified inventory costs.

## 5. Unverified inventory costs

If any stocked product has an unverified inventory
cost balance, the implementation sets global
available credit to zero.

This is a fail-closed protection.

It prevents credit decisions based on unreliable
inventory valuation.

## 6. What the capacity does not mean

Available credit is not:

- Physical cash in the shop
- Bank balance
- Realized profit
- Owner capital
- Guaranteed recoverable revenue

It is a calculated credit capacity based on
inventory-margin assumptions.

## 7. Enablement requirements

Before considering enablement:

1. Verify all stocked product costs.
2. Reconcile opening stock.
3. Reconcile customer opening debts.
4. Reconcile outstanding credit sales.
5. Review owner-account classification.
6. Confirm store currency.
7. Review the displayed capacity.
8. Test expected credit restrictions.
9. Obtain business approval.

Do not enable this feature merely because the
capacity appears positive.

## 8. Audit history

Changes to the enabled setting are recorded in
the optimized_debts_setting_changes table.

The record includes:

- Previous setting
- New setting
- User making the change
- Timestamp

## 9. Operational recommendation

Keep Optimized Debts disabled until production
financial reconciliation is complete.

Recheck the capacity after significant stock,
cost, debt, or refund changes.
