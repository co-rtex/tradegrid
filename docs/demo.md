# TradeGrid Developer Setup and Demo Guide

This guide explains how to start the full TradeGrid stack locally and run the two-account demo scenarios.

## What Works Today

- The Go backend starts a local HTTP server with `GET /health`, read-only API routes, and `POST /api/v1/orders` for order submission.
- The React frontend includes a live TradingDesk, portfolio view, real-time WebSocket updates, and order entry.
- PostgreSQL is available for migration-tool development, but the live exchange uses in-memory state.

## Prerequisites

- [Go 1.27.0 or newer](https://go.dev/doc/install) for the backend.
- Node.js 22.12+ (22.x) or Node.js 24+ with npm for the frontend.

## Start the Backend

From the repository root:

cd backend
go run ./cmd/server

The backend listens only on 127.0.0.1:8080. In a second terminal, check the currently implemented endpoint:

curl -i http://127.0.0.1:8080/health

It returns 200 OK and {"status":"ok"} when the HTTP server is running. This health check does not check PostgreSQL or a running exchange.

Read the exchange's current state, which starts as the seed state below:

curl -i http://127.0.0.1:8080/api/v1/accounts
curl -i "http://127.0.0.1:8080/api/v1/orderbook?symbol=AAPL"

The read routes and their query parameters are listed in the [backend README](../backend/README.md#read-the-exchange) and specified in the [API contract](api.md).

Set TRADEGRID_HTTP_PORT before starting the server to choose another local port:

TRADEGRID_HTTP_PORT=9090 go run ./cmd/server

Press Ctrl-C in the server terminal to stop it.

## Start the Frontend

From the repository root:

cd frontend
npm install
npm run dev

Vite prints the local development URL. With the current configuration it normally uses http://localhost:5173; if that port is already in use, Vite chooses another port and prints the actual URL.

Press Ctrl-C in the Vite terminal to stop it.

## Two-Account Demo Runbook

1. Open two separate browser windows side-by-side (or use one normal and one incognito window).
2. In Window A, select Alice (demo) from the Demo account dropdown.
3. In Window B, select Bob (demo).
4. Both accounts start with $100,000.00 cash and 100 shares of AAPL and MSFT.

### Scenario 1: Full Fill
1. In Window A (Alice), submit a SELL limit order for 10 AAPL at $190.00.
2. Watch Window B (Bob) update automatically (via WebSocket) to show the Best Ask at $190.00.
3. In Window B (Bob), submit a BUY limit order for 10 AAPL at $191.00.
4. The orders match at Alice's resting price ($190.00).
5. Verify:
   - Alice's cash available increases to $101,900.00.
   - Bob's cash available decreases to $98,100.00.
   - Both see a FILLED order and 1 execution.

### Scenario 2: Partial Fill and Risk Rejection
1. Alice submits a SELL for 10 AAPL at $190.00.
2. Bob submits a BUY for 25 AAPL at $190.50.
3. Verify Bob's order is PARTIALLY_FILLED, 10 shares executed, and 15 shares resting.
4. Bob's Cash reserved should be $2,857.50 (15 × $190.50).
5. Attempt a third order: Bob tries to BUY 600 AAPL at $200.00 ($120,000 total).
6. Verify the UI shows an INSUFFICIENT_CASH error and Bob's balances remain unchanged.

## Optional PostgreSQL Migration Foundation

PostgreSQL is separate from the runtime exchange. Start it from the repository root only when you are working on database tooling:

docker compose up -d --wait postgres
backend/db/migrate status
backend/db/migrate up

The container listens on 127.0.0.1:5432 by default. Set TRADEGRID_DB_PORT to use a different host port. backend/db/migrate status reports migration state and backend/db/migrate up applies pending migrations once. backend/db/test-migrate tests the migration tool with disposable test databases.

There are currently no application schema migrations or seed files in backend/db/migrations/; the PostgreSQL schema and seed design is documented, but not implemented. PostgreSQL does not supply, store, or update live account state for the current backend.

See [Local PostgreSQL and Migrations](../backend/db/README.md) for start, stop, test-database, and destructive-reset guidance. Do not use docker compose down --volumes unless you deliberately want to discard local database data.

## State and Reset Behavior

The running backend holds one in-memory exchange. When the server restarts, the exchange resets to the seed state (Alice and Bob, $100k cash, 100 shares each). PostgreSQL is not connected to that runtime.

## Demo Accounts and Symbols

| Account | Starting cash | AAPL | MSFT |
| --- | ---: | ---: | ---: |
| alice — Alice (demo) | 10000000 cents ($100,000.00) | 100 | 100 |
| bob — Bob (demo) | 10000000 cents ($100,000.00) | 100 | 100 |

At the initial state, cash and both positions have zero reserved amounts. The tradable symbols are AAPL and MSFT.

## Source Documents

- [Sprint 2 API and Exchange Contract](api.md) for API shapes, rules, account state, reservations, and settlement.
- [Fixture scenarios](fixtures/) for the exact JSON expected before and after each scenario.
- [Matching Engine Behavior](matching-engine.md) for price-time priority and fill rules.
- [Order Domain Model](order-model.md) for order fields, statuses, and lifecycle.
- [Order Input Validation](order-validation.md) for request-validation rules.
- [Trading Glossary](glossary.md) for beginner trading terms.
- [TradeGrid Visual System](visual-system.md) for interface color, typography, and financial-number conventions.
- [Local PostgreSQL and Migrations](../backend/db/README.md) and [PostgreSQL Schema Design](database-schema.md) for database work.