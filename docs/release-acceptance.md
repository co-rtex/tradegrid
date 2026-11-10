# Release Acceptance: Semester MVP

This is the release evidence file for [#92](https://github.com/co-rtex/tradegrid/issues/92)
(integrated trading → portfolio → realtime acceptance), with the QA matrix for
[#93](https://github.com/co-rtex/tradegrid/issues/93), the browser evidence
[#46](https://github.com/co-rtex/tradegrid/issues/46) asks for, and the
smoke-test record for [#97](https://github.com/co-rtex/tradegrid/issues/97).
[#95](https://github.com/co-rtex/tradegrid/issues/95) tracks release
coordination and sign-off.

It separates four states that must not be confused:

- **Implemented**: code exists on a branch.
- **PR open**: the code is up for review. Every row below is in this state.
- **Merged**: the code is on `main`.
- **Verified**: a recorded check passed on a named revision.

**Nothing below is a release sign-off.** Acceptance is signed off by tech
leadership only after a complete real-browser run by a person on the final
merged revision (#92). Automated results are recorded here as evidence for that
run, not as a substitute for it.

## Candidate Under Test

| Item | Value |
| --- | --- |
| Integration branch | `final/integration` plus `final/97-e2e-smoke` (smoke tests) |
| Revision tested | `45a8454` (`final/97-e2e-smoke`) |
| Date | 2026-11-09 |
| Machine | macOS (Apple silicon), Go 1.27.0, Node.js 22.22.2, Docker 28.5.2, Python 3.14.2 and 3.10 |
| Contributor PRs included unmerged | #85 (#41 form), #86 (#37 reads), #82 (#72 hub); #81 (#71 contract, docs only) is referenced, not merged into the candidate |

The integration branch merges every feature branch in
[Feature Status](#feature-status). Because #81, #82, #85, and #86 are still
open, **the candidate is not `main`**: the final run must be repeated after
they and the stacked PRs merge.

## Feature Status

| Issue | What | Implemented on | PR | Merged | Automated verification |
| --- | --- | --- | --- | --- | --- |
| #36 | `POST /api/v1/orders` | `final/36-order-submission` (stacked on #86) | #103 | No | httptest suite, race |
| #37 | Read endpoints | contributor PR #86 | #86 | No | its own tests; used by every check below |
| #41 | Limit-order form | contributor PR #85 | #85 | No | its own tests; used by the desk and e2e |
| #44 | Trading desk + app-level account selection | `final/44-trading-desk` (includes #85) | #107 | No | component tests, e2e |
| #46 | Two-account HTTP integration, atomicity, concurrency, CI race | `final/46-trading-integration-tests` | #104 | No | httptest suite, race ×3 |
| #65 | Pure portfolio valuation | `final/65-portfolio-valuation` | #101 | No | unit tests |
| #68 | Cost basis in atomic settlement | `final/68-cost-basis-settlement` | #102 | No | unit, worked examples, invariants, race |
| #69 | `GET /api/v1/portfolio` | `final/69-portfolio-endpoint` (stacked on #86, #65, #68) | #105 | No | httptest suite |
| #70 | Portfolio page | `final/70-portfolio-page` | #108 | No | component tests, e2e |
| #71 | Stream contract | contributor PR #81 | #81 | No | — (docs) |
| #72 | Bounded event hub | contributor PR #82 | #82 | No | its own tests, race |
| #73 | `GET /api/v1/stream` | `final/73-websocket-stream` (stacked on #86, #82) | #106 | No | WebSocket integration tests, race ×10 |
| #77 | Live trading-desk refresh | `final/77-live-trading-desk` | #109 | No | component tests, e2e |
| #96 | Live portfolio refresh | `final/96-live-portfolio` | #110 | No | component tests, e2e |
| #39 | PostgreSQL schema and seeds | `final/39-trading-schema` | #99 | No | `test-schema` on Docker PostgreSQL 18.6 |
| #76 | Offline trade-activity report | `final/76-trade-activity-report` | #100 | No | 50 unittest cases on Python 3.10 and 3.14 |
| #97 | Browser smoke tests | `final/97-e2e-smoke` | #111 | No | 8 Playwright tests, Chromium |

Already merged and used as-is: #35, #42 (PR #91), #43, #62, #63, #64, #66, #67,
#74, #75.

## Automated Results on `45a8454`

| Check | Command | Result |
| --- | --- | --- |
| Go formatting | `cd backend && gofmt -l .` | no files |
| Go vet | `go vet ./...` | clean |
| Go tests | `go test ./...` | 11 packages ok, 191 top-level tests passed, 0 failed |
| Go race detector | `go test -race -count=3 ./...` | 11 packages ok |
| Frontend lint | `cd frontend && npm run lint` | 0 warnings, 0 errors |
| Frontend typecheck | `npm run typecheck` | clean (app, config, and e2e projects) |
| Frontend tests | `npm test` | 16 files, 392 tests passed |
| Frontend build | `npm run build` | built |
| Browser smoke | `npm run e2e` | 8 passed in Chromium (fresh backend and Vite on 18080 / 15173) |
| PostgreSQL | `docker compose up -d --wait postgres && backend/db/migrate up && backend/db/seed && backend/db/seed && backend/db/test-migrate && backend/db/test-schema` | all passed: 5 migrations recorded, 84 invalid statements rejected by the expected constraint, 11 valid boundary cases accepted, reseeding changed nothing |
| Analytics | `python3 -m unittest discover -s analytics -v` | 50 passed (3.14 and 3.10) |

Each feature branch was also checked on its own before integration; see the
test evidence in its PR.

## Acceptance Scenarios (#92)

"Automated" means a recorded check on the revision above exercised the
scenario through the real backend and browser. "Human" is the person-run check
#92 requires before sign-off; it is **not yet done** for any scenario.

| # | Scenario | Automated evidence | Automated | Human |
| --- | --- | --- | --- | --- |
| 1 | Fresh checkout starts with the documented commands; Alice and Bob available | e2e starts `go run ./cmd/server` and Vite from the checkout; both accounts selectable | Pass | Pending: a second contributor follows [demo.md](demo.md) on a clean clone (#94) |
| 2 | Two windows trade LIMIT orders: full fill with price improvement, partial fill with resting remainder, orders, executions, cash/shares, reservations | `e2e/trading.spec.ts` tests 1–2 assert every balance, reservation, book level, order status, and trade on screen in two browser contexts | Pass | Pending |
| 3 | Invalid or risk-rejected orders change nothing; unclear outcomes are not shown as success; POST never retried | e2e test 2 (INSUFFICIENT_CASH, then Refresh shows identical state); Go httptest rejection and injected-failure tests (#36, #46); desk tests for "outcome unknown" and no retry, including across stream reconnects | Pass | Pending |
| 4 | Portfolio shows server-calculated positions, reference values, cost basis, realized/unrealized P&L, equity, and a trade changes them | e2e test 3: +$50.00 realized for Alice, −$50.00 unrealized for Bob, equities $161,050.00 / $160,950.00; Go tests replay docs/portfolio.md examples 1–5 | Pass | Pending |
| 5 | A committed trade makes the other window's desk and both portfolios re-fetch without Refresh; manual Refresh remains; reconnect and dropped-event recovery | e2e tests 1–3 (live desk and live portfolios in both windows); component tests for gaps, reconnect, server restart; scripted real-browser restart check below | Pass | Pending |
| 6 | Rapid account/symbol switching never shows stale data | e2e test 4; component tests with deferred late responses for account and symbol switches | Pass | Pending |
| 7 | PostgreSQL validated independently and not presented as runtime persistence | `test-schema` result above; docs state the backend never uses it | Pass | Pending |
| 8 | Backend tests with race, full frontend checks, recorded commands | table above | Pass | Pending: rerun on the merged `main` revision |
| 9 | Results, screenshots or recording, bugs, pass/fail per scenario recorded here | this file | Partial | Pending: screenshots or a screen recording from the human run |

### Scripted real-browser restart check

Run against the integrated backend binary and Vite in headless Chromium
(Playwright library), 2026-11-09:

1. Bob rests BUY 1 AAPL @ $100.00: the desk shows $100.00 reserved.
2. SIGINT to the backend with the stream open: exit code 0 after 3 ms; log
   "shutting down", then "stopped".
3. The desk shows **Live updates: Reconnecting…**.
4. Refresh while the backend is down shows "The TradeGrid server is not
   responding." and no stale balances.
5. Backend restarted: the desk reconnects by itself, detects the restart, and
   shows $0.00 reserved and "No orders yet for Bob (demo)." (the seed).
6. The outage banner is gone.

## Found and Fixed During Verification

| Problem | Where | Fix |
| --- | --- | --- |
| A desk test read balances as soon as the orders heading appeared; balances load independently, so it failed under parallel load | `TradingDesk.test.tsx` (#44) | Tests wait until every panel's first read finishes |
| The stream ping test could fail under load: an expired client read closes the socket and may report "closed connection" instead of a deadline | `stream_handler_test.go` (#73) | A test hook after each answered ping; the test waits for pongs, then checks an event still arrives |
| At 1024–1400 px the desk's two-column grid squeezed the order book and the recent-trades table into internal scroll areas, hiding the Orders and Quantity columns (seen in a screenshot) | `TradingDesk.css` (#44) | Side by side only from 75rem with a 22rem form column; full-width panels below |
| A missed pong made the server wait up to 5 s for a close handshake that an unresponsive peer never answers | `stream_handler.go` (#73) | The connection is dropped at once after a missed pong |

No open defects are known in the tested candidate.

## QA Matrix (#93)

| Browser / device | Viewport | Steps | Expected | Actual | Result |
| --- | --- | --- | --- | --- | --- |
| Chromium 156 (Playwright), macOS | 375 × 812 | Load desk; open Portfolio | No horizontal page scroll; tables scroll in their own focusable region | `scrollWidth − clientWidth ≤ 0` on both views | Pass (automated) |
| Chromium 156 (Playwright), macOS | 768 × 1024 | Same | Same | Same | Pass (automated) |
| Chromium 156 (Playwright), macOS | 1280 × 800 | Same | Same | Same | Pass (automated) |
| Chromium 156 (Playwright), macOS | 1280 × 720 | Tab through the desk | Demo account, Market, Refresh, Symbol, Quantity, Limit price, and Submit are all reachable, each with a visible focus style | All reached; none without a focus outline | Pass (automated) |
| Chromium 156 (Playwright), macOS | 1280 × 720 | Two-window trading and portfolio scenarios | Exact values in [demo.md](demo.md) | Matched | Pass (automated) |
| Chromium 156 (Playwright), macOS | 375 × 812 | Look at the top navigation | All items visible | "Markets" is cut off at the right edge; the bar scrolls horizontally (existing #10 component). Minor, not release-blocking | Finding (minor) |
| Firefox | any | Full runbook | Same as Chromium | Not run: Firefox for Playwright is not installed on the test machine | **Not covered** |
| Safari / WebKit | any | Full runbook | Same as Chromium | Not run: WebKit for Playwright is not installed; desktop Safari not checked | **Not covered** |
| Physical phone | 375-class | Full runbook | Usable one-column layout | Not run | **Not covered** |
| VoiceOver or NVDA | — | Desk and portfolio; submit an order; stream state change | Labels, table headers, and status and alert announcements are read; state changes are announced once | Not run | **Not covered** |

Accessibility properties covered by component tests (jsdom, not a screen
reader): labeled controls and table headers; `role="status"` and
`role="alert"` for loading, results, and errors; BUY/SELL, P&L direction, and
connection state stated in words, not only by color; focus returns to the
first invalid field or the submit button after a submission; account and
market selectors disabled while an order is pending.

To add Firefox and WebKit to the smoke tests, install them with
`npx playwright install firefox webkit` (about 200 MB of downloads) and add
projects to `frontend/playwright.config.ts`.

## Remaining Human Actions Before Sign-off

1. Review and merge the contributor PRs #86, #85, #82, and #81 (their owners
   resolve review comments and conflicts), then the stacked PRs in the order
   in [showcase.md](showcase.md#merge-order).
2. Rerun every command in [Automated Results](#automated-results-on-45a8454)
   on the merged `main` revision and update this file with that SHA.
3. A person runs the [two-window demonstration](demo.md#two-window-demonstration)
   on `main` in two real browser windows, records pass/fail per scenario
   above, and attaches screenshots or a recording.
4. A second contributor follows [demo.md](demo.md) from a clean clone (#94).
5. Cross-browser and screen-reader checks from the QA matrix, or an explicit
   decision to accept them as not covered (#93).
6. Tech leadership signs off (#92, #95).
