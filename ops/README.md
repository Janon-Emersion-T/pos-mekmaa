# Backups and recovery

backup.py creates a custom-format PostgreSQL dump, restores it to a uniquely
named disposable database, checks core tables and sale relationships, and only
then marks the backup successful. It never restores over the live database.
Failed backups exit nonzero; old verified backups are retained for 30 days.

Requirements: Python 3, matching PostgreSQL client tools (pg_dump, pg_restore,
psql, createdb, dropdb), and database credentials allowed to read the source.
The verification connection needs permission to create/drop databases. Prefer a
separate verification server using VERIFY_DATABASE_URL.

Test manually with DATABASE_URL loaded securely in the environment:

    BACKUP_DIR=/private/backup/path python3 ops/backup.py
    python3 ops/backup.py --verify /private/backup/path/counter-....dump

For automated Linux operation:
1. Install the app's ops directory at /opt/counter/ops.
2. Create the dedicated OS account counter-backup and give it exclusive write
   access to /var/backups/counter (mode 0700).
3. Create /etc/counter-backup.env, readable only by root (0600), containing
   DATABASE_URL, optionally VERIFY_DATABASE_URL, BACKUP_DIR, and
   BACKUP_RETENTION_DAYS. Do not commit this file.
4. Install the service and timer under /etc/systemd/system, run
   systemctl daemon-reload, then systemctl enable --now counter-backup.timer.
5. Run systemctl start counter-backup.service, check its exit status and journal,
   and monitor service failures with the host's alerting system.

The timer runs at 02:30 in the host's timezone, with up to 15 minutes jitter and
catch-up after downtime. No production timer or credentials are installed by
the repository change. Copy verified dumps to a protected off-host destination
using your infrastructure's backup service; on-host copies alone do not protect
against host loss.

For disaster recovery, stop the application, create a NEW recovery database,
restore with pg_restore --exit-on-error --no-owner --no-acl, validate it, and point
DATABASE_URL at that database. Keep the old database until recovery is accepted.
Product images, audit history, users, and transaction deduplication records are
all included because they live in PostgreSQL.
