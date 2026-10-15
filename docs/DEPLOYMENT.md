# Mekmaa POS — Deployment and CI/CD

## Production environment

Public URL:

https://fridge.mekmaa.com/

The GitHub Actions workflow identifies:

| Component | Value |
|---|---|
| Workflow | CI and Deploy Fridge POS |
| Branch | main |
| Production directory | /var/www/fridge.mekmaa.com |
| Systemd service | fridge-pos |
| Local health endpoint | http://127.0.0.1:8886/healthz |
| Public health endpoint | https://fridge.mekmaa.com/healthz |

## Continuous integration

A push to main starts the deployment workflow.

The CI job:

1. Checks out the repository.
2. Installs Go and Node.js.
3. Starts a PostgreSQL test service.
4. Installs frontend dependencies.
5. Builds Tailwind CSS.
6. Runs frontend tests.
7. Initializes the integration-test database.
8. Runs Go tests.
9. Builds the Go application.

Deployment requires the CI job to succeed.

## Production deployment

The deployment job uses an SSH connection configured
through GitHub Actions secrets.

The workflow uses these secret names:

- VPS_SSH_KEY
- VPS_HOST
- VPS_USER

Never include their actual values in documentation.

The production job:

1. Connects to the VPS.
2. Fetches main.
3. Synchronizes the deployment checkout.
4. Builds frontend assets.
5. Builds a new Go binary.
6. Preserves the previous binary.
7. Installs the new binary.
8. Restarts the systemd service.
9. Checks service readiness.
10. Verifies the public health endpoint.

## Rollback behavior

The workflow preserves the previous executable as:

    /var/www/fridge.mekmaa.com/fridge-pos.backup

When the service restart or local health check fails,
the workflow attempts to restore the previous binary.

This is a binary rollback, not a database rollback.

Database migrations and data changes may require
separate recovery planning.

## Production verification

After deployment, confirm:

- GitHub Actions reports success.
- The public health endpoint responds.
- Staff can sign in.
- Product and customer screens load.
- Register-session controls work.
- Relevant business operations behave correctly.

Do not perform real financial transactions solely
to test a deployment.

## Deployment safety

The production workflow uses:

    git reset --hard origin/main

Therefore, do not make uncommitted application changes
directly in the production checkout.

Production credentials and persistent data must be
managed outside the tracked application source.

## Important limitation

A successful HTTP health check verifies application
and database connectivity.

It does not prove that every financial workflow,
backup job, or reconciliation is correct.
