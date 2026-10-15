# Mekmaa POS — Security, Backups and Recovery

## 1. Access control

The application supports:

- cashier
- admin
- superadmin

Backend role checks restrict sensitive operations.

Use the minimum access required for each employee.

Disable accounts that are no longer authorized.

## 2. Authentication

Staff authenticate with individual credentials.

Temporary passwords should be changed as required.

Do not share staff accounts or administrator passwords.

Use HTTPS for production access.

## 3. Sensitive configuration

Protect:

- DATABASE_URL
- Bootstrap administrator credentials
- SSH deployment keys
- Production environment files
- Database backup credentials

Never commit secrets to Git.

Restrict filesystem permissions on secret files.

## 4. Financial audit controls

Sensitive financial transactions can retain:

- Responsible user
- Timestamp
- Transaction reference
- Before/after changes
- Correction or reversal relationships

Use supported corrections rather than modifying
database records directly.

## 5. Backup implementation

The repository contains:

    ops/backup.py

The backup script creates a PostgreSQL custom-format
dump and verifies it by restoring to a disposable
database.

Verification checks core tables and sale relationships.

The script does not restore over the live database.

## 6. Backup requirements

The backup environment needs:

- Python 3
- pg_dump
- pg_restore
- psql
- createdb
- dropdb
- Suitable database permissions

Prefer a separate verification database server.

## 7. Backup configuration

The operations documentation describes:

    DATABASE_URL
    VERIFY_DATABASE_URL
    BACKUP_DIR
    BACKUP_RETENTION_DAYS

The example automated configuration uses:

    /etc/counter-backup.env

The environment file must be protected and must not
be committed to the repository.

## 8. Scheduled backups

The repository includes systemd service and timer
definitions.

The documented timer schedule is 02:30 in the host
timezone, with scheduling jitter.

**Important:** The existence of these files does not
prove the timer is installed or active in production.

A production administrator must separately verify:

- Timer installation and activation
- Last successful backup
- Backup verification results
- Available storage
- Off-host backup copies
- Restore-test success

## 9. Retention

The documented default retention is 30 days for
verified backups.

Retention must be confirmed against the actual
production configuration.

Off-host backups are necessary to protect against
complete server loss.

## 10. Disaster recovery

Recovery must be performed by an authorized operator.

The documented procedure is:

1. Stop the application.
2. Create a new recovery database.
3. Restore a verified backup to that database.
4. Validate recovered data and relationships.
5. Point the application at the recovery database.
6. Verify the application.
7. Retain the original database until recovery
   has been accepted.

The restore process should use the PostgreSQL
options documented in `ops/README.md`.

Never restore directly over production without
an approved recovery plan.

## 11. Backup scope

According to the existing operations documentation,
product images, users, audit history and transaction
deduplication records are stored in PostgreSQL and
included in database backups.

## 12. Operational security checklist

- Review administrator access.
- Confirm HTTPS.
- Protect database credentials.
- Monitor deployment failures.
- Verify backup schedules.
- Verify restore capability.
- Review unusual financial changes.
- Investigate cash discrepancies.
- Preserve audit history.
