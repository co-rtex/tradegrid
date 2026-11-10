# TradeGrid Architecture

## Product Flow

```text
User
 |
Trading UI
 |
REST API / WebSockets
 |
Order Validation
 |
Pre-Trade Risk
 |
Matching Engine
 |
Execution
 |
Portfolio Update
 |
PostgreSQL
 |
Live UI Update
```

## Core Modules

### Orders
Receives, validates, and tracks LIMIT orders. Cancellation is future work.

See [Order Input Validation](order-validation.md) for the initial validation rules for the LIMIT-order MVP.
See [Order Domain Model](order-model.md) for the order fields, the closed Side/Type/Status enumerations, and the lifecycle transitions.

### Risk
Performs pre-trade checks such as valid quantity, price, symbol, and available buying power.

### Matching
Matches compatible buy and sell orders using price-time priority.

See [Matching Engine Behavior](matching-engine.md) for the limit-order rules, worked examples, and matching invariants.

### Executions
Represents completed trades produced by the matching engine.

### Portfolio
Tracks cash, positions, cost basis, and basic P&L.

See [Portfolio Accounting Contract](portfolio.md) for average-cost basis, realized and unrealized P&L, reference prices, and the portfolio endpoint.

### Market Data and Real-time Notifications
Order-book and execution snapshots are REST reads. The real-time stream
(`docs/realtime.md`, PR #81) publishes small change notifications after each
committed submission so clients know what to re-read; it carries no prices or
balances. Reference prices are fixed, simulated constants.

### Storage
The running exchange is in memory. The PostgreSQL schema for accounts, orders,
executions, and positions is implemented as migrations and seeds and validated
separately; runtime persistence is future work.

See [PostgreSQL Schema Design](database-schema.md) for the tables, keys, constraints, and indexes.

## Sprint 2 Contract

See [Sprint 2 API and Exchange Contract](api.md) for the REST endpoints, JSON shapes, error codes, numeric bounds, demo accounts, reservation and settlement rules, and the shared [fixtures](fixtures/).

## Semester MVP as Built

A local, single-process, simulated two-account application:

```text
 Browser window (React + TypeScript, Vite dev server)
   App: one demo-account selection; one view mounted at a time
   ├── Dashboard: TradingDesk ─ order form, balances, book, trades, orders
   └── Portfolio: PortfolioPage ─ positions, cost basis, P&L, equity
        │  REST (fetch, typed client)          │  WebSocket notices
        │  GET snapshots, POST /api/v1/orders  │  GET /api/v1/stream
        ▼                                       ▼
 Vite proxy: /api → 127.0.0.1:8080 (ws: true)
        │
 Go backend (net/http, one process)
   httpapi: routes, strict JSON decoding, contracted errors, stream
        │
   exchange.Service  ─ one mutex: the serialized, atomic boundary
     orders · risk (reservations) · matching (price-time) · executions
     accounts ledger (cash, shares, cost-basis lots)
        │ after each commit, non-blocking
   realtime.Hub ─ bounded per-connection queues → stream handler
        │
   portfolio.Value ─ pure valuation at fixed reference prices (read path)

 PostgreSQL (separate): schema, migrations, and seeds validated by scripts;
 never used by the running backend.
```

- **One write path.** `POST /api/v1/orders` decodes only the six client
  fields. The exchange validates, reserves, matches, settles cash, shares, and
  cost basis on a candidate copy, and publishes everything together or
  nothing. Rejections change nothing and are never retried.
- **Authoritative reads.** Every `GET` returns a copied snapshot taken under
  the same lock, so it reflects one point between submissions. The browser
  only formats integer cents and shares; it computes no balances, cost basis,
  or P&L (the portfolio card's combined P&L is the exact sum of the two server
  totals).
- **Notifications, not data.** After each commit the exchange hands one
  `exchange.changed` event to the hub without blocking. The stream sends it to
  every connection with the commit's sequence; clients re-read the REST
  snapshots it affects, re-read everything after a reconnect or a gap, and
  drop everything after a server restart. A slow client is disconnected
  (1013) rather than delaying trading.
- **Stale-response protection.** Every view loads through `useSnapshot`, which
  aborts the previous load and applies only the latest, so a late response for
  a previous account or symbol is never shown.
- **State lifetime.** Everything is in memory and resets when the backend
  restarts. There is no authentication: the demo account is a selector.

The [Developer Setup and Demo Runbook](demo.md) starts the system and walks
through the two-window demonstration. [Release Acceptance](release-acceptance.md)
records the verification evidence and what still needs a human.

## Architectural Principle

Start simple and modular. We will not introduce distributed infrastructure merely to make the architecture look more complex. New services should solve a demonstrated problem.
