#!/usr/bin/env python3
"""Create PostgreSQL backups and verify recovery in a disposable database."""
import argparse
import datetime as dt
import os
from pathlib import Path
import secrets
import subprocess
from urllib.parse import urlsplit, parse_qs, unquote

def connection(variable):
    value = os.environ.get(variable)
    if not value:
        raise SystemExit(f"Set {variable} in the private backup environment file")
    url = urlsplit(value)
    if url.scheme not in ("postgres", "postgresql"):
        raise SystemExit(f"{variable} must be a PostgreSQL connection URL")
    env = os.environ.copy()
    env.update(PGHOST=url.hostname or "localhost", PGPORT=str(url.port or 5432),
               PGUSER=unquote(url.username or ""), PGPASSWORD=unquote(url.password or ""),
               PGDATABASE=unquote(url.path.lstrip("/")))
    if "sslmode" in parse_qs(url.query):
        env["PGSSLMODE"] = parse_qs(url.query)["sslmode"][0]
    return env

def run(command, env, capture=False):
    return subprocess.run(command, env=env, check=True, text=True,
                          stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
                          stderr=subprocess.PIPE)

def verify(archive):
    env = connection("VERIFY_DATABASE_URL" if os.environ.get("VERIFY_DATABASE_URL") else "DATABASE_URL")
    name = "counter_restore_" + secrets.token_hex(8)
    created = False
    try:
        run(["createdb", name], env)
        created = True
        run(["pg_restore", "--exit-on-error", "--no-owner", "--no-acl",
             "--dbname", name, str(archive)], env)
        restored = dict(env, PGDATABASE=name)
        query = """SELECT
          (SELECT count(*) FROM information_schema.tables WHERE table_schema='public'
            AND table_name IN ('products','sales','sale_items','register_sessions','users','schema_migrations'))=6
          AND NOT EXISTS(SELECT 1 FROM sale_items i LEFT JOIN sales s ON s.id=i.sale_id WHERE s.id IS NULL)
          AND NOT EXISTS(SELECT 1 FROM sales s LEFT JOIN register_sessions r ON r.id=s.session_id WHERE r.id IS NULL);"""
        result = run(["psql", "-X", "-At", "-v", "ON_ERROR_STOP=1", "-c", query], restored, True)
        if result.stdout.strip() != "t":
            raise RuntimeError("Restored database failed integrity checks")
    finally:
        if created:
            run(["dropdb", name], env)

def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument("--verify", type=Path)
    args = parser.parse_args()
    if args.verify:
        verify(args.verify.resolve())
        print("Recovery verification passed; disposable database removed.")
        return
    retention = int(os.environ.get("BACKUP_RETENTION_DAYS", "30"))
    if retention < 1:
        raise SystemExit("BACKUP_RETENTION_DAYS must be positive")
    directory = Path(os.environ.get("BACKUP_DIR", "/var/backups/counter")).resolve()
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    archive = directory / ("counter-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
                           + "-" + secrets.token_hex(3) + ".dump")
    pending = archive.with_suffix(".partial")
    try:
        run(["pg_dump", "--format=custom", "--file", str(pending)], connection("DATABASE_URL"))
        verify(pending)
        pending.rename(archive)
    finally:
        pending.unlink(missing_ok=True)
    cutoff = dt.datetime.now().timestamp() - retention * 86400
    for old in directory.glob("counter-*.dump"):
        if old != archive and old.is_file() and old.stat().st_mtime < cutoff:
            old.unlink()
    print(f"Backup and recovery verification passed: {archive.name}")

if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as exc:
        raise SystemExit(f"Backup failed during {Path(exc.cmd[0]).name}; exit code {exc.returncode}")
