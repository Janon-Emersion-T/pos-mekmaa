# Mekmaa POS — Superadmin Guide

## Overview

The superadmin role has the highest level of application access.

It includes administrative controls for staff accounts,
register cash allocations, financial reconciliation,
audit history, and selected financial operations.

## 1. Staff accounts

Open **Staff users**.

A superadmin can:

- Create cashier, admin, and superadmin accounts.
- Assign a temporary password.
- Enable or disable other staff accounts.
- Review account status.

Staff should change temporary passwords when required.

Do not share superadmin credentials.

## 2. Register cash allocations

Open **Register session** to access superadmin controls.

The allocation interface supports assigning opening cash
to staff from authorized funding sources.

Before allocating cash:

1. Select the staff member.
2. Select the available funding source.
3. Enter the amount.
4. Review available funding.
5. Submit the allocation.

The backend rejects certain allocations when the
source has insufficient available funds.

## 3. Register reconciliation

The superadmin interface displays register reconciliations.

Each reconciliation can include:

- Staff member
- Register session
- Counted cash
- Expected cash
- Cash difference
- Available reconciled cash
- Review status

Pending reconciliations can be verified or rejected.

Review the underlying session and cash movements before
making a decision.

Carry-forward funding is restricted by reconciliation
eligibility and available reconciled cash.

## 4. Audit history

Open **Audit history**.

Review sensitive actions and recorded changes.

Audit records help identify who performed a transaction
and when it occurred.

Do not treat deletion or correction as removal of the
historical audit trail.

## 5. Optimized Debts

Open **Settings** as superadmin.

The Optimized Debts panel provides information about:

- Whether the policy is enabled.
- Verified potential inventory margin.
- Outstanding non-owner customer debt.
- Available credit capacity.
- Stocked products with unverified costs.

Keep the policy disabled until the figures are verified.

See [Optimized Debts](OPTIMIZED_DEBTS.md).

## 6. Database reset

The Settings page contains a restricted database-reset
operation.

This is a destructive administrative action.

It can remove operational and transaction records while
preserving staff accounts and store currency.

Before any reset:

1. Obtain explicit business authorization.
2. Complete and verify a database backup.
3. Confirm that historical records may be removed.
4. Review the reset confirmation requirements.
5. Perform the operation only during an approved window.

Never use reset as a substitute for correcting an
individual transaction.

## 7. Capital administration

Capital operations are implemented through restricted
backend endpoints.

The inspected frontend does not establish a complete
capital-management screen.

Do not instruct ordinary staff to submit capital
transactions directly through the API.

See [Finance and Capital](FINANCE_AND_CAPITAL.md).

## 8. Superadmin daily review

- Review register differences.
- Review pending reconciliations.
- Check customer debt and collections.
- Investigate unusual refunds and corrections.
- Review inventory valuation.
- Check financial account movements.
- Review audit records for sensitive operations.
