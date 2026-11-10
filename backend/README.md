# TradeGrid Backend

This directory contains the Go backend. Running it starts a local HTTP server
with a health endpoint, `GET /health`, that reports whether the server is up.
The trading API contract is in [docs/api.md](../docs/api.md). The read
routes (see [Read the exchange](#read-the-exchange)) and order submission
(see [Submit an order](#submit-an-order)) are implemented. See the repository's
[Developer Setup and Demo Guide](../docs/demo.md) for the current server,
fixture scenarios, and feature status.

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

### Read the exchange

The server holds one in-memory exchange with the two seeded demo accounts.
These read routes return its current state, as specified in
[docs/api.md](../docs/api.md):

```bash
curl -i http://127.0.0.1:8080/api/v1/symbols
curl -i http://127.0.0.1:8080/api/v1/accounts
curl -i "http://127.0.0.1:8080/api/v1/orders?userId=alice"
curl -i "http://127.0.0.1:8080/api/v1/orderbook?symbol=AAPL"
curl -i "http://127.0.0.1:8080/api/v1/executions?symbol=AAPL&userId=bob"
curl -i "http://127.0.0.1:8080/api/v1/portfolio?userId=alice"
```

`/api/v1/portfolio` (`internal/httpapi/portfolio_handlers.go`) values one
account's positions at the fixed simulated reference prices, with cost basis
and realized and unrealized P&L, as specified in
[docs/portfolio.md](../docs/portfolio.md).

The handlers are in `internal/httpapi/read_handlers.go`. Invalid queries get
the contract's JSON error body, for example `400 INVALID_QUERY` for a missing
`symbol` and `404 UNKNOWN_USER` for an unknown `userId`. Under `/api/`, an
unknown path gets `404 NOT_FOUND` and an unsupported method gets
`405 METHOD_NOT_ALLOWED` with an `Allow` header, both as JSON. Restarting the
server resets the exchange to the seeded state.

### Submit an order

`POST /api/v1/orders` submits one `LIMIT` order for a demo account. Send the
six request fields from [docs/api.md](../docs/api.md) as a JSON object with
`Content-Type: application/json`:

```bash
curl -i -X POST http://127.0.0.1:8080/api/v1/orders \
  -H 'Content-Type: application/json' \
  -d '{"userId":"alice","symbol":"AAPL","side":"SELL","type":"LIMIT","price":19000,"quantity":10}'
```

An accepted order is `201 Created` whether it rested, partly filled, or fully
filled. The body holds the order with its server-owned fields (`id`,
`status`, `remaining`, `sequence`, timestamps) and the executions this
submission produced (`[]` when it only rested). Read updated accounts and the
book with the GET routes above.

The handler is in `internal/httpapi/order_handlers.go`. It only decodes the
request; the exchange validates, reserves, matches, and settles the order in
one atomic step, so a rejected request changes nothing. Rejections use the
contract's JSON error body and error order:

| Problem | Status and code |
| --- | --- |
| Missing or non-JSON `Content-Type` | `415 UNSUPPORTED_MEDIA_TYPE` |
| Body over 4096 bytes | `413 BODY_TOO_LARGE` |
| Not exactly one JSON object, a duplicated key, or a wrong JSON type (for example `"price": "19000"`, `190.5`, `1e4`, or `null`) | `400 MALFORMED_JSON` |
| Any other field, including server-owned ones such as `id` or `status`, or a field name in the wrong case | `400 UNKNOWN_FIELD` |
| Unknown `userId` | `404 UNKNOWN_USER` |
| Missing field, bad symbol, side, type, price, or quantity | `422`, codes as in docs/api.md |
| Not enough available cash or shares | `422 INSUFFICIENT_CASH` / `INSUFFICIENT_SHARES` |

The server never retries a submission, and clients must not retry a POST
automatically: a timed-out request may already have been accepted, so check
`GET /api/v1/orders` instead.

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
each group of handlers lives in its own file. Register every `/api/` path with
`handleAPI` instead, for example
`handleAPI(mux, "/api/v1/symbols", methods{http.MethodGet: handleSymbols})`,
so its unsupported methods get the JSON `405` rather than the `/api/`
catch-all's `404`. Create packages
under `backend/internal/` as backend functionality is added; Go keeps packages
in that directory private to this backend module.

The Order domain model lives in `internal/orders`; see
[Order Domain Model](../docs/order-model.md) for its fields, enumerations, and
lifecycle. Exchange features, persistence, and real-time communication are left to their
respective contributor issues.
