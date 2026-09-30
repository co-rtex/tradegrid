# Matching Engine Behavior

This document specifies the first TradeGrid matching engine for [issue #13](https://github.com/co-rtex/tradegrid/issues/13). It is the behavioral source of truth for the table-driven tests in [issue #14](https://github.com/co-rtex/tradegrid/issues/14) and the first implementation. It defines observable results without requiring any particular programming language or data structure.

## Terms

An **order** requests a purchase (BUY) or sale (SELL) of a quantity of one symbol, such as AAPL. A **limit price** is the most a buyer will pay per unit or the least a seller will accept per unit. An **order book** holds the unfilled quantities waiting to trade, separately for each symbol.

| Term | Meaning |
| --- | --- |
| Bid | A buy order on the book. |
| Ask | A sell order on the book. |
| Best bid | The highest resting buy price for a symbol; absent if there are no bids. |
| Best ask | The lowest resting sell price for a symbol; absent if there are no asks. |
| Resting order | An order whose remaining quantity is already waiting on the book. |
| Incoming order | The newly accepted order currently being processed against the book. |
| Price level | All resting orders on one side at the same price for one symbol. |
| Marketable limit order | A limit order that can trade immediately with at least one opposite-side resting order within its limit price. |
| Execution (trade) | One match between an incoming order and one resting order, with a quantity and price. |
| Fill | Quantity traded for an order through an execution. An order can receive several fills. |
| Partial fill | A fill that leaves some of the order's quantity unfilled. |
| Full fill | The order's entire original quantity has traded, possibly across several executions. |
| Original quantity | The quantity requested when the order was accepted; this does not change as fills occur. |
| Remaining quantity | Original quantity minus the sum of that order's fills. |
| FIFO / time priority | First in, first out: at the same price, the earlier accepted resting order trades first. |

## Input Boundary and Arrival Order

The matching engine receives accepted limit orders after the Orders and Risk modules have validated them (see [architecture](architecture.md)). Each order has an identity, symbol, side, positive limit price, and positive original quantity. This specification assumes valid inputs and exact price and quantity comparisons; validation errors, numeric representation, and permitted increments belong to separate specifications.

Process inputs in a defined, sequential acceptance order. Finish all matching and book updates for one incoming order before processing the next. This acceptance order determines time priority, even when two orders have the same timestamp. Do not break ties using wall-clock timing, order identifiers, or an arbitrary iteration order. How concurrent submissions are assigned that acceptance order is outside this specification.

Only orders for the same symbol and opposite sides can match. A symbol's book starts empty and evolves through these rules. A test may instead supply an already valid starting book with explicit FIFO order at each price. Such a book contains only positive remaining quantities and has no compatible bid and ask left waiting to trade.

## Matching Rules

### Price and Time Priority

- An incoming BUY selects the **lowest** resting ask first.
- An incoming SELL selects the **highest** resting bid first.
- Better price always wins over arrival time. A newer ask at $99 precedes an older ask at $100 when a BUY arrives.
- Among orders at the selected price, choose the earliest accepted resting order (FIFO).
- A partially filled resting order retains its original priority; reducing its quantity does not move it behind newer orders.

### Marketability and Stopping

An incoming BUY is marketable when an ask exists and `buy limit >= best ask`. An incoming SELL is marketable when a bid exists and `sell limit <= best bid`. Equality allows a trade. With no opposite-side order for that symbol, no match is possible.

For each incoming order:

1. Start with remaining quantity equal to its original quantity.
2. If its remaining quantity is zero, stop. Otherwise select the best opposite-side resting order using price and then FIFO priority.
3. If no such order exists, or its price is outside the incoming limit, stop matching. A worse price cannot make the order marketable.
4. Execute a quantity equal to the **smaller of the two remaining quantities** at the **resting order's price**.
5. Subtract that executed quantity from both orders' remaining quantities. Record this execution before any later execution. Each incoming/resting pair produces its own execution; do not combine different resting orders into one execution even if prices are equal.
6. Remove a fully filled resting order from the active book. If it still has quantity, keep it in its original FIFO position. Repeat from step 2.
7. After matching stops, if the incoming order has positive remaining quantity, place that remainder on its own side at its original limit price, after earlier orders at that price. Otherwise it is fully filled and does not enter the active book.

An incoming remainder rests whether it received some fills or none. An order is not discarded merely because available matching quantity runs out. Original quantity remains distinct from remaining quantity throughout this process.

### Execution Price

Every trade executes at the **resting order's limit price**, not the incoming order's limit. The incoming limit determines which trades are allowed.

For example, a resting SELL of 10 AAPL at $199 matched by an incoming BUY of 10 AAPL with limit $200 executes 10 at **$199**. In the reverse direction, a resting BUY at $200 matched by an incoming SELL with limit $199 executes at **$200**. Both counterparties receive the same execution price and quantity.

## Worked Examples

Every example is independent and uses AAPL, dollars per share, and whole shares. These display choices do not establish allowed symbols, currencies, or quantity increments. `B` and `S` identifiers distinguish buy and sell orders within each example. Book entries use `ID: remaining @ price`. Bids are shown highest price first; asks lowest price first; entries at the same price are oldest first. Empty means there are no active orders on that side. No orders are omitted.

Unless stated otherwise, starting orders have not previously traded, so their original quantities equal their starting remaining quantities. Execution tables list trades in their required order and show both counterparties' remaining quantities immediately after each execution. Zero-remaining orders are absent from the final book.

### Example 1 — No Match

**Starting book:** bids: empty; asks: `S1: 5 @ $101`.

**Incoming order:** `B1`, BUY 4, limit $100.

**Why:** $100 is below the best ask of $101, so the BUY is not marketable.

**Matching sequence / executions:** none. All 4 shares of B1 rest at $100.

**Final remaining quantities:** B1 = 4; S1 = 5.

**Final book:** bids: `B1: 4 @ $100`; asks: `S1: 5 @ $101`.

### Example 2 — Simple Full Fill

**Starting book:** bids: empty; asks: `S1: 10 @ $199`.

**Incoming order:** `B1`, BUY 10, limit $200.

**Why:** $200 >= $199, so B1 can buy all 10 shares from S1.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | B1 | S1 | 10 | $199 | 0 | 0 |

**Final remaining quantities:** B1 = 0; S1 = 0. Both are fully filled.

**Final book:** bids: empty; asks: empty.

### Example 3 — Partial Fill with Incoming Remainder

**Starting book:** bids: empty; asks: `S1: 4 @ $100`.

**Incoming order:** `B1`, BUY 10, limit $101.

**Why:** $101 >= $100, but only 4 shares are available. After S1 is consumed, no asks remain, so B1's remaining 6 shares rest at its $101 limit.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | B1 | S1 | 4 | $100 | 6 | 0 |

**Final remaining quantities:** B1 = 6 (original 10, filled 4); S1 = 0.

**Final book:** bids: `B1: 6 @ $101`; asks: empty.

### Example 4 — FIFO at the Same Price

**Starting book:** bids: empty; asks: `S1: 4 @ $100`, `S2: 6 @ $100`, `S3: 2 @ $100`. Acceptance order is S1, then S2, then S3.

**Incoming order:** `B1`, BUY 7, limit $100.

**Why:** Equality is marketable. All asks have the same price, so S1 trades first, then S2. B1 fills before S3 receives any execution.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | B1 | S1 | 4 | $100 | 3 | 0 |
| 2 | B1 | S2 | 3 | $100 | 0 | 3 |

**Final remaining quantities:** B1 = 0; S1 = 0; S2 = 3; S3 = 2. S2 keeps priority over S3 for the next marketable BUY.

**Final book:** bids: empty; asks: `S2: 3 @ $100`, `S3: 2 @ $100` (in that FIFO order).

### Example 5 — Multiple Price Levels, Best Bid First

**Starting book:** bids: `B3: 4 @ $102`, `B2: 3 @ $101`, `B1: 2 @ $100`; asks: empty. Acceptance order is B1, then B2, then B3: the best bid is the newest order.

**Incoming order:** `S1`, SELL 9, limit $100.

**Why:** All three bids meet the sell limit. Price beats time: S1 sells at $102 first, then $101, then $100, despite B1 having arrived first.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | S1 | B3 | 4 | $102 | 5 | 0 |
| 2 | S1 | B2 | 3 | $101 | 2 | 0 |
| 3 | S1 | B1 | 2 | $100 | 0 | 0 |

**Final remaining quantities:** S1 = 0; B1 = 0; B2 = 0; B3 = 0.

**Final book:** bids: empty; asks: empty.

### Example 6 — Partial Walk Across Ask Levels

**Starting book:** bids: empty; asks: `S2: 3 @ $99`, `S1: 8 @ $100`. S1 arrived before S2.

**Incoming order:** `B1`, BUY 7, limit $101.

**Why:** Both asks meet the buy limit. The newer, lower-priced S2 trades first. B1 then consumes only 4 of S1's 8 shares and is fully filled. S1 remains at its original priority.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | B1 | S2 | 3 | $99 | 4 | 0 |
| 2 | B1 | S1 | 4 | $100 | 0 | 4 |

**Final remaining quantities:** B1 = 0; S2 = 0; S1 = 4 (original 8, filled 4).

**Final book:** bids: empty; asks: `S1: 4 @ $100`.

### Example 7 — Stop at the Non-Marketable Boundary

**Starting book:** bids: `B1: 3 @ $102`, `B2: 4 @ $101`, `B3: 5 @ $100`; asks: empty.

**Incoming order:** `S1`, SELL 10, limit $101.

**Why:** S1 can sell at $102 and $101. After these bids are consumed, the next bid is $100, below S1's minimum of $101. Matching stops even though S1 still has 3 shares and B3 still has 5. S1's remainder rests at $101.

| Step | Incoming | Resting | Executed quantity | Execution price | Incoming remaining | Resting remaining |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | S1 | B1 | 3 | $102 | 7 | 0 |
| 2 | S1 | B2 | 4 | $101 | 3 | 0 |

**Final remaining quantities:** S1 = 3; B1 = 0; B2 = 0; B3 = 5.

**Final book:** bids: `B3: 5 @ $100`; asks: `S1: 3 @ $101`.

## Matching Invariants

These properties apply to valid limit-order inputs and books within this specification. Quantities before an execution mean the quantities immediately before that particular step, not the original order quantities.

1. **Compatibility:** Every execution joins opposite sides of the same symbol and requires `buy limit >= sell limit`. Orders for other symbols are unchanged.
2. **Price priority:** No execution uses a worse price while a better compatible opposite-side price has positive remaining quantity.
3. **FIFO:** No execution uses a later order at the same price while an earlier order at that price has positive remaining quantity.
4. **Resting price:** Every execution price equals the selected resting order's price and satisfies `sell limit <= execution price <= buy limit`.
5. **Execution quantity:** Every execution has positive quantity equal to the smaller of the two pre-execution remaining quantities. It never exceeds either quantity.
6. **Quantity bounds:** Each order always has `0 <= remaining <= original`. Original quantity is unchanged, and `original = cumulative filled + remaining`.
7. **Conservation:** An execution of quantity q reduces the buyer's remaining quantity by q and the seller's by q. Total bought equals total sold; counting both counterparties' filled quantities counts each trade twice.
8. **Full fills:** No zero-remaining order stays active or participates in a subsequent execution. A fully filled incoming order never rests.
9. **Retained priority:** Partial fills of a resting order preserve its position ahead of later orders at the same price.
10. **Continued matching:** An incoming order with positive remaining quantity continues executing while compatible marketable resting quantity exists.
11. **Remainder placement:** At the end of processing, any positive incoming remainder rests exactly once at its original limit, behind earlier orders at that price. This applies whether matching stopped at a limit boundary or because the opposite side is empty.
12. **Completed book:** After each input is fully processed, active quantities are positive. If both sides exist for a symbol, `best bid < best ask`; no compatible pair remains unmatched.
13. **Execution ordering:** Record one execution per selected resting order in selection order, without aggregating trades across resting orders. Complete one input's executions before the next input's executions.
14. **Determinism:** Given the same valid starting book (including remaining quantities and FIFO order) and the same ordered inputs, produce the same ordered executions (counterparty identities, symbol, quantity, price) and final book (identities, sides, prices, remaining quantities, FIFO order). Generated timestamps and execution identifiers are not specified here and must not affect matching.

## Using This Specification for Issue #14

Use each worked example as a separate table-driven case: starting book and FIFO order, incoming order, ordered expected executions, and complete expected final book. Assert order identities as well as prices and quantities; aggregate totals alone will miss FIFO errors. Check the incoming remainder even when it is zero and verify that fully filled resting orders disappear.

Additional cases can mirror BUY cases as SELL cases (and vice versa), submit into an empty book, use a different symbol to check isolation, or feed a second input into Example 4's final book to verify S2 still precedes S3. Use equal timestamps with a known acceptance order to check that FIFO follows that order. Compare exact prices and quantities, and run the same input sequence again from the same initial state to check determinism.

The examples and invariants specify behavior, not exact order structs, package names, storage layouts, or test framework choices.

## Out of Scope

This first version defines accepted limit-order matching only. It intentionally leaves these behaviors undefined:

- Market, stop, stop-limit, and iceberg/hidden orders; auctions.
- Self-trade prevention and account-based matching restrictions.
- IOC (immediate-or-cancel), FOK (fill-or-kill), other advanced time-in-force rules, and expiration.
- Trading halts, exchange fees, and advanced market-maker behavior.
- Distributed matching, persistence, recovery, and concurrent input sequencing.
- Cancellation and amendment processing, including any effect of amendments on time priority. The Orders module owns cancellations, but their matching interaction needs a separate specification.
- Validation/rejection responses, risk/accounting rules, numeric encoding and permitted increments, generated execution metadata, and client event delivery.

Do not infer behavior for these features from the examples. Future specifications must resolve them before tests or implementations depend on them.
