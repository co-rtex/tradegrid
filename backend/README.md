# TradeGrid Backend

This directory contains the Go backend foundation. It currently has a small
executable that prints a startup message and exits successfully. It does not
listen for HTTP requests yet.

## Prerequisites

Install Go 1.27.0 or newer from [go.dev](https://go.dev/doc/install).
Verify your installation in a terminal:

```bash
go version
```

The backend uses only the Go standard library. No database or other services
are needed to run it. A local PostgreSQL container and migration workflow for
schema work are described in [db/README.md](db/README.md); the running backend
does not connect to it in Sprint 2.

## Run the backend

From the repository root, enter the backend directory:

```bash
cd backend
```

Run all remaining commands in this README from `backend/`:

```bash
go run ./cmd/server
```

Go compiles and runs the executable. Successful startup prints:

```text
TradeGrid backend started
```

The program then exits and returns you to the terminal prompt. This is expected
for the initial scaffold; there is no running server to stop.

## Format and check the code

```bash
go fmt ./...
go vet ./...
go test ./...
```

`go fmt` formats the code, `go vet` checks for common mistakes, and `go test`
builds the packages and runs their tests. The `./...` pattern includes all
packages under the current directory. Packages without tests report
`[no test files]`. Add tests in files ending in `_test.go` alongside the code
they exercise as features are introduced.

## Directory structure

```text
backend/
  go.mod              Module import path and minimum Go version
  db/
    migrate           Applies and reports versioned SQL migrations
    migrations/       Migration files, NNNN_description.sql
    test-migrate      Self-test for the migration tool
  cmd/
    server/
      main.go         Executable entry point
  internal/
    matching/         Limit-order matching engine
    orders/           Order domain model
  README.md           Backend development instructions
```

Keep `cmd/server/main.go` focused on starting the application. Create packages
under `backend/internal/` as backend functionality is added; Go keeps packages
in that directory private to this backend module.

The Order domain model lives in `internal/orders`; see
[Order Domain Model](../docs/order-model.md) for its fields, enumerations, and
lifecycle. Issue #5 will add the HTTP health endpoint and server startup.
Exchange features, persistence, and real-time communication are left to their
respective contributor issues.
