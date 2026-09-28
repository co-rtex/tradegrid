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

## Planned Architecture

```text
Trading UI
    |
REST API / WebSockets
    |
Go Backend
    |
+-------------------------------+
| Orders | Risk | Matching      |
| Executions | Portfolio        |
+-------------------------------+
    |
PostgreSQL
```

We are intentionally starting with a modular, understandable architecture. Advanced infrastructure can be added only after the core product works.

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

**Fall 2026: Workshop launch / Sprint 1**

TradeGrid is intentionally early. The project will be built collaboratively through the semester.
