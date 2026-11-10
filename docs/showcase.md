# November 30 Showcase and Handoff

The presentation plan, architecture walkthrough, contributor recognition, and
handoff package for the final workshop ([#98](https://github.com/co-rtex/tradegrid/issues/98)).
The technical runbook is [demo.md](demo.md) (#94); release evidence is
[release-acceptance.md](release-acceptance.md) (#92, #93); release
coordination is #95. This file does not repeat them.

**Before presenting, every claim and screenshot must match the merged,
verified release candidate.** Fixtures and component previews are never shown
as live behavior.

## Roles (to be confirmed by leadership)

| Role | Person | Confirmed |
| --- | --- | --- |
| Presenter | _to be assigned_ | No |
| Demo operator | _to be assigned_ | No |
| Backup operator | _to be assigned_ | No |
| Technical Q&A | _to be assigned_ | No |
| Rehearsal on the release candidate (by November 25) | _date and attendees_ | No |

## 8-Minute Outline

1. **Why TradeGrid (1 min).** A student-built, simulated trading platform to
   learn how financial software works, built by the UW–Madison FinTech Club
   through workshops: issue → branch → PR → review → merge.
2. **What we built (1 min).** Two simulated accounts, LIMIT orders with
   reservations, price-time matching, executions, a trading desk, a portfolio
   with average cost basis and P&L at fixed reference prices, live updates, a
   validated PostgreSQL schema, and an offline Python report.
3. **Live demo (4 min).** The [workshop script](demo.md#57-minute-workshop-demo-script):
   resting order, full fill with price improvement, partial fill, risk
   rejection, both portfolios updating live.
4. **Architecture (1 min).** See the walkthrough below.
5. **How we worked and what we learned (30 s).** Contracts first
   (`api.md`, `portfolio.md`, `realtime.md`), fixtures shared across backend,
   frontend, database, and analytics; small reviewed PRs; tests in CI.
6. **Limitations and roadmap (30 s).** Below. Say them plainly.

## Architecture Walkthrough

One sentence: *React reads authoritative REST snapshots from one Go process
that owns an in-memory exchange; after each committed trade, a WebSocket
notice tells every browser what to re-read.*

- **Write path.** `POST /api/v1/orders` → strict JSON decoding → one locked,
  atomic step: validate, reserve cash or shares, match by price then time,
  settle cash, shares, and cost basis on a copy, publish all or nothing.
- **Read path.** `GET` accounts, orders, order book, executions, and
  portfolio return copies taken under the same lock. The portfolio is valued
  on the server at fixed reference prices; the browser only formats cents.
- **Live path.** The exchange hands one small event per commit to a bounded
  hub without blocking; the stream sends it with a sequence number; browsers
  re-read only what changed, and everything after a reconnect, a gap, or a
  restart. A slow browser is dropped, never trading.
- **PostgreSQL** holds the designed schema, migrations, and seeds, validated
  by scripts; the running exchange does not use it yet.

Diagram: [architecture.md](architecture.md#semester-mvp-as-built).

## Fallback If the Live Demo Fails

1. **Live updates down:** continue with **Refresh** in the other window
   ([demo.md](demo.md#if-live-updates-are-unavailable)); every value is the
   same.
2. **Backend or browser problem:** restart the backend (it resets to the
   seed) and restart the script from step 1.
3. **No working machine:** show the screenshots or screen recording captured
   during the rehearsal on the release candidate. Capture them then, store
   them with the release evidence, and label them with the revision. Do not
   use component-preview pages or fixtures in their place.

## Contributor Recognition

Generated from GitHub's merged pull requests on November 9, 2026. Update it
from GitHub on the day; do not add contributions that are not in the PR or
issue record.

| GitHub account | Merged pull requests |
| --- | --- |
| Bill-Yunke-Zhang | #21 matching-engine tests; #28 align the matching order model; #48 execution records; #49 Sprint 2 API and exchange contract; #54 serialized exchange service; #80 accounts ledger in the exchange; #83 portfolio accounting contract; #84 average-cost accounting |
| PranavLR177 | #25 reject invalid quantities; #60 demo symbol catalog and validation |
| Vinuboi321 | #24 Order domain model |
| Ameya-me | #78 account order list; #79 live-connection status indicator |
| nonoche18 | #23 portfolio card; #91 order-book and recent-trades views |
| syedhaider082 | #26 visual system; #55 cents conversion and formatting helpers |
| lilbia | #29 order input validation; #61 developer setup and demo runbook |
| tyang392-crypto | #22 glossary |
| Ademark1 | #27 top navigation |
| co-rtex | #1 workshop foundation; #16, #17 backend and frontend bootstrap; #18 matching specification; #20 CI; #50 risk arithmetic; #51 PostgreSQL migrations; #52 frontend tests; #53 seeded settlement ledger; #56 HTTP server; #57 price boundary tests; #58 schema design; #87 reference prices; #88 positions table; #89 P&L breakdown; #90 real-time stream client |

Open contributor PRs at the time of writing, to recognize once merged:
#81 and #82 (Bill-Yunke-Zhang: stream contract and event hub), #85
(Ademark1: limit-order form), #86 (hellotheretm: read endpoints).

The final-sprint integration PRs #99–#111 (for issues #36, #39, #44, #46, #65,
#68, #69, #70, #73, #76, #77, #96, #97) and the documentation PR were opened from the co-rtex
account with AI assistance (Claude Code), as their descriptions state.

## Future Roadmap (not unfinished MVP promises)

- Authentication and per-user authorization.
- Durable runtime persistence on the PostgreSQL schema, with recovery.
- Order cancellation and amendment, and more order types.
- Real or delayed market data, and P&L history over time.
- Firefox, WebKit, and screen-reader checks in the automated suite.

## Merge Order

Contributor PRs stay with their owners; nobody else pushes to them.

1. **#86** (#37 reads): base for every HTTP change.
2. **#85** (#41 form): used by the trading desk.
3. **#81** (#71 contract) and **#82** (#72 hub). #82 conflicts with `main` in
   `backend/internal/exchange/service.go` only because #80 was squash-merged;
   its owner resolves it.
4. Independent, any time: **#99** (#39 schema and seeds), **#100** (#76
   analytics), **#101** (#65 valuation), **#102** (#68 cost-basis settlement).
5. **#103** (#36 order submission, after #86), then **#104** (#46 integration
   tests).
6. **#105** (#69 portfolio endpoint, after #86, #101, #102).
7. **#106** (#73 stream endpoint, after #86 and #82; rebase onto #103/#105 for
   a two-line router and README conflict).
8. **#107** (#44 trading desk, after #85), **#108** (#70 portfolio page),
   **#109** (#77 live desk), **#110** (#96 live portfolio).
9. **#111** (#97 smoke tests), then the documentation PR.

After each merge, rebase the next PR onto `main` and rerun CI.

## Handoff Archive

Fill these in on November 30:

| Item | Link |
| --- | --- |
| Final release commit on `main` | _SHA_ |
| Release acceptance results (#92) | [release-acceptance.md](release-acceptance.md) at that SHA |
| Runbook (#94) | [demo.md](demo.md) |
| Presentation slides or outline | _link_ |
| Rehearsal and demo recording | _link_ |
