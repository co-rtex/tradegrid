# Order Input Validation

This document defines the initial order-input validation rules for [issue #3](https://github.com/co-rtex/tradegrid/issues/3). It applies at the Orders boundary, before risk checks and matching.

TradeGrid's current MVP supports **LIMIT** buy and sell orders. The backend does not yet expose an order-submission API or a serialized order model, so this document defines the logical order values that must be valid. It does not establish HTTP request field names, JSON types, error responses, or status codes.

## Fields

| Field | Required | Current rule |
| --- | --- | --- |
| Symbol | Yes | Identifies the instrument being traded, for example `AAPL`. A submitted order must include a symbol. The permitted symbol list, ticker format, and case-handling rule are not yet defined. |
| Side | Yes | Exactly `BUY` or `SELL`. `BUY` requests a purchase; `SELL` requests a sale. |
| Order type | Yes | Exactly `LIMIT` for the current MVP. Market-order behavior is not defined yet. |
| Quantity | Yes | A positive whole number of shares. Quantity must be greater than zero; zero, negative, and fractional quantities are invalid. |
| Limit price | Yes | A positive price. It must be greater than zero. Zero and negative prices are invalid. |

## Limit Orders

A **limit order** sets a price boundary for a trade:

- A `BUY` limit order will buy only at its limit price or lower.
- A `SELL` limit order will sell only at its limit price or higher.

The [Matching Engine Behavior](matching-engine.md) document defines how accepted limit orders match. It assumes that their symbol, side, quantity, and price have already been validated.

## Price Representation

The matching engine currently stores each limit price as an integer number of cents. For example, its internal representation of a value of $175.25 is `17525` cents.

This is an internal implementation detail, not a client API rule. The format an eventual API accepts, the currency, allowed decimal precision, and any conversion from an API value to integer cents require team agreement before an API is implemented. This document only requires that the logical limit price is positive.

## Valid Examples

These examples describe valid logical orders; they are not JSON request bodies or a promise of a future API format.

| Symbol | Side | Order type | Quantity | Limit price | Why valid |
| --- | --- | --- | --- | --- | --- |
| `AAPL` | `BUY` | `LIMIT` | 10 | A positive price | The order has every required value, a supported side and type, a whole positive quantity, and a positive limit price. |
| `MSFT` | `SELL` | `LIMIT` | 5 | A positive price | A sell limit order follows the same input rules as a buy limit order. |

## Invalid Examples

| Example | Why invalid |
| --- | --- |
| An order with no symbol | A symbol is required. |
| `AAPL`, `HOLD`, `LIMIT`, quantity 1, positive limit price | `HOLD` is not a supported side; only `BUY` and `SELL` are valid. |
| `AAPL`, `BUY`, `MARKET`, quantity 1, no limit price | `MARKET` is outside the current limit-order MVP. Its matching behavior has not been defined. |
| `AAPL`, `BUY`, `LIMIT`, quantity 0, positive limit price | Quantity must be greater than zero. Negative and fractional quantities are also invalid. |
| `AAPL`, `SELL`, `LIMIT`, quantity 1, no limit price | A limit order requires a limit price. |
| `AAPL`, `SELL`, `LIMIT`, quantity 1, limit price 0 | Limit price must be greater than zero. Negative prices are also invalid. |

## Future Decisions

The following rules are intentionally outside this first validation contract and need team agreement before they are added:

- Which symbols are permitted, including ticker length and uppercase/lowercase handling.
- Whether and when to support market orders or other order types.
- The external API's request fields, JSON types, and error format.
- Currency, price precision, tick size, and conversion from an API price to the matching engine's integer cents.
- Maximum order quantities or prices, and risk rules such as available buying power or short-selling policy.
