# Mekmaa POS — Troubleshooting

## Application unavailable

Check:

1. The production URL.
2. The deployment workflow status.
3. The application service status.
4. PostgreSQL availability.
5. The health endpoint.

A healthy endpoint does not guarantee every business
workflow is functioning correctly.

## Cannot sign in

Check the staff account status and credentials.

A disabled account requires authorized administrative
review.

Temporary-password accounts may require a password
change before accessing other screens.

## Checkout unavailable

Confirm the logged-in user has opened their own
register session.

Check stock availability and any applicable customer
credit restrictions.

## Insufficient stock

Review product stock and inventory movements.

Use an authorized inventory adjustment or purchase
recording workflow.

Do not modify stock directly in PostgreSQL.

## Customer credit rejected

Review:

- Customer selection
- Existing outstanding balances
- Customer credit policy
- Optimized Debts setting
- Verified inventory costs

When Optimized Debts is enabled, unverified stocked
inventory costs can reduce global available credit
to zero.

## Duplicate transaction concern

If checkout, collection or refund appears to fail
because of a network interruption:

1. Check the transaction history.
2. Determine whether the operation was saved.
3. Use the application's supported recovery behavior.
4. Avoid manually submitting a second transaction.

## Register cash difference

Recount physical cash.

Review cash sales, customer collections and petty cash.

Record the actual counted amount and provide a reason
for discrepancies.

Management should review pending reconciliation items.

## Carry-forward allocation unavailable

Check whether the source register reconciliation
has been verified.

Confirm that sufficient reconciled cash is available.

Do not bypass reconciliation controls.

## Unexpected customer debt

Review:

- Credit sales
- Opening balances
- Collections
- Refunds
- Payment reversals

Use authorized correction workflows.

## Incorrect inventory valuation

Verify recorded purchase costs and stock movements.

Do not enable Optimized Debts until inventory
costs have been reconciled.

## Deployment failure

Review the GitHub Actions run.

Check whether the production job restored the
previous binary.

A binary rollback does not reverse database migrations.

Do not manually repeat a failed production deployment
without understanding the failure.

## Backup failure

Review the backup service logs and script output.

Check PostgreSQL connectivity, permissions,
available disk space and verification database access.

A dump file that has not passed verification should
not be treated as a confirmed recoverable backup.

## Escalation

For unresolved financial issues:

1. Preserve relevant transaction references.
2. Record the affected user and session.
3. Record the approximate time.
4. Avoid duplicate transactions.
5. Escalate to the authorized superadmin.
6. Preserve audit and diagnostic evidence.
