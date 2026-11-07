# Portfolio Accounting Contract

This document is the single source of truth for Sprint 3 portfolio semantics ([issue #62](https://github.com/co-rtex/tradegrid/issues/62)). Reference prices ([#63](https://github.com/co-rtex/tradegrid/issues/63)), the cost-basis calculator ([#64](https://github.com/co-rtex/tradegrid/issues/64)), valuation ([#65](https://github.com/co-rtex/tradegrid/issues/65)), the positions table and P&L breakdown ([#66](https://github.com/co-rtex/tradegrid/issues/66), [#67](https://github.com/co-rtex/tradegrid/issues/67)), exchange integration ([#68](https://github.com/co-rtex/tradegrid/issues/68)), the portfolio endpoint ([#69](https://github.com/co-rtex/tradegrid/issues/69)), and the portfolio page ([#70](https://github.com/co-rtex/tradegrid/issues/70)) build against it. Change it through a pull request before changing code that depends on it.

It extends the [Sprint 2 API and Exchange Contract](api.md). Reservations, settlement, and every Sprint 2 endpoint stay exactly as they are; this document only adds cost basis, P&L, valuation, and one new read endpoint.

## Rules in One Paragraph

Every amount is an integer number of US cents. Each account tracks, per symbol, the **cost basis** of the shares it holds and its cumulative **realized P&L**. Buying adds the actual execution cost to the basis. Selling removes an average-cost share of the basis, rounded down, and realizes the proceeds minus that removed basis. Positions are valued at a fixed, simulated **reference price** per symbol, never at a live or last-trade price. Starting shares carry a basis equal to their reference value, so a fresh account shows zero P&L.

## Definitions

All values are per account. "Per symbol" values exist for every tradable symbol.

| Term | Field | Definition |
| --- | --- | --- |
| Cash total | `cash.total` | Cash the account owns, including reserved cash. Same as in [api.md](api.md#reservations-and-settlement). |
| Cash reserved | `cash.reserved` | Cash held by resting BUY orders. Still owned. |
| Cash available | `cash.available` | `total - reserved`. What a new BUY may use. |
| Shares total | `total` | Shares owned, including reserved shares. |
| Shares reserved | `reserved` | Shares held by resting SELL orders. Still owned. |
| Shares available | `available` | `total - reserved`. What a new SELL may use. |
| Cost basis | `costBasis` | What the account paid for the shares it still holds, under average cost (below). Covers all `total` shares, reserved or not. `0` when `total` is `0`. |
| Reference price | `referencePrice` | The fixed simulated price per share used to value a position (below). Not an execution price. |
| Market value | `marketValue` | `total × referencePrice`. |
| Unrealized P&L | `unrealizedPnl` | `marketValue - costBasis`. Gain or loss on shares still held. May be negative. |
| Realized P&L | `realizedPnl` | Sum, over every SELL fill since the server started, of `proceeds - allocated basis`. May be negative. Kept after the position closes. |
| Total equity | `totals.equity` | `cash.total + Σ marketValue`. |

**Reserved amounts are counted once.** Reserved cash is part of `cash.total`, and reserved shares are part of `total` and so of `marketValue`. Equity adds the totals; it never adds or subtracts reserved amounts separately. A resting order does not change equity.

P&L stands for profit and loss. "Fill" and "execution" mean the same thing here: one `executions.Record`.

## Reference Prices

Positions are valued at one fixed reference price per symbol, defined in the instruments catalog ([#63](https://github.com/co-rtex/tradegrid/issues/63)):

| Symbol | `referencePrice` |
| --- | --- |
| `AAPL` | `19000` ($190.00) |
| `MSFT` | `42000` ($420.00) |

- They are **simulated constants**, not market data. Nothing fetches, polls, or updates them, and trades do not move them.
- They are not the last execution price. Using the last trade would let one demo user change the other's unrealized P&L by trading with themselves at any price.
- A supported symbol without a reference price is a server bug: the portfolio read fails with `INTERNAL`. It is never valued as `0`.

Changing these values changes the seeded basis below and every example in this document, so it needs a pull request to this contract.

## Seeded Positions

The two demo accounts start with 100 AAPL and 100 MSFT each ([api.md](api.md#demo-accounts-and-symbols)). Their starting cost basis is `quantity × referencePrice`:

| Account | Symbol | `total` | `costBasis` | `realizedPnl` |
| --- | --- | --- | --- | --- |
| `alice`, `bob` | `AAPL` | 100 | `1900000` ($19,000.00) | `0` |
| `alice`, `bob` | `MSFT` | 100 | `4200000` ($42,000.00) | `0` |

So each account starts with `marketValue = costBasis`, zero unrealized and realized P&L, and equity `10000000 + 1900000 + 4200000 = 16100000` ($161,000.00).

**Why the basis must not be zero or missing.** The demo never bought these shares; they were granted. A basis of `0` would report the full $61,000.00 market value as unrealized gain before any trade, and selling a granted share would show its whole price as realized profit. Neither is a real gain. Seeding the basis at the reference value means P&L measures only what trading changed. The exchange ([#68](https://github.com/co-rtex/tradegrid/issues/68)) must seed this basis explicitly, never infer it.

## Average-Cost Accounting

Each account keeps, per symbol, `total` shares (as today), `costBasis`, and `realizedPnl`. One execution of `q` shares at execution price `p` changes them as follows. `p` is the execution price (the resting order's price), **never** the incoming order's limit.

**Buyer** (BUY fill):

```text
costBasis   += p × q
total       += q
```

**Seller** (SELL fill), with `Q` = `total` and `B` = `costBasis` before the fill:

```text
allocated    = B                  if q == Q   (closing the position)
             = floor(B × q / Q)   otherwise
costBasis   -= allocated
total       -= q
realizedPnl += p × q - allocated
```

**Rounding.** The only division is the allocation, and it rounds down (truncates, since every value is positive). Selling the last share allocates whatever basis remains, so a position sold in any number of pieces allocates exactly its full basis: no cent is created or lost.

Example: 3 shares with basis `1000`. Selling 1 allocates `floor(1000 × 1 / 3) = 333`, leaving `667`. Selling 1 more allocates `floor(667 × 1 / 2) = 333`, leaving `334`. Selling the last one allocates `334`. Total allocated: `333 + 333 + 334 = 1000`.

`B × q` can exceed `int64` even when the result fits, so implementations compute it with a 128-bit intermediate (`math/bits.Mul64` and `bits.Div64`) or `math/big`. Every other step uses checked `int64` arithmetic. Floating point is never used for money, quantities, or intermediate results.

**Order of application.** A submission's executions are applied in execution `sequence` order, buyer and seller for each, inside the same candidate state as cash, shares, and reservations. They are published together with the rest of the submission or not at all ([api.md](api.md#reservations-and-settlement)).

**Same-account executions** (`buyerUserId == sellerUserId`) leave `costBasis` and `realizedPnl` unchanged, just as cash and share totals net to zero. Trading with yourself cannot create realized P&L or change average cost.

**Errors.** A SELL fill for more than `total` shares, a quantity or price that is not positive, an input that breaks the invariants below, or a result outside `±9007199254740991` is an error. During settlement it is a bug (reservations make it impossible) and rolls the submission back with `INTERNAL`. The calculator returns new values and never changes its inputs on error.

## Worked Examples

The examples run in order on the same exchange, starting from the seed. Each step lists only what changed. Cash and share columns follow the Sprint 2 settlement rules; basis and P&L follow this document. AAPL is valued at `19000` and MSFT at `42000` throughout.

### 1. Basic BUY and SELL

Alice `SELL 10 AAPL @ 18500` rests. Bob `BUY 10 AAPL @ 18500` executes 10 at `18500`, so proceeds and cost are `185000`.

| Account | AAPL `total` | AAPL `costBasis` | AAPL `realizedPnl` | AAPL `unrealizedPnl` | `cash.total` | Equity |
| --- | --- | --- | --- | --- | --- | --- |
| Alice | 100 → 90 | `1900000` → `1710000` | `0` → `-5000` | `0` | `10000000` → `10185000` | `16095000` |
| Bob | 100 → 110 | `1900000` → `2085000` | `0` | `0` → `5000` | `10000000` → `9815000` | `16105000` |

Alice allocates `floor(1900000 × 10 / 100) = 190000` and realizes `185000 - 190000 = -5000`: she sold below her $190.00 average cost. Bob's 110 shares are worth `110 × 19000 = 2090000` against a basis of `2085000`.

### 2. Price improvement

Alice `SELL 5 AAPL @ 18800` rests. Bob `BUY 5 AAPL @ 19500` executes 5 at the resting price `18800`. Bob reserved `97500` but pays `94000`; `3500` returns to available cash.

| Account | AAPL `total` | AAPL `costBasis` | AAPL `realizedPnl` | AAPL `unrealizedPnl` | `cash.total` | Equity |
| --- | --- | --- | --- | --- | --- | --- |
| Alice | 90 → 85 | `1710000` → `1615000` | `-5000` → `-6000` | `0` | `10185000` → `10279000` | `16094000` |
| Bob | 110 → 115 | `2085000` → `2179000` | `0` | `5000` → `6000` | `9815000` → `9721000` | `16106000` |

Bob's basis grows by the `94000` he paid, **not** his `97500` limit. Alice allocates `floor(1710000 × 5 / 90) = 95000` and realizes `94000 - 95000 = -1000`.

### 3. Partial fill with rounding

Bob `SELL 7 AAPL @ 19300` rests. Alice `BUY 3 AAPL @ 19300` executes 3 at `19300` (`57900`). Bob's order stays `PARTIALLY_FILLED` with 4 shares still reserved.

| Account | AAPL `total` | AAPL `costBasis` | AAPL `realizedPnl` | AAPL `unrealizedPnl` | `cash.total` | Equity |
| --- | --- | --- | --- | --- | --- | --- |
| Alice | 85 → 88 | `1615000` → `1672900` | `-6000` | `0` → `-900` | `10279000` → `10221100` | `16093100` |
| Bob | 115 → 112 | `2179000` → `2122157` | `0` → `1057` | `6000` → `5843` | `9721000` → `9778900` | `16106900` |

Bob allocates `floor(2179000 × 3 / 115) = floor(56843.47…) = 56843` and realizes `57900 - 56843 = 1057`. His 4 reserved shares stay in `total`, `costBasis`, and `marketValue`. Alice paid above the reference price, so her unrealized P&L falls to `88 × 19000 - 1672900 = -900`.

### 4. Multiple fills that close a position

Alice `SELL 60 MSFT @ 41500` rests, then Alice `SELL 40 MSFT @ 42500` rests. Bob `BUY 100 MSFT @ 43000` executes 60 at `41500` (`2490000`), then 40 at `42500` (`1700000`). Bob reserved `4300000` and pays `4190000`.

| Step | Alice MSFT `total` | Alice MSFT `costBasis` | Alice MSFT `realizedPnl` |
| --- | --- | --- | --- |
| Before | 100 | `4200000` | `0` |
| Fill 1: allocate `floor(4200000 × 60 / 100) = 2520000` | 60 → 40 | `1680000` | `2490000 - 2520000 = -30000` |
| Fill 2: closing, allocate the remaining `1680000` | 40 → 0 | `0` | `-30000 + (1700000 - 1680000) = -10000` |

| Account | MSFT `total` | MSFT `costBasis` | MSFT `realizedPnl` | MSFT `unrealizedPnl` | `cash.total` | Equity |
| --- | --- | --- | --- | --- | --- | --- |
| Alice | 100 → 0 | `4200000` → `0` | `0` → `-10000` | `0` | `10221100` → `14411100` | `16083100` |
| Bob | 100 → 200 | `4200000` → `8390000` | `0` | `0` → `10000` | `9778900` → `5588900` | `16116900` |

Alice's closed MSFT row still reports `realizedPnl: -10000`, with `total`, `costBasis`, `marketValue`, and `unrealizedPnl` all `0`.

### 5. Same-account trade

Alice `SELL 2 AAPL @ 19000` rests. Alice `BUY 2 AAPL @ 19000` executes against it. Cash, shares, cost basis, and P&L are all unchanged; both reservations are released.

### Reconciliation after the examples

| Account | Cash | AAPL total / basis / realized / unrealized | MSFT total / basis / realized / unrealized | Realized + unrealized | Equity |
| --- | --- | --- | --- | --- | --- |
| Alice | `14411100` | 88 / `1672900` / `-6000` / `-900` | 0 / `0` / `-10000` / `0` | `-16900` | `16083100` |
| Bob | `5588900` | 112 / `2122157` / `1057` / `5843` | 200 / `8390000` / `0` / `10000` | `16900` | `16116900` |

Checks: each account's equity minus `16100000` equals its realized plus unrealized P&L (`-16900` and `16900`); combined equity is `32200000`; combined P&L is `0`; AAPL totals add to 200 and MSFT totals to 200.

## Invariants

Implementations and tests should check these after every committed submission:

1. Every amount is an integer within `0..9007199254740991`, except `realizedPnl` and `unrealizedPnl`, which may be negative and stay within `±9007199254740991`.
2. `total == 0` implies `costBasis == 0`. `total > 0` implies `costBasis >= total` (every share cost at least one cent, and rounding down never takes a remaining share's basis below one cent).
3. For each account: `equity - 16100000 == Σ realizedPnl + Σ unrealizedPnl`. This holds because the seeded basis equals the seeded market value.
4. Across both accounts: combined equity is `32200000`, combined realized plus unrealized P&L is `0`, and each symbol's shares total 200. With fixed reference prices, trading only moves value between the accounts.
5. A rejected or rolled-back submission changes no basis or P&L. A resting order with no fills changes no basis, P&L, or equity.
6. Reading a portfolio changes nothing, and the returned value is a copy.

## API: `GET /api/v1/portfolio?userId=alice`

Proposed for [#69](https://github.com/co-rtex/tradegrid/issues/69); it is additive and changes no Sprint 2 endpoint. `GET /api/v1/accounts` keeps its current shape and gains no fields.

Returns one account's portfolio as a single consistent snapshot, taken under the exchange's serialized boundary. Alice after example 1:

```json
{
  "userId": "alice",
  "displayName": "Alice (demo)",
  "asOfSequence": 2,
  "cash": { "total": 10185000, "reserved": 0, "available": 10185000 },
  "positions": [
    {
      "symbol": "AAPL",
      "total": 90,
      "reserved": 0,
      "available": 90,
      "costBasis": 1710000,
      "referencePrice": 19000,
      "marketValue": 1710000,
      "unrealizedPnl": 0,
      "realizedPnl": -5000
    },
    {
      "symbol": "MSFT",
      "total": 100,
      "reserved": 0,
      "available": 100,
      "costBasis": 4200000,
      "referencePrice": 42000,
      "marketValue": 4200000,
      "unrealizedPnl": 0,
      "realizedPnl": 0
    }
  ],
  "totals": {
    "costBasis": 5910000,
    "marketValue": 5910000,
    "unrealizedPnl": 0,
    "realizedPnl": -5000,
    "equity": 16095000
  }
}
```

| Field | Meaning |
| --- | --- |
| `asOfSequence` | Acceptance sequence of the last committed submission included, as in `GET /api/v1/orderbook`. `0` before any order. |
| `cash` | Identical to the account's `cash` in `GET /api/v1/accounts`. |
| `positions` | Every tradable symbol, sorted by symbol, including zero holdings. Share fields match `GET /api/v1/accounts`. |
| `totals.costBasis`, `totals.marketValue`, `totals.unrealizedPnl`, `totals.realizedPnl` | Sums over `positions`. |
| `totals.equity` | `cash.total + totals.marketValue`. |

All values are integer cents or whole shares. No field is ever `null` or omitted.

**Empty and closed positions.** A symbol with `total: 0` still appears, with `costBasis`, `marketValue`, and `unrealizedPnl` of `0` and its cumulative `realizedPnl`. There is no `averageCost` field: it is not an exact cent amount. A client that displays it computes `costBasis / total` for `total > 0`, rounds half up to the cent for display only, and shows `—` when `total` is `0`.

**Errors** use the [Sprint 2 error body](api.md#errors) and add no codes:

| Status | `code` | When |
| --- | --- | --- |
| 400 | `INVALID_QUERY` | `userId` is missing or empty (`field: "userId"`). |
| 404 | `UNKNOWN_USER` | `userId` is not a demo account (`field: "userId"`). |
| 405 | `METHOD_NOT_ALLOWED` | Not `GET`. Includes `Allow: GET`. |
| 500 | `INTERNAL` | A missing reference price, an overflow, or any broken invariant. Nothing changes. |

**Refreshing.** A portfolio changes only when a submission commits and the account is a party to it. With the real-time stream ([#71](https://github.com/co-rtex/tradegrid/issues/71)), a client re-fetches the portfolio when an `exchange.changed` event's `affectedUserIds` contains the selected user.

## Proposed Go Shape

For [#64](https://github.com/co-rtex/tradegrid/issues/64) and [#65](https://github.com/co-rtex/tradegrid/issues/65), in `backend/internal/portfolio`. Names may change in review; the arithmetic may not.

```go
// Lot is one account's holding in one symbol. All fields are cents or shares.
type Lot struct {
	Quantity    int64
	CostBasis   int64
	RealizedPnL int64
}

// ApplyBuy and ApplySell return the lot after one fill and never change l.
func ApplyBuy(l Lot, quantity, price int64) (Lot, error)
func ApplySell(l Lot, quantity, price int64) (Lot, error)
```

The exchange ([#68](https://github.com/co-rtex/tradegrid/issues/68)) stores a `Lot` per account and symbol beside the existing balances, applies fills on its candidate ledger, and exposes a copied view. Valuation ([#65](https://github.com/co-rtex/tradegrid/issues/65)) takes that view, the cash snapshot, and the reference prices, and returns the response above.

## Out of Scope

- Fees, commissions, and taxes.
- Dividends, splits, and other corporate actions.
- Options, margin, borrowing, and short selling.
- Lot-level methods (FIFO, specific identification) and wash-sale rules. Average cost is the only method.
- Live, delayed, or last-trade prices; price history; and time-weighted or percentage returns.
- P&L history or charts over time.
- Persistence: a server restart resets every balance, basis, and P&L to the seed.
