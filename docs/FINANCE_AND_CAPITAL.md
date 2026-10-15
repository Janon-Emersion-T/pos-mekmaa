# Mekmaa POS — Finance and Capital

## Scope

This guide documents financial structures and backend
capabilities verified in the source code.

Not every backend capability has a confirmed browser
interface.

Financial operations should only be performed through
authorized, tested workflows.

## 1. Financial accounts

The database supports these account types:

- staff_cash
- superadmin_cash
- central_cash
- bank
- investment_cash

Staff and superadmin cash accounts are associated with
individual users.

Central cash, bank, and investment cash accounts are
business-owned accounts without an individual user owner.

## 2. Capital entries

The capital API is restricted to superadmins.

Supported entry types include:

### Contribution

Records new invested capital.

A funding account is required.

### Withdrawal

Records a return of invested capital.

The backend rejects withdrawals exceeding remaining
invested capital in the current currency.

A funding account is required.

### Reinvestment

Records capital reinvestment.

The implementation does not permit a funding account
to be supplied for this entry type.

### Correction

A separate restricted endpoint records capital
corrections.

The database preserves a link to the original entry.

A uniqueness constraint permits only one correction
record per original capital entry.

## 3. Capital versus profit

Invested capital and business profit are different.

A capital withdrawal is not automatically a profit
distribution.

Reinvestment should not be interpreted as new cash
received from an outside investor.

## 4. Profit drawings

The database includes profit drawing records with
these possible statuses:

- pending
- approved
- rejected
- cancelled
- paid

The migration explicitly separates profit drawings
from returns of invested capital.

It also states that cash movement must not occur
until profit eligibility is verified.

A complete user-facing profit-drawing workflow was
not established by the current inspection.

Do not assume that a database status alone authorizes
a cash payment.

## 5. Purchase payments

Purchases and purchase payments are recorded separately.

Existing purchases are not automatically marked as
paid when payment accounting is introduced.

The database derives these statuses:

- unpaid
- partially_paid
- paid

A purchase payment includes:

- Purchase reference
- Financial account
- Amount
- Currency
- Funding classification
- Request identifier
- Reference and notes
- Responsible user and timestamp

Supported funding classifications include:

- superadmin_capital
- reinvested_revenue
- other

Funding classification does not create additional
invested capital.

## 6. Purchase funding versus stock receipt

Receiving stock and paying the supplier are separate
financial events.

Do not assume that stock receipt proves supplier
payment has been completed.

Reconcile supplier invoices against recorded payments.

## 7. Cash shortages

The backend contains services for:

- Recording shortages
- Reading shortage balances
- Resolving shortages
- Recovering shortages

The current inspection does not establish every
user-facing step for these services.

Do not manually change balances to conceal shortages.

## 8. Financial transfers

The backend includes transfer request and review
services.

A pending transfer must not be treated as an
approved or completed movement.

Financial transfers should preserve the relevant
source and destination accounting records.

## 9. Financial audit

Capital entries and purchase payments have database
audit triggers.

The responsible user is recorded for sensitive
financial transactions.

Corrections should preserve historical traceability.

## 10. Reconciliation checklist

Before relying on financial reports:

1. Reconcile opening cash and investment balances.
2. Verify supplier invoices and purchase payments.
3. Confirm customer opening debts.
4. Review cash collections and refunds.
5. Reconcile register sessions.
6. Review capital entries and corrections.
7. Verify inventory costs.
8. Investigate unresolved cash shortages.
