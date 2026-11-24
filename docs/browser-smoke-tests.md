# Browser End-to-End Smoke Tests

## Purpose

The browser smoke-test suite (#97) verifies the integrated
TradeGrid frontend and Go backend using Playwright. It
provides repeatable regression coverage for trading,
risk rejection, financial balances, and real-time updates.

These tests supplement the HTTP integration tests (#46)
and do not replace manual release acceptance (#92).

## Prerequisites

- Git
- Node.js 22.12+ (22.x) or 24+
- npm
- Go 1.27+
- Playwright Chromium

PostgreSQL is not required because the smoke tests use
the backend's seeded in-memory exchange.

## Installation and Execution

From the repository root:

```bash
cd frontend
npm ci
npx playwright install chromium
npm run e2e
```

On Windows PowerShell, use the following commands if
PowerShell blocks npm scripts:

```powershell
cd frontend
npm.cmd ci
npx.cmd playwright install chromium
npm.cmd run e2e
```

To view the HTML test report:

```bash
npx playwright show-report
```

On Windows, use `npx.cmd` instead of `npx` if necessary.

## Test Environment and Isolation

Playwright automatically starts the real Go backend
and Vite frontend using dedicated ports:

- Backend: 18080
- Frontend: 15173

Existing servers are not reused. Each full test run
starts with fresh seeded exchange state.

Alice and Bob each begin with $100,000 in cash and
100 shares of both AAPL and MSFT.

The trading scenarios execute serially because they
build on the exchange state established by earlier tests.

## Smoke-Test Scenarios

### 1. Full Fill and Price Improvement

Alice submits a SELL LIMIT order for 10 AAPL shares
at $190.00. Bob submits a BUY LIMIT order for 10
shares at $191.00.

The test verifies:

- Execution at the resting price of $190.00.
- Alice's cash increases to $101,900.00.
- Bob's cash decreases to $98,100.00.
- Correct share balances and released reservations.
- Filled orders and execution history.
- Live updates without manual refresh.

### 2. Partial Fill and Risk Rejection

Alice submits another SELL LIMIT order for 10 shares
at $190.00. Bob submits a BUY LIMIT order for 25
shares at $190.50.

The test verifies:

- Ten shares execute at $190.00.
- Fifteen shares remain resting at $190.50.
- Bob retains a $2,857.50 cash reservation.
- Bob has $93,342.50 in available cash.

Bob then attempts an unaffordable $120,000 order.

The rejection must preserve:

- Both accounts' cash balances and reservations.
- Both accounts' AAPL share balances.
- Existing order counts.
- Execution history.
- The existing 15-share resting bid.

Both accounts are refreshed from the server to verify
authoritative state after the rejection.

### 3. Real-Time Portfolio Updates

Both accounts open their portfolios in separate
browser contexts.

A cross-account MSFT trade is submitted through the
REST API while both portfolios remain open.

The test verifies automatic portfolio updates:

- Alice: $50.00 realized gain and $161,050.00 equity.
- Bob: $50.00 unrealized loss and $160,950.00 equity.
- Correct positions and combined profit and loss.

No manual portfolio refresh is required.

## Additional Coverage

The suite also checks:

- Rapid account switching.
- Responsive layouts at 375px, 768px, and 1280px.
- Keyboard navigation and visible focus indicators.

## Reliability and Cleanup

Tests use deterministic financial values and
Playwright's condition-based assertions rather than
arbitrary fixed sleeps.

Separate browser contexts isolate Alice and Bob.

Browser contexts are closed after execution. Contexts
are also explicitly closed if desk initialization fails.

Playwright manages the lifecycle of the backend and
frontend test servers.

On Windows, server cleanup can be checked using:

```powershell
Get-NetTCPConnection -LocalPort 18080,15173 -State Listen -ErrorAction SilentlyContinue
```

No output indicates that neither designated port has
a remaining listening server.

Failure screenshots and traces are retained for
diagnostic purposes.

## Continuous Integration

The GitHub Actions E2E job installs the required
dependencies and Chromium, then runs `npm run e2e`.

The job has a 15-minute timeout and uploads the
Playwright report on failure.

## Independent Verification

Verification date: November 23, 2026.

Baseline integrated revision:

`45a8454b0744d3e6d17ef92e01574f00e3d69b9f`

Environment:

- Windows AMD64
- Node.js 26.13.1
- npm 11.20.0
- Go 1.27.4
- GCC 16.2.0
- Playwright 1.64.1

### Baseline Results

| Check | Result |
| --- | --- |
| Browser smoke tests, first run | 8/8 passed (49.7s) |
| Browser smoke tests, second run | 8/8 passed (14.6s) |
| Frontend unit tests | 392/392 passed |
| Frontend lint | 0 errors, 0 warnings |
| TypeScript typecheck | Passed |
| Production build | Passed |
| Go vet | Passed |
| Go backend tests | 11/11 packages passed |
| Go race tests | 11/11 packages passed |
| Successful-run server cleanup | Verified |

### Smoke-Test Improvements

After strengthening risk-rejection assertions and
adding initialization failure-path cleanup:

- TypeScript typecheck passed.
- Lint passed with zero errors and warnings.
- All eight browser tests passed.
- Final browser run passed all eight tests in 12.2 seconds.
- Production build and TypeScript typecheck passed.
- No listening servers remained on ports 18080 or 15173.
- Git diff whitespace validation passed.

A controlled portfolio assertion failure also
confirmed that Playwright reports failures, skips
dependent serial tests, and retains diagnostic
screenshots and traces.

Initialization-specific failure cleanup has not
yet been independently verified.

- Final frontend unit tests: 392/392 passed (13.03s).
- Final lint: zero warnings and zero errors.

## Known Limitations

- Chromium is the only automated browser.
- Firefox and WebKit have not been verified.
- Intermittent Vite WebSocket ECONNABORTED warnings
  were observed on Windows without failed assertions.
  Their precise cause has not been independently
  established.
- The tested baseline is an integrated candidate,
  not a final merged release revision.

## Related Issues

- [#46 - HTTP integration and concurrency](https://github.com/co-rtex/tradegrid/issues/46)
- [#73 - WebSocket stream](https://github.com/co-rtex/tradegrid/issues/73)
- [#77 - Live trading desk updates](https://github.com/co-rtex/tradegrid/issues/77)
- [#96 - Live portfolio refresh](https://github.com/co-rtex/tradegrid/issues/96)
- [#97 - Browser smoke tests](https://github.com/co-rtex/tradegrid/issues/97)
- [#92 - Final release acceptance checklist](https://github.com/co-rtex/tradegrid/issues/92)

Passing automated smoke tests does not establish
release readiness. Final manual acceptance and
leadership sign-off remain required under #92.