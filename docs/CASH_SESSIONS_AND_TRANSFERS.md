# Mekmaa POS — Register Sessions and Cash Reconciliation

## Overview

Each staff member operates an individual register session.

The application tracks opening cash, cash sales, cash
collections, petty-cash movements, closing amounts, and
differences.

## 1. Opening cash allocation

A superadmin can assign opening cash using the authorized
register cash allocation workflow.

The allocation is associated with a funding source.

The application checks relevant funding and reconciliation
restrictions before permitting certain allocations.

## 2. Open a register session

1. Sign in as the assigned staff member.
2. Open **Register session**.
3. Review the assigned opening cash.
4. Enter the permitted opening amount.
5. Select **Open register session**.

A different staff member's open session does not enable
checkout for your account.

## 3. Expected register cash

Expected cash is calculated from recorded cash movements.

A simplified example:

Opening cash: LKR 2,000
Cash sales: LKR 10,000
Cash collections: LKR 1,500
Petty cash out: LKR 500

Expected cash: LKR 13,000

This example assumes no other relevant cash movements.

Card payments and unpaid credit sales do not add physical
cash to the register.

## 4. Petty cash

Open **Petty cash**.

Choose:

- **Cash out** for money removed from the register.
- **Cash in** for money added to the register.

Enter the amount, category, and note.

Always record the actual purpose of a cash movement.

## 5. Close a session

1. Open **Register session**.
2. Review the expected cash.
3. Physically count the available cash.
4. Enter **Counted closing cash**.
5. Provide a reason if there is a difference.
6. Select **Close session**.

The application preserves closing information for
later review.

## 6. Cash shortages and overages

A shortage occurs when counted cash is below expected cash.

Example:

Expected cash: LKR 13,000
Counted cash: LKR 12,500
Difference: -LKR 500

Do not change the counted cash to hide the shortage.

Record the correct amount and explain the difference.

The backend contains controlled shortage-resolution and
recovery workflows. Management must use authorized
procedures and retain the associated audit records.

## 7. Transfers and reconciliation

The backend contains financial transfer approval and
register cash reconciliation controls.

Superadmin operations can include reviewing cash
allocations, funding sources, reconciliation status,
and carry-forward eligibility.

Carry-forward allocations may be restricted until
required reconciliation has been completed.

Do not treat a pending transfer or an unapproved
reconciliation as completed.

## 8. Session history

The session history includes information such as:

- Session number
- Opening and closing staff
- Opening and closing times
- Sales count and total
- Expected closing cash
- Counted cash
- Difference

Historical records without complete attribution may
display **Not recorded**.

## 9. Management checklist

- Verify staff opening allocations.
- Review cash and card transactions separately.
- Confirm customer collections.
- Review petty-cash movements.
- Investigate shortages and overages.
- Review pending reconciliation work.
- Preserve accurate audit records.
