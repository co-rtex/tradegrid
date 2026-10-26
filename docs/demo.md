# TradeGrid Developer Setup and Demo Guide

This guide explains what a new contributor can run today and what Sprint 2
behavior is defined by the contract and tested fixtures but is not yet wired
into the running browser or HTTP server.

## What Works Today

- The Go backend starts a local HTTP server with `GET /health`.
- The React frontend starts a local TradeGrid application shell.
- PostgreSQL can run locally for migration-tool development.
- The matching, account, risk, order, and execution packages have tests that
  verify the fixture scenarios in this guide.

The browser TradingDesk and the live REST trading endpoints are not implemented
yet. You cannot submit a trade through the running browser or server at this
time. The full-fill, partial-fill, and risk-rejection sections below are exact
fixture and contract behavior, not a live demo procedure yet.

## Prerequisites

Install the following tools:

- [Go 1.27.0 or newer](https://go.dev/doc/install) for the backend.
- Node.js 22.12+ (22.x) or Node.js 24+ with npm for the frontend.
- [Docker Engine with the Compose plugin](https://docs.docker.com/get-started/get-docker/)
  only when working on the optional PostgreSQL migration foundation.

Check the installed versions:

```bash
go version
node --version
npm --version
docker compose version
```

## Start the Backend

From the repository root:

```bash
cd backend
go run ./cmd/server
```

The backend listens only on `127.0.0.1:8080`. In a second terminal, check the
currently implemented endpoint:

```bash
curl -i http://127.0.0.1:8080/health
```

It returns `200 OK` and `{"status":"ok"}` when the HTTP server is running.
This health check does not check PostgreSQL or a running exchange.

Set `TRADEGRID_HTTP_PORT` before starting the server to choose another local
port:

```bash
TRADEGRID_HTTP_PORT=9090 go run ./cmd/server
```

Press Ctrl-C in the server terminal to stop it.

## Start the Frontend

From the repository root:

```bash
cd frontend
npm install
npm run dev
```

Vite prints the local development URL. With the current configuration it
normally uses `http://localhost:5173`; if that port is already in use, Vite
chooses another port and prints the actual URL. The frontend currently shows a
TradeGrid application shell and navigation only. It does not connect to the
backend, display live account data, or submit orders.

Press Ctrl-C in the Vite terminal to stop it.

## Optional PostgreSQL Migration Foundation

PostgreSQL is separate from the Sprint 2 runtime exchange. Start it from the
repository root only when you are working on database tooling:

```bash
docker compose up -d --wait postgres
backend/db/migrate status
backend/db/migrate up
```

The container listens on `127.0.0.1:5432` by default. Set
`TRADEGRID_DB_PORT` to use a different host port. `backend/db/migrate status`
reports migration state and `backend/db/migrate up` applies pending migrations
once. `backend/db/test-migrate` tests the migration tool with disposable test
databases.

There are currently no application schema migrations or seed files in
`backend/db/migrations/`; the PostgreSQL schema and seed design is documented,
but not implemented. PostgreSQL does not supply, store, or update live account
state for the current backend.

See [Local PostgreSQL and Migrations](../backend/db/README.md) for start, stop,
test-database, and destructive-reset guidance. Do not use
`docker compose down --volumes` unless you deliberately want to discard local
database data.

## State and Reset Behavior

The current executable backend exposes only `/health`, so it has no live
trading state to inspect or preserve. The Sprint 2 exchange contract specifies
an in-memory exchange: when the future exchange server restarts, it begins
again with the two demo accounts below, no orders, and no executions. It has no
runtime persistence or recovery. PostgreSQL is not connected to that runtime.

## Demo Accounts and Symbols

The contract and tested fixtures define two demo accounts. Money values are
integer cents and quantities are whole shares.

| Account | Starting cash | AAPL | MSFT |
| --- | ---: | ---: | ---: |
| `alice` — Alice (demo) | `10000000` cents ($100,000.00) | 100 | 100 |
| `bob` — Bob (demo) | `10000000` cents ($100,000.00) | 100 | 100 |

At the initial state, cash and both positions have zero reserved amounts. The
tradable symbols are `AAPL` and `MSFT`.

## Fixture-Defined Trading Scenarios

The scenarios below come from the shared contract and fixture tests. They are
useful for understanding and testing the implementation, but cannot yet be
replayed through the live HTTP server or browser.

### Full Fill with Price Improvement

1. Alice submits a sell for 10 AAPL at `19000` cents ($190.00).
2. Bob submits a buy for 10 AAPL at `19100` cents ($191.00).

The orders fully fill at the resting sell price of `19000` cents. Bob reserves
`191000` cents when the buy is accepted, pays `190000` cents, and the remaining
`1000` cents returns to available cash.

| Account | Cash total | Cash reserved | Cash available | AAPL total | AAPL reserved |
| --- | ---: | ---: | ---: | ---: | ---: |
| Alice | `10190000` | `0` | `10190000` | `90` | `0` |
| Bob | `9810000` | `0` | `9810000` | `110` | `0` |

Alice and Bob each have a `FILLED` order. The AAPL order book is empty. MSFT
remains 100 shares for each account with no reservation.

### Partial Fill with a Resting Remainder

1. Alice submits a sell for 10 AAPL at `19000` cents ($190.00).
2. Bob submits a buy for 25 AAPL at `19050` cents ($190.50).

Ten shares fill at `19000` cents. Bob's remaining 15 shares rest as a bid at
`19050` cents and retain a `285750`-cent ($2,857.50) reservation.

| Account | Cash total | Cash reserved | Cash available | AAPL total | AAPL reserved |
| --- | ---: | ---: | ---: | ---: | ---: |
| Alice | `10190000` | `0` | `10190000` | `90` | `0` |
| Bob | `9810000` | `285750` | `9524250` | `110` | `0` |

Alice's order is `FILLED`. Bob's order is `PARTIALLY_FILLED` with 15 shares
remaining. The AAPL order book contains one bid: price `19050`, quantity `15`,
and order count `1`. MSFT is unchanged for both accounts.

### Risk Rejection

Bob submits a buy for 600 AAPL at `20000` cents ($200.00). Its value is
`12000000` cents ($120,000.00), which exceeds Bob's starting available cash of
`10000000` cents ($100,000.00).

The expected result is HTTP `422` with code `INSUFFICIENT_CASH`. No order,
reservation, execution, order-book entry, or account balance changes. Both
accounts remain at the starting values above.

## Current and Future Work

The following work is not implemented in the live application yet:

- Live HTTP trading API and integration with the in-memory exchange.
- Browser TradingDesk, demo-account selection, and order submission.
- Manual Refresh; the planned UI must reload authoritative snapshots after an
  order or a user-requested refresh, without polling or WebSockets.
- Runtime persistence and recovery.
- Authentication.
- WebSockets and live UI updates.
- Portfolio P&L.
- Cancellation and amendment APIs.
- PostgreSQL schema migrations and repeatable demo seeds.

## Source Documents

Use these documents as the source of truth instead of copying their full
specifications into this guide:

- [Sprint 2 API and Exchange Contract](api.md) for API shapes, rules, account
  state, reservations, and settlement.
- [Fixture scenarios](fixtures/) for the exact JSON expected before and after
  each scenario.
- [Matching Engine Behavior](matching-engine.md) for price-time priority and
  fill rules.
- [Order Domain Model](order-model.md) for order fields, statuses, and
  lifecycle.
- [Order Input Validation](order-validation.md) for request-validation rules.
- [Trading Glossary](glossary.md) for beginner trading terms.
- [TradeGrid Visual System](visual-system.md) for interface color, typography,
  and financial-number conventions.
- [Local PostgreSQL and Migrations](../backend/db/README.md) and
  [PostgreSQL Schema Design](database-schema.md) for database work.
