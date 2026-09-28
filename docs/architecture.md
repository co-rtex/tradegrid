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
Receives, validates, tracks, and cancels orders.

### Risk
Performs pre-trade checks such as valid quantity, price, symbol, and available buying power.

### Matching
Matches compatible buy and sell orders using price-time priority.

### Executions
Represents completed trades produced by the matching engine.

### Portfolio
Tracks cash, positions, cost basis, and basic P&L.

### Market Data
Publishes order-book and execution updates to clients.

### Storage
Persists accounts, orders, executions, positions, and related state.

## Architectural Principle

Start simple and modular. We will not introduce distributed infrastructure merely to make the architecture look more complex. New services should solve a demonstrated problem.
