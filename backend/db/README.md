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
it ready. The health check connects over TCP, so the temporary socket-only
server that runs during first-time initialization does not count as ready. Check it at any time:

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
backend/db/migrate status   # applied, pending, or a problem (see below)
backend/db/migrate up       # apply every pending migration in order
```

`status` labels each migration `applied`, `pending`, or one of the problems
below, and exits non-zero if there is any problem:

| Label      | Meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| `MODIFIED` | The applied file was edited after it ran.                        |
| `MISSING`  | A recorded migration's file is gone.                             |
| `CONFLICT` | The version was applied under a different file name.             |
| `BLOCKED`  | A pending file is numbered below the latest applied version.     |
| `INVALID`  | A pending file breaks the rules for migration files (see below). |

How `up` behaves:

- Before any SQL runs, `up` checks every pending file. If one is invalid,
  nothing is applied, including earlier valid files.
- Each migration runs in **its own transaction**, together with the row that
  records it in the `schema_migrations` table. If any statement fails, or the
  connection is lost, the whole migration rolls back, nothing is recorded,
  `up` stops and exits non-zero, and the migration stays `pending`.
- `up` sends `COMMIT` only after the migration and its row have succeeded, and
  only while it is still running. If `up` stops first, PostgreSQL rolls the
  transaction back. See [Interrupting `up`](#interrupting-up).
- After each commit, `up` reads the `schema_migrations` row back over a new
  connection and fails unless it names the file with the checksum of the exact
  bytes that ran. When it finishes, it checks that every file is recorded and
  unchanged.
- Applied migrations are never run again. Rerunning `up` is safe.
- Each applied file's SHA-256 checksum is stored. If an applied file is later
  edited or deleted, `status` reports `MODIFIED` or `MISSING` and exits
  non-zero, and `up` refuses to run. **Never edit an applied migration; add a
  new one.**
- A new file must have a higher version than every applied migration. If two
  branches pick the same number, renumber yours after the other merges.
- Concurrent `up` runs are serialized with an advisory lock. Under the lock,
  each run checks again that its version is not already recorded and that no
  newer version has been applied, so each migration is applied once and in
  order. A run that finds its exact file already applied skips it.

How the transaction is protected: the file is not fed to `psql` line by line.
`up` passes its contents to PostgreSQL as one string, run with `EXECUTE`
inside the migration's transaction. PostgreSQL refuses transaction control
there, and `psql` never interprets the file, so a metacommand cannot quit
early, turn off error handling, or run shell commands. The pre-check below
rejects the same things earlier, with a file and line number.

Rules for migration files:

- Do not write `BEGIN`, `START TRANSACTION`, `COMMIT`, `END`, `ROLLBACK`,
  `ABORT`, `SAVEPOINT`, `RELEASE`, or `PREPARE TRANSACTION`. The tool wraps
  each file in one transaction. These words are fine inside function bodies,
  `BEGIN ATOMIC ... END` blocks, strings, and comments.
- Write plain SQL only. `psql` metacommands (`\set`, `\i`, `\copy`, `\gexec`,
  and so on), `psql` variables (`:name`), and `COPY ... FROM STDIN` are not
  available. The pre-check rejects any backslash outside a string or comment.
  Load seed data with `INSERT`.
- Statements that cannot run in a transaction or in a function, such as
  `CREATE INDEX CONCURRENTLY`, `VACUUM`, or `CREATE DATABASE`, fail. A
  procedure or `DO` block that commits also fails. Use `CREATE TABLE ... AS`
  instead of `SELECT ... INTO`.
- A file must contain at least one statement, must not contain NUL bytes, and
  must close every quote and comment.
- There are no down migrations. Undo a change with a new migration.

Limits of the rollback guarantee: PostgreSQL does not roll back sequence
increments (`nextval`), so a failed migration can leave gaps in sequence
values. Work a migration does over another connection, such as through
`dblink`, is not part of its transaction.

### Interrupting `up`

Stopping your terminal's `psql` does not stop PostgreSQL: the server keeps
running a statement it has already received. So when `up` gets Ctrl-C
(`SIGINT`), `SIGTERM`, or `SIGHUP` while a migration is running, it does not
just exit. It:

1. stops sending input to `psql`. If `COMMIT` has not been sent yet, it never
   will be, so the migration cannot be committed after `up` has gone.
2. terminates the migration's server session and waits until it is gone. Its
   transaction has then either committed or rolled back for good.
3. reads `schema_migrations` over a new connection and reports what happened.

It exits with status 130 (143 for `SIGTERM`, 129 for `SIGHUP`) and prints one
of these:

| Message says                                 | Meaning                                                              |
| -------------------------------------------- | -------------------------------------------------------------------- |
| `was not applied: COMMIT was never sent`     | Verified: the session has ended and nothing was recorded.            |
| `was not applied: ... ended without committing` | Verified, although `COMMIT` had been sent.                        |
| `was committed and is recorded as applied`   | The commit finished before the session was stopped.                  |
| `the outcome ... is unknown`                 | `up` could not check, for example because the server was unreachable. |

A migration that was not applied stays `pending`; rerun `up` to apply it.

`up` reports a rollback only when it has verified it. If the outcome is
unknown, run `backend/db/migrate status` (with the same `TRADEGRID_DB_NAME`) to
see whether the migration is `applied` or `pending`. Pressing Ctrl-C a second
time while `up` is checking makes it give up and report the outcome as unknown.

`kill -9` cannot be handled, so `up` reports nothing and leaves its temporary
directory (`tradegrid-migrate.*` under `$TMPDIR`) behind. `COMMIT` is still
never sent after `up` has gone, but a migration whose `COMMIT` was already sent
may have committed. Run `status` to find out.
- Keep the core schema in [#39](https://github.com/co-rtex/tradegrid/issues/39),
  following the [#12](https://github.com/co-rtex/tradegrid/issues/12) design and
  the resources in [docs/api.md](../../docs/api.md#database-mapping).

## Test databases

Anything that drops or resets data must use a separate database whose name
ends in `_test`. The tool refuses other names, so it cannot drop `tradegrid`.

Test database names must start with a lowercase letter, contain only lowercase
letters, digits, and underscores, end in `_test`, and be at most 63 bytes,
because PostgreSQL would silently shorten a longer name and could cut off its
`_test` suffix. `TRADEGRID_DB_NAME` follows the same character rules (it may
start with `_` and need not end in `_test`), and migration file names must be
`NNNN_` plus lowercase letters, digits, and underscores plus `.sql`. Each name
is checked as a whole string before any SQL runs, so spaces, newlines,
punctuation, and quotes are all refused.

```bash
backend/db/migrate create-test-db tradegrid_schema_test
TRADEGRID_DB_NAME=tradegrid_schema_test backend/db/migrate up
TRADEGRID_DB_NAME=tradegrid_schema_test backend/db/migrate status
backend/db/migrate drop-test-db tradegrid_schema_test
```

`TRADEGRID_DB_NAME` selects the target database for `up` and `status`.
`TRADEGRID_MIGRATIONS_DIR` points them at another directory of migration files.

The tool's own tests start the container if needed and use a disposable
database with a unique name (`tg_migself_<pid>_<random>_test`) and temporary
migration files, so several runs can share one server. They do not touch
`tradegrid` or `migrations/`, and they drop their database on exit, including
after Ctrl-C:

```bash
backend/db/test-migrate
```

The tests compare a full `pg_dump` of the database before and after each
failed run, which proves the run left no change behind. They cover SQL errors,
every form of transaction control, `psql` metacommands, a lost connection,
Ctrl-C before and during `COMMIT` and when the outcome cannot be checked,
unreadable and malformed files, a tampered tracking row, and concurrent runs.
They also check, before any SQL runs, that malformed database and file names
are refused, and that `drop-test-db` cannot reach a database
(`tg_migself_<pid>_<random>_victim`) through a crafted name.
A run that is killed outright (`kill -9`) cannot clean up. If that happens,
remove the database it printed with `backend/db/migrate drop-test-db NAME`.

To run the tests and the tool under a particular shell, for example `dash`:

```bash
TRADEGRID_SELFTEST_SHELL=dash dash backend/db/test-migrate
```

CI runs the same script in the `database` job.

## Resetting your development database

There is no reset command. To discard **all** local data deliberately, remove
the container and its volume:

```bash
docker compose down --volumes
```

This deletes the `tradegrid_postgres-data` volume and cannot be undone.
