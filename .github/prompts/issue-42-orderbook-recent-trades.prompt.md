# Codex: Implement TradeGrid Issue #42 on Noah-branch

**Repository:** `co-rtex/tradegrid`  
**Issue:** https://github.com/co-rtex/tradegrid/issues/42  
**Target branch:** `Noah-branch` ONLY

## Goal
Build two reusable, fixture-driven React/TypeScript presentation components:
- `frontend/src/components/OrderBook.tsx`: bid/ask price levels, best bid/ask, remaining share quantities.
- `frontend/src/components/RecentTrades.tsx`: completed execution prices, quantities, timestamps.

Do not add live data fetching, API integration, WebSockets, order-entry logic, backend changes, or production `App.tsx` composition.

## 1. Branch and preparation
- Work **only** in the existing `Noah-branch`. Never commit or push to `main`.
- The remote `Noah-branch` was fast-forwarded to `main` after issue #40 was merged (Oct 26, 2026), but verify refs again before beginning.
- Run `git status`, `git fetch origin`, and `git switch Noah-branch`. If the local branch is behind its remote and the working tree is clean, use `git pull --ff-only origin Noah-branch`. Do not discard local changes, force-push, or reset branches.
- Inspect `docs/api.md`, `docs/visual-system.md`, `docs/fixtures/*.json`, `frontend/src/lib/money.ts`, `frontend/src/components/TopNavigation.tsx`, `frontend/src/components/TopNavigation.test.tsx`, `frontend/package.json`, and `frontend/vitest.config.ts`.
- Preserve existing code and project conventions. If a dependency is missing, explain rather than rewriting unrelated features.

## 2. Shared formatting from completed issue #40
`frontend/src/lib/money.ts` is already on `main` and `Noah-branch`. Reuse these exported helpers instead of creating alternatives:
- `formatUsdCents(cents)` (e.g. 19050 -> $190.50)
- `formatWholeShares(quantity)` (e.g. 15 -> 15 shares)
- `formatUtcTime(timestamp)` or `formatDisplayTime(timestamp, timeZone)`

Import the helpers from `../lib/money`. API monetary values remain integer cents; price formatting happens only at the presentation boundary. Do not duplicate issue #40's conversion/parser logic.

## 3. Backend data contract
Follow `docs/api.md`, not invented structures.

Order-book snapshot:
```ts
type PriceLevel = { price: number; quantity: number; orderCount: number }
type OrderBookSnapshot = {
  symbol: string
  asOfSequence: number
  bids: PriceLevel[]
  asks: PriceLevel[]
}
```
- Prices are integer cents; quantities are remaining whole shares aggregated by price.
- Supplied bids already appear best-first (highest price first); supplied asks best-first (lowest price first).
- Display levels in **supplied order**, without sorting/mutating the props. First bid and first ask, when present, are best bid/ask.
- Empty bids with nonempty asks, or vice versa, is a valid one-sided book, not an overall error.
- There is no order-level `remaining` field in aggregated PriceLevel: `quantity` is the contracted remaining quantity at that level.

Execution and list:
```ts
type Execution = {
  id: string
  sequence: number
  executedAt: string
  symbol: string
  price: number
  quantity: number
  buyOrderId: string
  sellOrderId: string
  buyerUserId: string
  sellerUserId: string
}
type ExecutionsResponse = { executions: Execution[] }
```
- Executions arrive newest-first by descending server-generated `sequence`; do not sort by timestamp or change order.
- Execution DTO does **not** contain a standalone BUY/SELL side field. Do not invent a side, color trades as buys/sells, or infer the aggressor from buyer/seller IDs.
- The timestamp is an RFC 3339 UTC string (e.g. `2026-10-20T15:00:02Z`). Display timezone explicitly using shared helpers.

## 4. Component APIs and files
Create:
- `frontend/src/components/OrderBook.tsx`
- `frontend/src/components/OrderBook.css`
- `frontend/src/components/OrderBook.test.tsx`
- `frontend/src/components/RecentTrades.tsx`
- `frontend/src/components/RecentTrades.css`
- `frontend/src/components/RecentTrades.test.tsx`

Expose clear typed props, for example `data?: OrderBookSnapshot | null`, `loading?: boolean`, `error?: string | null` and analogous recent-trades props, with predictable state precedence. Components must be pure/presentational (no fetch, effects, intervals, or network calls). Keep data mocks in tests or dev preview only, never embedded in production components.

## 5. Design and accessibility
Use the **TradeGrid dark, compact market-data panel** design as the default:
- OrderBook: clearly labeled BID / BUY and ASK / SELL sections, side-by-side on wider screens, stacked on narrow screens. Columns: Price (USD), Remaining (shares), optionally Orders. Best bid/ask summaries only when present.
- RecentTrades: compact execution table with Time (UTC), Price (USD), Quantity (shares); include symbol if useful.
- Responsive narrow layouts: no tiny/unreadable fonts, no content clipping; allow meaningful horizontal scroll within table wrapper where needed.
- Semantic `section` and `h2` headings, real `table` elements with accessible captions/labels and scoped column headers. Prefer explicit BUY and SELL text over color alone.
- Follow `docs/visual-system.md`: dark `#111827` panels, `#334155` dividers, readable text, cyan `#22D3EE` interactive accent, green `#22C55E` for bid/buy semantics, red `#EF4444` for ask/sell semantics. Use restrained spacing, tabular digits, and scoped CSS classes.
- Show deliberate variants: loading, error, both-sides-empty, one-sided book, populated, and partial quantities. Distinguish an error from an ordinary empty result.
- Do not decorate a completed execution as BUY/SELL unless actual supported contextual data is added by a future API change.

## 6. Fixture-driven tests
Use Vitest/React Testing Library already configured by issue #45, with assertions for user-visible behavior rather than snapshots.
Use actual JSON scenarios in `docs/fixtures` and add small test-only crafted data for bid+ask multi-level ordering, one-sided books, etc. Existing scenarios include:
- `00-seed.json`: empty bid/ask arrays and no executions.
- `01-no-fill.json`: resting ASK 19000 cents, 10 shares.
- `02-full-fill.json`: empty book, one 10-share execution at 19000 cents.
- `03-partial-fill.json`: resting BID 19050 cents, 15 shares; 10-share execution at 19000 cents.
- `04-risk-rejection.json`: no changed book/executions.

Test:
- Exact prices and quantities (`$190.50`, `15 shares`, etc.).
- Row counts and supplied order, including multiple levels/executions.
- Best bid/ask displayed only when appropriate.
- Empty versus one-sided and partial-quantity displays.
- Loading and error states.
- Accessible headings, column labels, and table accessible names.
- Time formatting via the shared helper with explicit timezone.
- Lack of speculative execution BUY/SELL badges.
- Components render without a backend.

## 7. Preview and screenshots
Provide a small **development/test-only** fixture preview that does not modify production `App.tsx` or add production application composition. Avoid a new third-party library. If a real browser/screenshot utility is available, save genuine populated/empty screenshots for OrderBook and RecentTrades and document their paths. If unavailable, document exact steps to preview and capture screenshots; do not fabricate screenshots.

## 8. Validation and delivery
From `frontend/`, run:
```bash
npm test
npm run lint
npm run typecheck
npm run build
```
Correct any failures caused by the implementation. Review the diff for unrelated changes. If everything passes and the worktree is clean apart from intended changes, commit your issue #42 changes to **Noah-branch only** with message `feat(frontend): add fixture-based market views (#42)` and push that branch without force. Do not open/merge a PR unless asked.

Report:
- Components, designs, and file paths.
- Which issue #40 helpers were reused.
- Tests and commands with pass/fail outcomes.
- Preview and genuine screenshot paths (or manual instructions).
- Branch/commit confirmation and whether any tasks remain.
