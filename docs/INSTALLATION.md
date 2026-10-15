# Mekmaa POS — Installation and Development

## Technology

- Go application server
- PostgreSQL database
- JavaScript frontend
- Tailwind CSS
- Node.js and npm for frontend tooling

Application assets are embedded in the compiled Go binary.

## Repository

https://github.com/Janon-Emersion-T/pos-mekmaa

## Prerequisites

Install compatible versions of Go, PostgreSQL,
Node.js, npm and Git.

Check the required Go version in `go.mod`.

The CI pipeline uses Node.js 22 and PostgreSQL 17.

## Local development

Clone the repository:

    git clone https://github.com/Janon-Emersion-T/pos-mekmaa.git
    cd pos-mekmaa

Create a dedicated local PostgreSQL database and user.

Use a strong, unique password. Do not reuse the example
credentials from older documentation.

## Configuration

Supported application environment variables include:

| Variable | Purpose |
|---|---|
| DATABASE_URL | PostgreSQL connection string |
| ADDR | HTTP listening address |
| COOKIE_SECURE | Force secure session cookies |
| TRUST_PROXY | Enable trusted reverse-proxy handling |
| BUSINESS_TIMEZONE | Business timezone |
| BOOTSTRAP_ADMIN_EMAIL | Initial superadmin email |
| BOOTSTRAP_ADMIN_PASSWORD | Initial superadmin password |
| TEST_DATABASE_URL | Disposable integration-test database |

Never commit production credentials.

Use a protected environment file or another approved
secret-management mechanism.

## Install frontend dependencies

    npm ci

## Build CSS

    npm run css

## Run the application

Provide DATABASE_URL through your local environment.

    go run .

The default HTTP port is 8886 unless overridden.

## Build the application

    go build -o fridge-pos .

## Run tests

    npm test
    go test ./...

Some database integration tests require TEST_DATABASE_URL
to point to a disposable PostgreSQL database.

Never use the production database for automated tests.

## Database migrations

Schema changes are stored in numbered SQL files
under `migrations/`.

The application tracks applied migrations in
`schema_migrations`.

Do not manually mark migrations as completed.

Before upgrading production, verify a recoverable
database backup.

## First administrator

The application supports initial superadmin
bootstrap configuration.

Protect bootstrap credentials and follow the
application's password-change requirements.

There is no public staff registration endpoint.

## Health check

    GET /healthz

The endpoint reports readiness only while the
application can reach PostgreSQL.
