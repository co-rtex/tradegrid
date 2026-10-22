# Local PostgreSQL and Migrations

This directory holds TradeGrid's PostgreSQL migrations and the small tool that
applies them. It exists for schema and seed work ([#39](https://github.com/co-rtex/tradegrid/issues/39)).
**The running backend does not use PostgreSQL in Sprint 2**: the exchange is
in memory, there is no Go database driver, and `go run ./cmd/server` and
`go test ./...` need no database.

## Prerequisites

[Docker Desktop](https://docs.docker.com/get-started/get-docker/) (or Docker
Engine with the Compose plugin). Check it from a terminal:

```bash
docker compose version
```

No PostgreSQL client is needed on your machine: the tools run `psql` inside
the container.

## Start and stop the database

Run these from the repository root.

```bash
docker compose up -d --wait postgres
```

This starts PostgreSQL 18 (`postgres:18.6-alpine`, pinned in
[`compose.yaml`](../../compose.yaml)) and returns once the health check reports
it ready. Check it at any time:

```bash
docker compose ps postgres
```

The `STATUS` column shows `healthy` when the database accepts connections.

Stop it when you are done. Your data stays in the `tradegrid_postgres-data`
Docker volume and is there next time you start it:

```bash
docker compose stop postgres
```

### Configuration

The defaults work without any setup: user `tradegrid`, database `tradegrid`,
port `5432` on `127.0.0.1` only. To change them, copy the tracked example to an
untracked `.env` file at the repository root and edit it:

```bash
cp .env.example .env
```

Change `TRADEGRID_DB_PORT` if another PostgreSQL already uses port 5432. The
password is for this local container only and is not a secret. Never put real
credentials in `.env.example`. `.env` is ignored by Git.

To connect with your own client, use
`postgres://tradegrid:tradegrid-local-only@127.0.0.1:5432/tradegrid`, or open a
shell inside the container:

```bash
docker compose exec postgres psql -U tradegrid -d tradegrid
```

## Migrations

Migrations are plain SQL files in [`migrations/`](migrations/), named
`NNNN_short_description.sql` with a four-digit version, for example
`0001_create_accounts.sql`. The version determines the order.

```bash
backend/db/migrate status   # applied, pending, MODIFIED, or MISSING
backend/db/migrate up       # apply every pending migration in order
```

How `up` behaves:

- Each migration runs in **its own transaction**, together with the row that
  records it in the `schema_migrations` table. If any statement fails, the
  whole migration rolls back, nothing is recorded, `up` stops and exits
  non-zero, and the migration stays `pending`.
- Applied migrations are never run again. Rerunning `up` is safe.
- Each applied file's SHA-256 checksum is stored. If an applied file is later
  edited or deleted, `status` reports `MODIFIED` or `MISSING` and exits
  non-zero, and `up` refuses to run. **Never edit an applied migration; add a
  new one.**
- A new file must have a higher version than every applied migration. If two
  branches pick the same number, renumber yours after the other merges.
- Concurrent `up` runs are serialized with an advisory lock, so each migration
  is applied once.

Rules for migration files:

- Do not write `BEGIN`, `COMMIT`, or `ROLLBACK`; the tool wraps each file in a
  transaction, and a file that ends it fails. Statements that cannot run in a
  transaction, such as `CREATE INDEX CONCURRENTLY`, are not supported.
- There are no down migrations. Undo a change with a new migration.
- Keep the core schema in [#39](https://github.com/co-rtex/tradegrid/issues/39),
  following the [#12](https://github.com/co-rtex/tradegrid/issues/12) design and
  the resources in [docs/api.md](../../docs/api.md#database-mapping).

## Test databases

Anything that drops or resets data must use a separate database whose name
ends in `_test`. The tool refuses other names, so it cannot drop `tradegrid`.

```bash
backend/db/migrate create-test-db tradegrid_schema_test
TRADEGRID_DB_NAME=tradegrid_schema_test backend/db/migrate up
TRADEGRID_DB_NAME=tradegrid_schema_test backend/db/migrate status
backend/db/migrate drop-test-db tradegrid_schema_test
```

`TRADEGRID_DB_NAME` selects the target database for `up` and `status`.
`TRADEGRID_MIGRATIONS_DIR` points them at another directory of migration files.

The tool's own tests start the container if needed and use the disposable
database `tradegrid_migrate_selftest_test` with temporary migration files.
They do not touch `tradegrid` or `migrations/`:

```bash
backend/db/test-migrate
```

CI runs the same script in the `database` job.

## Resetting your development database

There is no reset command. To discard **all** local data deliberately, remove
the container and its volume:

```bash
docker compose down --volumes
```

This deletes the `tradegrid_postgres-data` volume and cannot be undone.
