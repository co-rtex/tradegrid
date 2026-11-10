# TradeGrid

**TradeGrid** is a student-built, real-time simulated trading platform developed through the UW–Madison FinTech Club technology workshops.

Our goal is to learn how modern financial software is designed by building one shared system together — from order entry and risk checks to matching, executions, portfolios, market data, and a trading interface.

## Semester MVP

By the end of the semester, TradeGrid should allow multiple users to:

- Receive simulated cash
- Submit limit buy and sell orders
- Match compatible orders using price-time priority
- View executions and an order book
- Track positions, cash, and basic P&L
- See market and portfolio updates in near real time

## Architecture

```text
Trading UI (React / TypeScript)
    |
REST API (authoritative)  +  WebSocket change notifications
    |
Go Backend: one in-memory exchange
+-------------------------------+
| Orders | Risk | Matching      |
| Executions | Portfolio        |
+-------------------------------+

PostgreSQL: schema, migrations, and seeds, validated separately
(not used by the running backend yet)
```

We intentionally kept a modular, understandable architecture. Advanced
infrastructure can be added only after the core product works. See
[docs/architecture.md](docs/architecture.md).

## Repository Structure

```text
frontend/       Frontend application
backend/        Go backend and core trading logic
analytics/      Quantitative/analytics experiments
docs/           Architecture, roadmap, and onboarding
contributors/   Contributor profiles
.github/        Issue and pull request templates
```

## New Contributor?

Start with **[docs/onboarding.md](docs/onboarding.md)** and read **[CONTRIBUTING.md](CONTRIBUTING.md)** before opening your first pull request.

No previous fintech or Git/GitHub experience is required.

## How We Work

Every meaningful change follows the same workflow:

```text
Issue -> Branch -> Code -> Pull Request -> Review -> Merge
```

Please do not push directly to `main`.

## Technology Direction

The current target stack is:

- **Backend:** Go
- **Frontend:** React / TypeScript
- **Database:** PostgreSQL
- **Real-time communication:** WebSockets
- **Analytics:** Python

The stack may evolve as the team learns and the product develops.

## Leadership

TradeGrid is organized through the UW–Madison FinTech Club technology team. Maintainers coordinate architecture, reviews, onboarding, and integration; contributors own individual issues and features.

## Status

**Fall 2026: semester MVP integration.** The two-account trading desk,
portfolio, and live updates are implemented on review branches; see
[Release Acceptance](docs/release-acceptance.md) for what is merged, what is
verified automatically, and what still needs a human sign-off before the
November 30 showcase.

What the MVP includes:

- Two simulated demo accounts, Alice and Bob, with $100,000.00 and 100 AAPL +
  100 MSFT each, chosen with a demo-account selector (not a sign-in).
- LIMIT orders with cash and share reservations, price-time matching, partial
  fills, price improvement, and risk rejections, through a REST API.
- A trading desk with balances, the order book, recent trades, and orders.
- A portfolio with average cost basis, realized and unrealized P&L, and
  equity at fixed, simulated reference prices.
- Live updates over a WebSocket that make other windows re-read the server's
  data, with reconnect and a manual Refresh fallback.
- A separately validated PostgreSQL schema with migrations and seeds, and an
  offline Python trade-activity report.

Everything runs locally in one Go process and one Vite dev server, in memory:
restarting the backend resets the demo. There is no authentication, real
market data, order cancellation, or runtime database persistence.

## Developer Setup and Demo

See the [Developer Setup and Demo Runbook](docs/demo.md) to start the backend
and frontend, run the two-window Alice/Bob demonstration with expected values
at every step, and run the automated checks. The
[architecture overview](docs/architecture.md) explains how the pieces fit, and
the [showcase plan](docs/showcase.md) covers the November 30 presentation and
handoff.
