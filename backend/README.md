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
are needed to run it.

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
packages under the current directory. There are no tests yet, so `go test`
currently reports `[no test files]`. Add tests in files ending in `_test.go`
alongside the code they exercise as features are introduced.

## Directory structure

```text
backend/
  go.mod              Module import path and minimum Go version
  cmd/
    server/
      main.go         Executable entry point
  README.md           Backend development instructions
```

Keep `cmd/server/main.go` focused on starting the application. Create packages
under `backend/internal/` as backend functionality is added; Go keeps packages
in that directory private to this backend module. The directory is not needed
until the first package exists.

Issue #5 will add the HTTP health endpoint and server startup. Issue #6 will
define the Order model, which can live in a package under `internal/`.
Exchange features, persistence, and real-time communication are left to their
respective contributor issues.
