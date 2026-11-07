# TradeGrid Analytics

This directory is reserved for analytics and quantitative work that complements the core exchange.

Possible future work:
- Returns and volatility
- Portfolio metrics
- Market microstructure statistics
- Strategy experiments
- Risk analytics

Analytics work is secondary to shipping the semester MVP.

## Trade activity report

`trade_activity.py` is an offline command-line report ([issue #76](https://github.com/co-rtex/tradegrid/issues/76)). It reads execution records from a JSON file and prints, for each symbol, how many executions happened, how many shares traded, and the traded notional in integer US cents. It does not call the backend API, fetch market data, make predictions, or compute P&L.

### Requirements

Python 3.10 or later, standard library only. There is nothing to install: no virtual environment, no `pip install`, and no third-party packages.

### Run it

From the repository root:

```sh
python3 analytics/trade_activity.py analytics/synthetic-executions.json
```

Output:

```text
TradeGrid trade activity
source: analytics/synthetic-executions.json
execution records read: 6; unique executions: 6; duplicate records ignored: 0

symbol  executions  shares  notional_cents  avg_price_cents
------  ----------  ------  --------------  ---------------
AAPL             4      24          455750            18990
MSFT             2       7          287060            41009

Units: executions = fills (one per execution record); shares = whole shares;
notional_cents = sum of price x quantity in integer US cents;
avg_price_cents = notional_cents / shares in US cents per share, rounded to
the nearest whole cent with halves rounded up.
```

The checked-in scenario fixtures work too:

```sh
python3 analytics/trade_activity.py docs/fixtures/03-partial-fill.json
```

That prints one AAPL execution of 10 shares, `190000` cents notional. The file holds the same execution twice (in the POST response and in the `after` executions read), so the summary line reports `execution records read: 2; unique executions: 1; duplicate records ignored: 1`. `00-seed.json`, `01-no-fill.json`, and `04-risk-rejection.json` have no executions and print `No executions found.`

Run `python3 analytics/trade_activity.py --help` for the short form of this section.

### Input

The command takes exactly one JSON file in either of two shapes:

1. **An executions document**, the response shape of `GET /api/v1/executions` in [docs/api.md](../docs/api.md): `{"executions": [<Execution>, ...]}`. Other top-level keys, such as `"description"`, are ignored.
2. **A scenario fixture** from [`docs/fixtures/`](../docs/fixtures/). Executions are read from `response.body.executions` and from every `after` entry whose key starts with `GET /api/v1/executions`. Other `after` reads (accounts, orders, order book) are ignored.

A file is treated as an executions document if it has a top-level `"executions"` key, and as a scenario fixture if it has `"response"` or `"after"` instead. Anything else is rejected.

The checked-in fixtures each contain at most one execution, all for AAPL, so [`synthetic-executions.json`](synthetic-executions.json) provides a richer example in the executions-document shape. It is **synthetic**: written by hand to follow the API contract, not captured from a running server. It has six executions for the seeded `alice` and `bob` accounts, listed newest first like the API:

- Alice's sell `ord-1` (25 AAPL at $190.00) fills in three separate executions (`exe-1`, `exe-3`, `exe-6`): partial fills are separate records.
- Bob's buy `ord-4` sweeps two price levels (`exe-2` at $189.50, `exe-3` at $190.00).
- `exe-6` is a same-account trade (Alice buys from her own resting sell). It is still one execution.
- MSFT has two executions at different prices, so its average price is not a whole number of cents and shows the rounding rule.

Only one file is read per run. Execution IDs are unique within one exchange run, but every fixture starts from a fresh server, so `exe-1` in two different fixture files are two different fills. Combining files would make IDs ambiguous.

### What it measures

Each execution record is one fill: a quantity of shares that changed hands between one buy order and one sell order at one price. Accepted orders that did not fill, and the unfilled part of a partially filled order, are not trades and do not appear.

| Column | Meaning | Unit |
| --- | --- | --- |
| `symbol` | Ticker, rows sorted by symbol (plain character order) | — |
| `executions` | Number of unique execution records for the symbol | fills |
| `shares` | Sum of `quantity` | whole shares |
| `notional_cents` | Sum of `price × quantity` | integer US cents (`190000` = $1,900.00) |
| `avg_price_cents` | Quantity-weighted average execution price, `notional_cents / shares` | US cents per share, rounded as below |

**One execution counts once.** Every execution has a buyer and a seller. The report counts the execution, not its two sides, so a 10-share fill is 1 execution and 10 shares, never 2 executions or 20 shares. This also holds when the buyer and seller are the same account.

**Duplicates are counted once.** The same execution can appear more than once in one file. A scenario fixture lists it in the POST response and again in the `after` executions read, and reads filtered by `userId` would list a fill once for the buyer and once for the seller. Records are deduplicated by `id`. A repeated record must be identical to the first one; a different record with the same `id`, or two IDs with the same `sequence`, is an error. The summary line shows how many records were read and how many repeats were ignored.

### Exact money and rounding

Prices are integer cents and quantities are integer shares, as in the API. Notional is computed as an exact Python integer, so it never loses a cent, even beyond the `2^53` limit where floating-point numbers become inexact. No float is used anywhere in the calculation.

The average price is computed exactly as a `fractions.Fraction`, `notional_cents / shares`. For display it is rounded to the nearest whole cent, and an exact half cent rounds up (away from zero; all values are positive). It is not banker's rounding: 102.5 cents shows as 103. Example: MSFT in the synthetic file is `287060 / 7 = 41008.571...` cents, shown as `41009`. The rounded value is for display only; nothing else is computed from it.

### Validation and errors

Every record must have all fields of the Execution resource in [docs/api.md](../docs/api.md), with these checks (unknown extra fields are ignored):

| Field | Rule |
| --- | --- |
| `id`, `buyOrderId`, `sellOrderId`, `buyerUserId`, `sellerUserId` | non-empty strings; `buyOrderId` and `sellOrderId` must differ |
| `sequence` | integer from `1` to `9007199254740991` |
| `executedAt` | RFC 3339 UTC timestamp ending in `Z`, for example `2026-10-20T15:00:02Z`; fractional seconds allowed |
| `symbol` | uppercase letters only, for example `AAPL` (not limited to the two Sprint 2 symbols) |
| `price` | integer cents from `1` to `100000000` |
| `quantity` | integer shares from `1` to `1000000` |

"Integer" means a JSON integer: `190.5`, `19000.0`, `"19000"`, `true`, and `null` are all rejected. The JSON itself must be well formed, without `NaN`/`Infinity` or repeated keys in one object.

Bad input prints no report. The command lists every problem found, with its location, on standard error, for example:

```text
trade_activity: error: bad.json: 2 problems in execution records:
  executions[0]: "price" must be an integer number of cents, got number 190.5
  executions[3]: missing required field "quantity"
```

Exit status: `0` when the report is printed (including an empty one), `1` for an unreadable file, malformed JSON, or invalid records, and `2` for wrong command-line arguments.

### Tests

From the repository root:

```sh
python3 -m unittest discover -s analytics -v
```

The tests use only the standard library `unittest` module. They cover empty input, multiple symbols, partial fills, exact money and rounding, duplicate-counting avoidance, malformed JSON and records, every checked-in fixture, and the command's output and exit codes. CI does not run them yet.
