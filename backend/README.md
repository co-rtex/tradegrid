# TradeGrid Backend

This directory contains the Go backend. Running it starts a local HTTP server
with a health endpoint, `GET /health`, that reports whether the server is up.
The trading API routes in [docs/api.md](../docs/api.md) are added by later
issues.

## Prerequisites

Install Go 1.27.0 or newer from [go.dev](https://go.dev/doc/install).
Verify your installation in a terminal:

```bash
go version
```

The backend uses only the Go standard library, including `net/http` for the
HTTP server. No database or other services are needed to run it. A local PostgreSQL container and migration workflow for
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

Go compiles and runs the server. Successful startup prints a line like:

```text
2026/10/22 15:00:00 TradeGrid backend listening on http://127.0.0.1:8080 (press Ctrl-C to stop)
```

The server keeps running and does not return you to the prompt. Leave it running
and use a second terminal to send it requests.

The server listens only on `127.0.0.1` (this computer), so other machines on
your network cannot reach it.

### Choose a different port

The default port is `8080`. If another program already uses it, startup fails
with an error such as `bind: address already in use`. Set
`TRADEGRID_HTTP_PORT` to pick another port:

```bash
TRADEGRID_HTTP_PORT=9090 go run ./cmd/server
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `TRADEGRID_HTTP_PORT` | `8080` | Port to listen on, from `1` to `65535`. |

### Check that it is running

In a second terminal:

```bash
curl -i http://127.0.0.1:8080/health
```

A running server answers with status `200 OK` and a JSON body:

```text
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
...

{"status":"ok"}
```

`/health` only confirms that the HTTP server is answering. It does not check
the database or the exchange. It accepts `GET` (and `HEAD`); any other method,
such as `POST`, gets `405 Method Not Allowed` with an `Allow: GET, HEAD`
header. Paths with no route get `404 Not Found`.

### Stop the server

Press `Ctrl-C` in the terminal where the server is running. The server stops
accepting new connections, waits up to five seconds for requests in progress to
finish, prints `TradeGrid backend stopped`, and exits.

## Format and check the code

```bash
go fmt ./...
go vet ./...
go test ./...
```

`go fmt` formats the code, `go vet` checks for common mistakes, and `go test`
builds the packages and runs their tests. The HTTP tests start the server on a
random free port, so they do not need port 8080 to be free, a running server,
or a database. The `./...` pattern includes all
packages under the current directory. Packages without tests report
`[no test files]`. To check for data races too (slower):

```bash
go test -race ./...
```

Add tests in files ending in `_test.go` alongside the code
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
      main.go         Configuration and HTTP server startup and shutdown
  internal/
    httpapi/          HTTP routes and JSON responses
    matching/         Limit-order matching engine
    orders/           Order domain model
    risk/             Pure cash/share reservation and settlement arithmetic
  README.md           Backend development instructions
```

Keep `cmd/server/main.go` focused on starting the application: reading
configuration, opening the listener, and stopping cleanly. Routes belong in
`internal/httpapi`. `NewRouter` in `router.go` registers each route with a
method and path, such as `mux.HandleFunc("GET /health", handleHealth)`, and
each group of handlers lives in its own file. Create packages
under `backend/internal/` as backend functionality is added; Go keeps packages
in that directory private to this backend module.

The Order domain model lives in `internal/orders`; see
[Order Domain Model](../docs/order-model.md) for its fields, enumerations, and
lifecycle. Exchange features, persistence, and real-time communication are left to their
respective contributor issues.
