# TradeGrid Developer Setup and Demo Runbook

This guide takes a new contributor from a clean checkout to the two-account
Alice/Bob demonstration of the semester MVP: a **local, single-process,
simulated** trading application. It covers prerequisites, startup, ports,
reset behavior, troubleshooting, shutdown, the scripted demo with the values
you should see at each step, a 5–7 minute workshop script, and the fallback if
live updates are unavailable.

Every amount below was checked against the running application by the
browser smoke tests (`frontend/e2e/trading.spec.ts`), which run the same
sequence.

## What the MVP Is, and What It Is Not

| It is | It is not |
| --- | --- |
| Two **simulated demo accounts**, `alice` and `bob`, with $100,000.00 and 100 AAPL + 100 MSFT each | Real money, real securities, or a brokerage |
| A **demo-account selector**: anyone can choose either account | Authentication or authorization; choosing an account is not a sign-in |
| One **in-memory** exchange in the Go process | Persistent: **restarting the backend resets everything** to the seed |
| LIMIT orders with reservations, price-time matching, and settlement | MARKET orders, cancellation, or amendment |
| Portfolio values at **fixed, simulated reference prices** (AAPL $190.00, MSFT $420.00) | Live or delayed market data; trades never move reference prices |
| Live **change notifications** over a WebSocket that make the browser re-read REST data | A market-data feed; the socket carries no balances or prices |
| A separately validated **PostgreSQL schema, migrations, and seeds** | Runtime persistence: the running backend never connects to PostgreSQL |

## Prerequisites

- [Go 1.27.0 or newer](https://go.dev/doc/install) for the backend.
- Node.js 22.12+ (22.x) or Node.js 24+ with npm for the frontend.
- A current Chromium-based browser, Firefox, or Safari for the demo.
- Optional: [Docker Engine with the Compose plugin](https://docs.docker.com/get-started/get-docker/)
  for the PostgreSQL schema validation, and Python 3.10+ for the offline
  analytics report.

```bash
go version
node --version
npm --version
```

## Start the Application

Use two terminals, both starting from the repository root.

**Terminal 1: backend.**

```bash
cd backend
go run ./cmd/server
```

It prints `TradeGrid backend listening on http://127.0.0.1:8080`. The backend
listens on the loopback interface only.

**Terminal 2: frontend.**

```bash
cd frontend
npm ci
npm run dev
```

Open the URL Vite prints, normally `http://localhost:5173`.

### Ports and the development proxy

| Process | Address | Setting |
| --- | --- | --- |
| Go backend | `127.0.0.1:8080` | `TRADEGRID_HTTP_PORT` |
| Vite frontend | `localhost:5173` | Vite picks the next free port if 5173 is taken and prints it |

The browser talks only to Vite. Vite forwards `/api` to the backend, including
the WebSocket upgrade for `/api/v1/stream` (`ws: true` in
`frontend/vite.config.ts`), so the app uses same-origin URLs and needs no CORS
setup. To move the backend, set the same variable for both processes:

```bash
TRADEGRID_HTTP_PORT=9090 go run ./cmd/server   # in backend/
TRADEGRID_HTTP_PORT=9090 npm run dev           # in frontend/
```

### Check the backend without the browser

```bash
curl -i http://127.0.0.1:8080/health
curl -i http://127.0.0.1:8080/api/v1/accounts
curl -i "http://127.0.0.1:8080/api/v1/portfolio?userId=alice"
curl -i http://127.0.0.1:8080/api/v1/stream   # 426 UPGRADE_REQUIRED: it needs a WebSocket client
```

Every route, request, and error is listed in the
[backend README](../backend/README.md) and specified in [api.md](api.md),
[portfolio.md](portfolio.md), and `docs/realtime.md` (the stream contract, in
PR #81).

## Stop, Restart, and Reset

- **Stop**: press Ctrl-C in each terminal. The backend closes every live
  stream connection (code 1001) and exits within five seconds.
- **Reset the demo**: stop and start the backend. Accounts, orders, the book,
  executions, cost basis, and P&L all return to the seed. There is no reset
  endpoint. Open browser windows notice the restart on their own: the stream
  reconnects, sees a lower sequence, clears every value from the old server,
  and reloads.
- The frontend can stay running across backend restarts.

## The Screen

- **Demo account** (top of the Dashboard and Portfolio views) chooses the
  account for the whole app. Each browser window remembers its own choice
  across reloads, so one window can stay on Alice and another on Bob.
- **Dashboard** is the trading desk: balances (cash and shares, each total,
  reserved, and available), the order form, the selected market's order book
  and recent trades, and the account's orders.
- **Portfolio** shows the account's positions, cost basis, market value at the
  reference prices, realized and unrealized P&L, and equity.
- **Live updates** shows the stream state: Connecting, Connected,
  Reconnecting, Disconnected, or Connection error. It describes the
  notification stream only, never whether an order succeeded.
- **Refresh** re-reads everything from the server at any time.
- **Balance** in the navigation bar is the selected account's available cash.

Orders and Markets in the navigation are placeholders; their data is on the
Dashboard.

## Two-Window Demonstration

Open the app in **two separate browser windows** side by side. In window A
choose **Alice (demo)**; in window B choose **Bob (demo)**. Both start with
Market **AAPL**. Wait until both show **Live updates: Connected**.

Starting state, in both windows: cash $100,000.00 available, $0.00 reserved;
AAPL and MSFT 100 shares each, none reserved; the AAPL book shows
"No resting orders."; no recent trades.

### 1. A resting order

In **A (Alice)**: Side **SELL**, Quantity `10`, Limit price `190.00`, then
**Submit limit order**.

- A: "Order ord-1 accepted for Alice (demo): resting on the book with no
  fills, 10 shares remaining." AAPL shows 90 available, 10 reserved,
  100 total. Cash is unchanged: a SELL reserves shares, not cash.
- **B (Bob), without pressing anything**: the AAPL book shows
  "Best ask: $190.00" with 10 shares.

### 2. A full fill with price improvement

In **B (Bob)**: **BUY**, `10`, `191.00`, **Submit limit order**.

- B: "Order ord-2 accepted for Bob (demo): filled, 0 shares remaining. 1 fill:
  10 shares at $190.00." The trade happens at the **resting** price, $190.00,
  not Bob's $191.00 limit: Bob reserved $1,910.00, paid $1,900.00, and the
  $10.00 difference returned. Bob: cash $98,100.00 available, $0.00 reserved;
  AAPL 110.
- **A updates by itself**: cash $101,900.00 available; AAPL 90 total, 0
  reserved; Alice's order shows **Filled**.
- Both windows: the book is empty again and Recent trades shows one AAPL trade,
  10 shares at $190.00.

### 3. A partial fill with a resting remainder

In **A**: **SELL**, `10`, `190.00`. In **B**: **BUY**, `25`, `190.50`.

- B: "... partially filled, 15 shares remaining. 1 fill: 10 shares at
  $190.00." Bob's remaining 15 shares rest as a bid and keep their
  reservation: 15 × $190.50 = **$2,857.50 reserved**. Bob: cash total
  $96,200.00, reserved $2,857.50, available $93,342.50; AAPL 120.
- Both windows: "Best bid: $190.50", one level of 15 shares from 1 order.
- A: cash $103,800.00 available; AAPL 80.

### 4. A risk rejection

In **B**: **BUY**, `600`, `200.00` ($120,000.00, more than Bob's $93,342.50
available).

- B shows "Order rejected by the server: ... (INSUFFICIENT_CASH). No order was
  created." Press **Refresh**: every balance, the book, and Bob's orders are
  exactly as after step 3. Nothing was reserved, stored, or matched.

### 5. Portfolios at fixed reference prices

Click **Portfolio** in both windows.

- Both: equity **$161,000.00** (cash total plus every share at its reference
  price), and $0.00 realized, unrealized, and combined P&L. Every fill so far
  was at $190.00, which is AAPL's reference price, so value only changed form.
- Alice: cash $103,800.00, AAPL 80, MSFT 100. Bob: cash $96,200.00 (with
  $2,857.50 still reserved), AAPL 120, MSFT 100. Reserved cash and shares are
  still owned and still counted once in equity.

### 6. A fill away from the reference price, updating both portfolios live

Keep **both windows on Portfolio**. In a third terminal, Alice sells 5 MSFT at
$430.00 and Bob buys them (MSFT's reference price is $420.00):

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/orders -H 'Content-Type: application/json' \
  -d '{"userId":"alice","symbol":"MSFT","side":"SELL","type":"LIMIT","price":43000,"quantity":5}'
curl -s -X POST http://127.0.0.1:8080/api/v1/orders -H 'Content-Type: application/json' \
  -d '{"userId":"bob","symbol":"MSFT","side":"BUY","type":"LIMIT","price":43000,"quantity":5}'
```

Both portfolios update without Refresh:

| | Alice | Bob |
| --- | --- | --- |
| Cash total | $105,950.00 | $94,050.00 |
| MSFT | 95 shares, basis $39,900.00 | 105 shares, basis $44,150.00 |
| Realized P&L | **+$50.00** ($2,150.00 proceeds − $2,100.00 average cost) | $0.00 |
| Unrealized P&L | $0.00 | **−$50.00** (105 × $420.00 − $44,150.00) |
| Equity | **$161,050.00** | **$160,950.00** |

Combined, the two accounts still have $322,000.00 and zero P&L: trading only
moved value between them.

To do this step from the browser instead: in A go to Dashboard, choose Market
MSFT, and SELL 5 at 430.00; switch A to Portfolio (equity is still
$161,000.00: a resting order changes nothing); then BUY 5 MSFT at 430.00 in B.
A's portfolio updates live; B's shows the new values when you open its
Portfolio.

### 7. Rapid switching

In either window, switch **Demo account** back and forth quickly. The view
always ends on the account selected last and never shows the previous
account's numbers, because each switch clears the view and late responses for
the old account are dropped.

### Screenshots

Captured from the running application after steps 1–3 and 6 (state created
through the real REST API, headless Chromium, 2026-11-09). They show live
server values, not mocks:

- [Bob's trading desk at 1280 px](../frontend/docs/trading-desk-bob.png):
  $2,857.50 reserved for the 15-share remainder, the $190.50 bid, two trades at
  $190.00, and Bob's three orders.
- [Alice's portfolio at 1280 px](../frontend/docs/portfolio-alice.png):
  $161,050.00 equity and +$50.00 realized P&L.
- [Bob's portfolio at 375 px](../frontend/docs/portfolio-bob-phone.png):
  $160,950.00 equity and −$50.00 unrealized P&L in the one-column phone layout.

## 5–7 Minute Workshop Demo Script

| Time | Presenter says | Operator does |
| --- | --- | --- |
| 0:00 | What TradeGrid is: a simulated, local, two-account exchange we built in Go and React. No real money, no sign-in. | Both windows open: A = Alice, B = Bob, Live updates Connected. |
| 0:45 | "Orders are reserved before they can trade." | Step 1. Point at Alice's 10 reserved shares and the ask appearing in Bob's window by itself. |
| 1:45 | "Price-time priority: the resting price wins, and the buyer keeps the improvement." | Step 2. Point at $190.00, not $191.00, and both balances. |
| 2:45 | "Partial fills leave a remainder that keeps its reservation." | Step 3. Point at $2,857.50 reserved and the $190.50 bid in both windows. |
| 3:30 | "Risk checks reject what you cannot afford, and nothing changes." | Step 4, then Refresh. |
| 4:15 | "The portfolio is calculated on the server, at fixed simulated prices." | Step 5. |
| 5:00 | "One trade, two portfolios updated live." | Step 6 (terminal or browser). |
| 6:00 | Architecture in one sentence: React reads REST, Go owns the exchange, WebSocket notices only say what to re-read. Limitations: in memory, no auth, no cancellation, simulated prices. | Optionally restart the backend to show the reset. |

Rehearse on the release candidate and reset (restart the backend) right before
presenting, so the order IDs and values match this guide.

## If Live Updates Are Unavailable

Live updates are a convenience; REST is always authoritative.

- If **Live updates** shows Reconnecting, Disconnected, or Connection error,
  keep going: after each step press **Refresh** in the other window. Every
  value above is the same.
- After Disconnected or Connection error, **Reconnect live updates** restarts
  the stream.
- Nothing about orders depends on the stream: a reconnect never resends an
  order, and the indicator never means an order failed.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| "Could not load the demo accounts" or "The TradeGrid server is not responding" | The backend is not running or is on another port. Start it, or set the same `TRADEGRID_HTTP_PORT` for both processes, then press Retry or Refresh. |
| `listen on 127.0.0.1:8080: address already in use` | Another backend is running. Stop it (Ctrl-C), or choose another port as above. |
| Vite opened on 5174 or another port | 5173 was taken. Use the URL Vite prints; the proxy keeps the page same-origin, so live updates still work. |
| Live updates stays on Connecting or Reconnecting | The backend is down or restarting; it reconnects with backoff (up to 30 s between attempts). Refresh still works once the backend is back. |
| "Order outcome unknown — refresh to check" | The POST got no readable answer (for example the backend stopped mid-request). The order may or may not exist: press Refresh and check the order list before submitting again. It is never retried automatically. |
| Values do not match this guide | The backend has state from earlier trades. Restart it to reset, and refresh both windows. |
| `go: cannot find main module` | Run backend commands from `backend/`. |
| `npm ci` fails | Use Node.js 22.12+ or 24+, and run it from `frontend/`. |

## Automated Checks

From the repository root (all of these also run in CI):

```bash
(cd backend && gofmt -l . && go vet ./... && go test ./... && go test -race ./...)
(cd frontend && npm ci && npm run lint && npm run typecheck && npm test && npm run build)
(cd frontend && npx playwright install chromium && npm run e2e)
python3 -m unittest discover -s analytics -v
```

`npm run e2e` starts its own backend and frontend on ports 18080 and 15173,
so it does not disturb a demo you have running. See
[Release Acceptance](release-acceptance.md) for recorded results and the
human checks that remain.

## Optional: PostgreSQL Schema Validation

The PostgreSQL schema, migrations, and seeds (#39) are a **separately
validated foundation**. The running backend does not connect to PostgreSQL,
and no trade is ever written to it.

```bash
docker compose up -d --wait postgres
backend/db/migrate up
backend/db/seed
backend/db/test-schema
```

`backend/db/test-schema` works in a disposable database: it checks the
constraints, that seeding twice changes nothing, and prints sample queries for
a partial fill, both counterparties, an account's orders, and ordered
execution history. See [Local PostgreSQL and Migrations](../backend/db/README.md).
Stop the database with `docker compose stop postgres`.

## Optional: Offline Trade-Activity Report

```bash
python3 analytics/trade_activity.py docs/fixtures/03-partial-fill.json
```

It summarizes execution records from a JSON file in integer cents, using only
the standard library. See [analytics/README.md](../analytics/README.md).

## Source Documents

- [Sprint 2 API and Exchange Contract](api.md): endpoints, JSON shapes, errors,
  reservations, and settlement; [fixtures](fixtures/) hold the exact JSON.
- [Portfolio Accounting Contract](portfolio.md): cost basis, P&L, reference
  prices, and the portfolio endpoint.
- `docs/realtime.md` (PR #81): the stream's messages, ordering, and reconnect
  rules.
- [Matching Engine Behavior](matching-engine.md), [Order Domain Model](order-model.md),
  and [Order Input Validation](order-validation.md).
- [Architecture](architecture.md), [Trading Glossary](glossary.md), and
  [TradeGrid Visual System](visual-system.md).
- [PostgreSQL Schema Design](database-schema.md).
