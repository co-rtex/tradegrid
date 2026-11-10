# TradeGrid Frontend

A React + TypeScript application built with [Vite](https://vite.dev/guide/).
The Dashboard is a trading desk for the two simulated demo accounts: choose an
account and a symbol, place limit orders, and read balances, orders, the order
book, and recent trades from the Go backend's REST API
([contract](../docs/api.md)). See the repository's
[Developer Setup and Demo Guide](../docs/demo.md) for the backend and the
fixture-defined trading scenarios.

The accounts are simulated, there is no sign-in, and everything resets when the
backend restarts. The desk says so on screen.

## Prerequisites

Install **Node.js 22.12+ (22.x) or Node.js 24+** and npm, which comes with Node.js.
Use an LTS version of Node.js. Check your installation in a terminal:

```bash
node --version
npm --version
```

## Run locally

From the root of your cloned TradeGrid repository, enter the frontend directory:

```bash
cd frontend
```

Install the packages listed in `package.json`:

```bash
npm install
```

Start the backend in another terminal (from the repository root, see the
[demo guide](../docs/demo.md)):

```bash
cd backend
go run ./cmd/server
```

Then start the development server:

```bash
npm run dev
```

Open the local URL printed in the terminal (usually `http://localhost:5173`).
Keep the command running while developing; saving source files updates the page.
Press **Ctrl+C** in the terminal to stop the server.

### Backend proxy and port

The browser only talks to the Vite server. Vite forwards every request under
`/api` (the app uses `/api/v1/...`) to the backend at
`http://127.0.0.1:8080`, the backend's default address, so no CORS setup or
environment file is needed. `npm run preview` uses the same proxy.

If you run the backend on another port, set the same `TRADEGRID_HTTP_PORT`
variable for both commands:

```bash
TRADEGRID_HTTP_PORT=9090 go run ./cmd/server   # in backend/
TRADEGRID_HTTP_PORT=9090 npm run dev           # in frontend/
```

When the backend is not running, the proxy answers with an empty
`502 Bad Gateway` and the desk shows that the server is not responding; start the backend and press
**Refresh**. The proxy is a development convenience only: a production
deployment must serve `/api/v1` from the same origin as the built files.

### How the desk talks to the server

- `src/api/client.ts` is a small typed `fetch` client for the endpoints in
  `docs/api.md`. Errors carry the contracted `code`, `field`, and `message`, and
  say whether the request was refused by the server, never reached it, timed
  out, or got an unreadable response.
- An order is submitted with exactly one `POST`; it is never retried. If no
  readable answer comes back, the desk reports **"Order outcome unknown —
  refresh to check"** rather than success or rejection, because the order may
  have been accepted.
- After an accepted order, and whenever you press **Refresh**, the desk reads
  the accounts, the selected account's orders, and the selected symbol's order
  book and trades again. There is no polling.
- **Live updates** (`docs/realtime.md`): the desk opens one WebSocket to
  `/api/v1/stream` through the Vite proxy (`ws: true`). Each `exchange.changed`
  notice is only a signal: the desk re-reads, through the same REST calls, the
  snapshots it affects for the current selection (the symbol's book and trades
  when the symbol matches, the account's balances and orders when the account is
  in `affectedUserIds`), with bursts coalesced. `hello`, a reconnect, or a
  missed sequence re-reads everything; a server restart clears every held value
  first. Another window's trades therefore appear without pressing Refresh. The
  **Live updates** indicator shows the stream state only: it says nothing about
  whether an order succeeded. If the stream stops, **Refresh** still works and
  **Reconnect live updates** restarts it. A reconnect never resends an order.
- The demo account is chosen once for the whole app (`src/hooks/useDemoAccounts.ts`):
  the app loads the accounts from `GET /api/v1/accounts`, selects the first one
  until you choose, and every view that shows account data renders the same
  **Demo account** selector bound to that choice. Each browser tab remembers its
  own choice (sessionStorage), so one window can stay on Alice and another on
  Bob across reloads. Choosing an account is not a sign-in.
- Only the active view is mounted. Leaving the Dashboard and coming back
  reloads the desk from the server instead of showing old values.
- Switching account or symbol cancels the previous reads, and a response that
  arrives late is ignored, so one account's data never appears under another.
- All amounts shown are the server's integer cents and shares, only formatted
  for display by `src/lib/money.ts`.

### The portfolio view

**Portfolio** shows the selected demo account's `GET /api/v1/portfolio`
([contract](../docs/portfolio.md)): cash, positions, cost basis, market value at
the fixed **simulated** reference prices, realized and unrealized P&L, and
equity. Every amount is calculated by the server; the page only formats it. The
one exception is the card's combined P&L, the exact integer sum of the server's
realized and unrealized totals (the contract has no combined field). Switching
account clears the view before loading, a late response for the previous
account is dropped, and the client rejects a response for any account other
than the one requested. **Refresh** reloads it.

The portfolio also updates live (#96): it opens its own stream connection (the
desk's closes when you leave the Dashboard, since only one view is mounted) and
re-reads `GET /api/v1/portfolio` only when an `exchange.changed` notice lists the
selected account in `affectedUserIds`. `hello`, a reconnect, or a missed
sequence re-read it; a server restart clears the old values first. A trade
between Alice and Bob therefore updates both accounts' portfolios in their own
windows without pressing Refresh. The same **Live updates** indicator,
**Refresh**, and **Reconnect live updates** fallback appear as on the desk.

Run all commands below from `frontend/` as well.

## Checks and production build

```bash
npm run lint
npm run typecheck
npm test
npm run build
```

- `lint` runs Oxlint to check for common code and React mistakes.
- `typecheck` checks the application and Vite configuration with TypeScript.
- `test` runs the component tests once and exits non-zero if any fail. CI runs
  this command. See [Tests](#tests).
- `build` runs the same TypeScript checks, then creates a production bundle in
  `dist/`. Do not commit this generated directory.

To view the production build locally after building:

```bash
npm run preview
```

Open the URL printed in the terminal (usually `http://localhost:4173`). Stop it
with **Ctrl+C**. This is a local preview, not a production hosting service.

## Tests

Component tests use [Vitest](https://vitest.dev/) with
[React Testing Library](https://testing-library.com/docs/react-testing-library/intro/)
in a simulated browser (jsdom). No backend or browser is needed: the API client
and desk tests stub `fetch` or pass a fake client, often built from the JSON
scenarios in `docs/fixtures/`.

```bash
npm test             # run every test once (what CI runs)
npm run test:watch   # rerun affected tests as you save files; press q to quit
npx vitest run src/components/TopNavigation.test.tsx   # run one file
```

Put a test next to the code it covers, named `*.test.tsx` (or `*.test.ts`), as in
`src/components/TopNavigation.test.tsx`. Write tests the way a person uses the
page:

- Find elements by role and accessible name, for example
  `screen.getByRole('button', { name: 'Orders' })`, rather than by CSS class.
- Interact with `userEvent` (clicks, typing, Tab, Enter) and await each action.
- Assert what a user or parent component can observe: visible text,
  `aria-current`, focus, and callback arguments from `vi.fn()`. Avoid snapshots.

`src/test/setup.ts` adds matchers such as `toBeInTheDocument` and
`toHaveAttribute` and clears the page after each test. Test configuration is in
`vitest.config.ts`, separate from `vite.config.ts`, so test tools never reach
the production bundle.

## Directory structure

```text
frontend/
  docs/app-shell.png Screenshot of the initial shell for review
  docs/top-navigation.png Desktop navigation screenshot for issue #10
  src/
    main.tsx         Mounts React into the HTML page
    App.tsx          App shell: navigation, account selection, active view
    pages/
      PortfolioPage.tsx The selected account's server-valued portfolio
    App.test.tsx     App-level navigation, selection, and balance tests
    App.css          App shell styles
    index.css        Global styles and basic browser defaults
    api/
      client.ts      Typed REST client for docs/api.md
      client.test.ts Client tests with a stubbed fetch
      describe.ts    Plain-language text for load failures
    components/
      TradingDesk.tsx Container that loads data and composes the panels below
      DemoAccountSelect.tsx The one demo-account selector, bound to App's choice
      ServerHint.tsx What to do when the backend does not answer
      OrderEntryForm.tsx Limit-order form (controlled by its parent)
      OrderBook.tsx  Aggregated bids and asks for one symbol
      RecentTrades.tsx Executions for one symbol, newest first
      OrderList.tsx  One account's orders and their status
      PortfolioSummary.tsx Portfolio card: equity, cash, and P&L from the server
      TopNavigation.tsx Reusable top navigation and its props
      PositionsTable.tsx, ProfitLossBreakdown.tsx Portfolio display components
      ConnectionStatus.tsx Live-stream connection indicator
      *.css, *.test.tsx Styles and behavioral tests beside each component
    hooks/
      useDemoAccounts.ts App-level demo-account directory and selection
      useSnapshot.ts One REST snapshot with stale-response protection
      useRealtime.ts Real-time stream hook (docs/realtime.md)
    lib/
      money.ts       Integer cents and share formatting and price parsing
      realtime.ts    Stream client: messages, sequences, reconnect
      portfolio.ts   Display-only combined P&L from the server totals
    test/
      setup.ts       Test setup: DOM matchers and cleanup
  index.html         HTML entry point and page title
  package.json       Dependencies and npm commands
  package-lock.json  Locked dependency versions; commit dependency updates
  vite.config.ts     Vite configuration: React plugin and the /api proxy
  vitest.config.ts   Test runner configuration (jsdom, setup file)
  tsconfig*.json     TypeScript settings for the app and build configuration
  .oxlintrc.json     Linter configuration
```

## Adding features

Keep each component's CSS beside it in `src/components/`, and compose
components in `App.tsx` or in a container such as `TradingDesk.tsx`. Use
`index.css` for global styles and `App.css` for the shell layout. New API calls
belong in `src/api/client.ts`, following `docs/api.md`.

### Reusing the navigation

`TopNavigation` is controlled by its parent. Pass `activeItem` (Dashboard,
Portfolio, Orders, or Markets), an `onNavigate` callback, and optionally a formatted
`balance` string. The balance defaults to an em dash until data is available;
the app passes the selected demo account's available cash from the server.

The active button exposes `aria-current="page"`, and the wordmark returns to
Dashboard. Below 768px the navigation wraps to a second row, with horizontal
scrolling if its items exceed the available width. Only Dashboard has content
so far: Orders and Markets show a placeholder, and Portfolio shows the
`PortfolioSummary` card (#23) under a "sample values" label, because its
values are built in rather than read from the server. It is kept off the live
desk so sample numbers are never mistaken for account data.

There is no URL routing or state-management library; navigation is local state
in `App.tsx`.
