# Order Domain Model

This document specifies the TradeGrid Order model for [issue #6](https://github.com/co-rtex/tradegrid/issues/6). An order is the central object that flows through validation, risk, matching, execution, and persistence, so every module agrees on the fields and states described here.

The Go implementation lives in `backend/internal/orders`. This document describes behavior; the package is the executable form of it.

## Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `ID` | string | Uniquely identifies the order across the exchange. |
| `UserID` | string | The account that submitted the order. |
| `Symbol` | string | The instrument being traded, such as `AAPL`. |
| `Side` | enum | `BUY` or `SELL`. |
| `Type` | enum | `LIMIT` or `MARKET`. |
| `Price` | integer | Limit price in whole cents; `0` for a market order. |
| `Quantity` | integer | The quantity requested at acceptance. Never changes. |
| `Remaining` | integer | The part of `Quantity` that has not traded yet. |
| `Status` | enum | Lifecycle state; see below. |
| `CreatedAt` | timestamp | When the order was accepted. |
| `UpdatedAt` | timestamp | When the order last changed. Equals `CreatedAt` until the first fill or cancellation. |

`Quantity` and `Remaining` are kept as separate fields, matching the distinction the [matching engine specification](matching-engine.md) relies on: original quantity is fixed for the life of the order, and `Quantity = filled + Remaining` holds at every moment.

### Why prices are integer cents

Money in binary floating point does not compare or add exactly, and matching is built on exact price comparison. Prices are therefore whole cents in an integer, which is also what `backend/internal/matching` already uses. A price of `19900` means $199.00.

Whether cents are the right precision for every future instrument is a separate decision. Changing it is a change to this document, not something individual modules decide for themselves.

### No database coupling

The model carries no database tags, no SQL, and no driver imports. The same type is usable by the HTTP layer, the matching engine, and whatever persistence arrives with [issue #12](https://github.com/co-rtex/tradegrid/issues/12), without any one of them dictating the others' shape. JSON field names are defined on the struct because the API layer needs a stable wire form; a storage layer is free to map the fields differently.

## Closed Enumerations

`Side`, `Type`, and `Status` are closed enumerations, not free-form strings. A typo like `"sel"` or a locale-dependent `"Buy"` cannot reach the matching engine, and the compiler lists every case a `switch` has to handle.

Each enumeration reserves its zero value as `UNSPECIFIED`, which is never a valid member. An order that was never populated therefore fails validation instead of silently defaulting to `BUY` or to a real status. Parsing is exact: `ParseSide("buy")` is an error, so callers that accept user input normalize case first and get one consistent answer rather than each module inventing its own leniency.

### Side

| Value | Wire form | Meaning |
| --- | --- | --- |
| `SideBuy` | `BUY` | A request to purchase the symbol. |
| `SideSell` | `SELL` | A request to sell the symbol. |

### Type

| Value | Wire form | Meaning |
| --- | --- | --- |
| `TypeLimit` | `LIMIT` | Trades only at `Price` or better. |
| `TypeMarket` | `MARKET` | Trades at whatever price the book offers. |

The first matching engine accepts limit orders only; market orders are explicitly out of scope there. `MARKET` exists in the model now so that adding the behavior later does not require changing the shape of every order that has already been stored or serialized.

### Status

| Value | Wire form | Meaning |
| --- | --- | --- |
| `StatusNew` | `NEW` | Accepted, nothing filled yet. Resting on the book or about to be matched. |
| `StatusPartiallyFilled` | `PARTIALLY_FILLED` | At least one fill, with positive remaining quantity. |
| `StatusFilled` | `FILLED` | The entire quantity has traded. |
| `StatusCanceled` | `CANCELED` | Withdrawn before the remaining quantity traded. Requires positive remaining quantity. |
| `StatusRejected` | `REJECTED` | Refused by validation or risk. Never traded, never rested. |

`FILLED`, `CANCELED`, and `REJECTED` are terminal: no transition leaves them. `NEW` and `PARTIALLY_FILLED` are open, meaning the remaining quantity can still trade.

## Lifecycle

```text
                    ┌──────────────────┐
                    │       NEW        │
                    └──┬─────┬───────┬─┘
             fill < qty│     │fill   │reject
                       │     │= qty  │
                       v     │       v
          ┌────────────────┐ │  ┌──────────┐
     fill │PARTIALLY_FILLED│ │  │ REJECTED │
     <rem └──┬──────┬──────┘ │  └──────────┘
       ^     │      │        │
       └─────┘      │fill    │
                    │= rem   │
          cancel    v        v
       ┌──────┐  ┌────────────┐
       │CANCEL│  │   FILLED   │
       │  ED  │  └────────────┘
       └──────┘
```

Both `NEW` and `PARTIALLY_FILLED` can be canceled.

Three rules govern every transition:

1. **A fill only reduces remaining quantity.** It never changes `Quantity`. A fill larger than `Remaining` is an error, and the order is left untouched rather than partially updated.
2. **Cancellation keeps the fills that already happened.** A partially filled order that is canceled stops working but keeps its filled quantity; it does not revert. Cancellation withdraws quantity that has not traded, so there has to be some: a fully filled order has nothing left to withdraw and is `FILLED`, not `CANCELED`.
3. **Rejection applies only before trading.** An order that has received any fill cannot be rejected, because the counterparty to that fill has already traded.

Transitions take the timestamp as an argument rather than reading the clock. The matching engine specification requires deterministic replays, and hidden calls to the system clock would make the same input sequence produce different records on each run.

### Fills carry quantity, not price

A fill records how much traded. The price it traded at belongs to the Execution record the matching engine produces, which also names both counterparties. The order itself deliberately does not accumulate an average price; derived numbers like that are the Portfolio module's job and would be a second source of truth here.

## Invariants

These hold for any order that passes validation:

1. `Quantity > 0`.
2. `0 <= Remaining <= Quantity`.
3. `Quantity = filled + Remaining`, and `Quantity` never changes after acceptance.
4. `Side`, `Type`, and `Status` are members of their enumeration; the zero value is never a member.
5. `ID`, `UserID`, and `Symbol` are non-empty.
6. A `LIMIT` order has `Price > 0`; a `MARKET` order has `Price = 0`.
7. `NEW` and `REJECTED` imply `Remaining = Quantity`.
8. `PARTIALLY_FILLED` implies `0 < Remaining < Quantity`.
9. `FILLED` implies `Remaining = 0`.
10. `CANCELED` implies `Remaining > 0`.
11. `CreatedAt` and `UpdatedAt` are set, and `UpdatedAt` moves only when the order changes.

Invariants 7 through 10 are worth stating separately from the field rules: they are the combinations that make a status meaningful. Without them, a record could claim `FILLED` while still offering quantity to the book, and nothing downstream could tell which half to believe.

## Out of Scope

This document defines the shape and lifecycle of an order. It does not define:

- Trading rules such as permitted price increments, lot sizes, maximum order size, or the set of tradable symbols. Those belong to the input rules in [issue #3](https://github.com/co-rtex/tradegrid/issues/3) and the validation work in issues [#7](https://github.com/co-rtex/tradegrid/issues/7) and [#8](https://github.com/co-rtex/tradegrid/issues/8).
- Buying power, position limits, and other pre-trade risk checks.
- How IDs are generated, and how concurrent submissions are assigned an acceptance order.
- Matching behavior, which the [matching engine specification](matching-engine.md) owns.
- Order amendment, and the effect of cancellation on resting orders inside the matching engine.
- Persistence, including the table layout in [issue #12](https://github.com/co-rtex/tradegrid/issues/12).

Do not infer any of these from the model. The matching engine currently defines its own minimal order struct in `backend/internal/matching`; aligning it with this model is a follow-up, and the engine's behavior is unchanged by this document.
