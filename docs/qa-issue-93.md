# Responsive and accessibility QA — issue #93

This is a preparation record, **not release-candidate sign-off**. Complete the
matrix below after the actual integrated release candidate is available and a
tech lead has verified it. Do not close #93 based on this document alone.

## Build and scope

- Main-branch baseline: `cb9903825884e7a8e1b21dd49d99f99b2ae60da4`, plus
  this branch's example-data label and regression test.
- At this baseline, the app is a navigation shell with a static portfolio
  example. The integrated trading desk (#44), portfolio page (#70), and live
  refresh (#77) are still open and are not in `main`.
- For exploratory unit-test evidence only, the separately preserved integration
  candidate `45a8454b0744d3e6d17ef92e01574f00e3d69b9f` was also tested. It is
  unmerged and is **not** the release candidate or the code in this PR.
- This environment had no usable Chromium, Firefox, Safari/WebKit, or screen
  reader. Chromium's executable was missing and the Playwright browser download
  returned HTTP 403. A separate browser could not reach the local server. No
  responsive screenshots or real-browser results were obtained.

## Results recorded on 2026-11-23

| Check | Build | Expected | Actual and evidence | Result |
| --- | --- | --- | --- | --- |
| Frontend lint, typecheck, unit tests, build | Main baseline plus label | Commands exit successfully | `npm run lint`, `npm run typecheck`, `npm test`, `npm run build`: passed; 310 tests. Vitest uses jsdom, not a browser. | Pass (automated unit/build only) |
| Example portfolio is identified as a mock | Main baseline plus label | Example amounts cannot be mistaken for an account snapshot | A visible text label is rendered in the portfolio section; `App.test.tsx` checks it. No screenshot was captured. | Pass (DOM test only) |
| Exploratory integration unit suite | Unmerged `45a8454` | Suite passes | One run had 391 passed and one failed: `PortfolioPage > shows the seeded portfolio exactly as the server values it` expected the last `onBalanceChange` call to contain available cash, but received `undefined`. An isolated rerun passed all 15 tests, and a subsequent full rerun passed all 392. The cause is unconfirmed; investigate and rerun on the integrated build. | Intermittent / unresolved |

The example-data issue was found by source inspection: the current shell
supplied hard-coded dollar values and an empty positions list with no on-screen
indication they were examples. The added label does not claim they are live.

## Required release-candidate matrix

Record the candidate commit, OS, exact browser and version, tester, date,
screenshots and actual result in each row. Do not replace `NOT RUN` with
`PASS` until the named browser and viewport are tested on the integrated build.
Use screenshots that show the full page and, where needed, focused controls or
the scrollable table region. Attach them to the review or link checked-in
evidence. No screenshots are attached by this preparation pass.

| Browser | Viewport | Steps | Expected | Actual | Screenshots | Result |
| --- | --- | --- | --- | --- | --- | --- |
| Chromium | 375 × 812 | Open desk, then portfolio; inspect controls and tables | No page-level horizontal overflow, clipping, or unreadable content; table scrolling remains usable | Not run | Not captured | NOT RUN |
| Chromium | 768 × 1024 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |
| Chromium | 1280 × 800 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |
| Firefox | 375 × 812 | Open desk, then portfolio; inspect controls and tables | Same | Not run | Not captured | NOT RUN |
| Firefox | 768 × 1024 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |
| Firefox | 1280 × 800 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |
| Safari | 375 × 812 | Open desk, then portfolio; inspect controls and tables | Same | Not run | Not captured | NOT RUN |
| Safari | 768 × 1024 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |
| Safari | 1280 × 800 | Repeat desk and portfolio checks | Same | Not run | Not captured | NOT RUN |

If Safari cannot be run at an exact emulated viewport, record the actual device,
browser version, viewport and any deviation rather than assuming coverage.

## Interaction and state checks

Run these on the same integrated build; record actual observations, screenshots
and browser in the review. All are **NOT RUN** in this preparation pass.

| Scenario and steps | Expected |
| --- | --- |
| Keyboard only: use Tab, Shift+Tab, Enter, Space and arrow keys through navigation, account/symbol selectors, order inputs, Submit, Refresh, portfolio and reconnect/retry controls. | Logical focus order, visible focus, no trapped or lost focus, labels identify each control. |
| Submit an invalid order, a rejected order, and a delayed submission; recover using only the keyboard. | Errors/pending state are announced and visible; the user can correct or retry deliberately without accidental duplicate submissions. |
| Test initial loading, valid empty results, disconnected/reconnecting status and server failure. | Loading, empty, disconnected and error states are distinct from valid zero balances or prices; recovery is clear. |
| Switch Alice/Bob and symbols rapidly while an earlier response is delayed. | A stale response never displays under the new selection; manual refresh remains usable. |
| Inspect table header associations, inputs, status messages, BUY/SELL, P&L and connection states. | Accessible names and headers are meaningful; status/error announcements work; labels or symbols convey meaning without color alone. |
| At least one screen reader (VoiceOver or NVDA), keyboard-only. Navigate selectors and submit an invalid order; trigger a status change. | Record announced names, errors and live status changes; no missing or misleading announcements. |

## Suggested verification workflow

1. Once #44, #70 and #77 are integrated, record the exact release-candidate SHA
   and follow `docs/demo.md` for its backend and frontend startup instructions.
2. Run `npm ci`, `npm run lint`, `npm run typecheck`, `npm test`, and
   `npm run build` in `frontend/`. Record command output and failures.
3. In each available browser, perform every viewport row above. Verify page
   overflow as well as intentional horizontal scrolling inside tables. Use the
   app normally and capture screenshots; jsdom alone cannot check layout.
4. Perform the interaction/state checks and the screen-reader smoke test. Record
   exact reproduction steps and expected versus actual behavior for defects;
   link them to the owning issue or a narrowly scoped bug PR as appropriate.
5. Update this matrix honestly for unavailable browsers or assistive tools,
   resolve release-blocking findings with review, and obtain tech-lead
   verification before treating #93 as complete.
