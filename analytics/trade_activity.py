#!/usr/bin/env python3
"""Offline per-symbol trade-activity report for TradeGrid execution records.

The report reads one JSON file that holds TradeGrid executions (the Execution
resource in docs/api.md) and prints, for each symbol, how many executions
happened, how many shares traded, and the traded notional in integer US cents.

Accepted input files:

* An executions document, the shape returned by
  ``GET /api/v1/executions``: ``{"executions": [<Execution>, ...]}``.
  Other top-level keys, such as ``"description"``, are ignored.
* A scenario fixture from ``docs/fixtures/``. Executions are read from
  ``response.body.executions`` and from every ``after`` entry whose key starts
  with ``GET /api/v1/executions``.

Each execution record is one fill between one buy order and one sell order, so
it counts once: the buy side and the sell side are never counted separately.
The same execution can appear several times in one file (for example in the
POST response and again in the ``after`` read). Records are deduplicated by
``id``; repeated records must be identical, otherwise the file is rejected.

Money is handled with integers and ``fractions.Fraction`` only, never floats.

Standard library only; compatible with Python 3.10 and later.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from datetime import datetime
from fractions import Fraction
from typing import Any, Sequence

# Bounds from the "Numeric Bounds" section of docs/api.md.
MIN_PRICE_CENTS = 1
MAX_PRICE_CENTS = 100_000_000
MIN_QUANTITY = 1
MAX_QUANTITY = 1_000_000
MAX_SEQUENCE = 2**53 - 1

# Scenario fixtures name their expected reads "<METHOD> <path>".
EXECUTIONS_READ_PREFIX = "GET /api/v1/executions"

STRING_FIELDS = ("id", "buyOrderId", "sellOrderId", "buyerUserId", "sellerUserId")
REQUIRED_FIELDS = (
    "id",
    "sequence",
    "executedAt",
    "symbol",
    "price",
    "quantity",
    "buyOrderId",
    "sellOrderId",
    "buyerUserId",
    "sellerUserId",
)

SYMBOL_PATTERN = re.compile(r"[A-Z]+")
TIMESTAMP_PATTERN = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?Z"
)

EXIT_OK = 0
EXIT_INVALID_INPUT = 1
EXIT_USAGE = 2  # argparse's own exit status for bad arguments

COLUMNS = ("symbol", "executions", "shares", "notional_cents", "avg_price_cents")
UNITS_NOTE = (
    "Units: executions = fills (one per execution record); shares = whole shares;\n"
    "notional_cents = sum of price x quantity in integer US cents;\n"
    "avg_price_cents = notional_cents / shares in US cents per share, rounded to\n"
    "the nearest whole cent with halves rounded up."
)


class InputError(Exception):
    """The input file is unreadable, malformed, or holds invalid executions."""


@dataclass(frozen=True)
class Execution:
    """The fields of one validated execution that the report uses."""

    id: str
    sequence: int
    symbol: str
    price_cents: int
    quantity: int


@dataclass(frozen=True)
class SymbolActivity:
    """Aggregated trade activity for one symbol."""

    symbol: str
    execution_count: int
    shares: int
    notional_cents: int

    @property
    def average_price_cents(self) -> Fraction:
        """Exact quantity-weighted average execution price, in cents per share."""
        return Fraction(self.notional_cents, self.shares)

    @property
    def rounded_average_price_cents(self) -> int:
        """The average price rounded to the nearest whole cent, halves up."""
        return round_half_up(self.average_price_cents)


@dataclass(frozen=True)
class Report:
    rows: tuple[SymbolActivity, ...]
    records_read: int
    duplicates_ignored: int

    @property
    def unique_executions(self) -> int:
        return self.records_read - self.duplicates_ignored


def round_half_up(value: Fraction) -> int:
    """Round a non-negative Fraction to the nearest integer; .5 rounds up."""
    if value < 0:
        raise ValueError("round_half_up expects a non-negative value")
    return int(value + Fraction(1, 2))  # int() truncates, i.e. floors here


# ---------------------------------------------------------------------------
# Reading JSON


class _DuplicateKeyError(ValueError):
    pass


def _reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise _DuplicateKeyError(f"duplicate object key {json.dumps(key)}")
        result[key] = value
    return result


def _reject_constant(name: str) -> Any:
    raise ValueError(f"{name} is not valid JSON")


def parse_json(text: str, source: str) -> Any:
    """Parse JSON strictly: no NaN/Infinity and no repeated object keys."""
    try:
        return json.loads(
            text,
            object_pairs_hook=_reject_duplicate_keys,
            parse_constant=_reject_constant,
        )
    except json.JSONDecodeError as exc:
        raise InputError(
            f"{source}: malformed JSON at line {exc.lineno} column {exc.colno}: {exc.msg}"
        ) from None
    except ValueError as exc:
        raise InputError(f"{source}: malformed JSON: {exc}") from None


def load_json(path: str) -> Any:
    try:
        with open(path, encoding="utf-8") as handle:
            text = handle.read()
    except FileNotFoundError:
        raise InputError(f"{path}: file not found") from None
    except IsADirectoryError:
        raise InputError(f"{path}: is a directory, expected a JSON file") from None
    except UnicodeDecodeError:
        raise InputError(f"{path}: file is not valid UTF-8 text") from None
    except OSError as exc:
        raise InputError(f"{path}: cannot read file: {exc.strerror}") from None
    return parse_json(text, path)


# ---------------------------------------------------------------------------
# Finding execution lists


def find_execution_lists(document: Any, source: str) -> list[tuple[str, Any]]:
    """Return (location, raw list) pairs for every execution list in a file."""
    if not isinstance(document, dict):
        raise InputError(
            f"{source}: expected a JSON object such as "
            f'{{"executions": [...]}} or a docs/fixtures scenario, got {describe(document)}'
        )

    if "executions" in document:
        return [("executions", document["executions"])]

    if "response" not in document and "after" not in document:
        raise InputError(
            f'{source}: no execution list found; expected an "executions" array '
            'or a scenario fixture with "response" and/or "after"'
        )

    lists: list[tuple[str, Any]] = []
    response = document.get("response")
    if isinstance(response, dict):
        body = response.get("body")
        if isinstance(body, dict) and "executions" in body:
            lists.append(("response.body.executions", body["executions"]))

    after = document.get("after")
    if isinstance(after, dict):
        for key in sorted(after):
            if not (key == EXECUTIONS_READ_PREFIX or key.startswith(EXECUTIONS_READ_PREFIX + "?")):
                continue
            read = after[key]
            if not isinstance(read, dict) or "executions" not in read:
                raise InputError(
                    f'{source}: after["{key}"]: expected an object with an '
                    f'"executions" array, got {describe(read)}'
                )
            lists.append((f'after["{key}"].executions', read["executions"]))

    if not lists:
        raise InputError(
            f"{source}: scenario fixture has no execution list in "
            f'response.body.executions or after["{EXECUTIONS_READ_PREFIX}..."]'
        )
    return lists


# ---------------------------------------------------------------------------
# Validating one execution record


def describe(value: Any) -> str:
    """Name a JSON value's type (and short value) for error messages."""
    if value is None:
        return "null"
    if isinstance(value, bool):
        return f"boolean {'true' if value else 'false'}"
    if isinstance(value, int):
        return f"integer {value}"
    if isinstance(value, float):
        return f"number {value!r}"
    if isinstance(value, str):
        shown = value if len(value) <= 40 else value[:37] + "..."
        return f"string {json.dumps(shown)}"
    if isinstance(value, list):
        return "array"
    if isinstance(value, dict):
        return "object"
    return type(value).__name__


def _check_bounded_int(
    value: Any, field: str, low: int, high: int, unit: str = ""
) -> str | None:
    # bool is a subclass of int in Python; JSON true/false are not numbers here.
    if type(value) is not int:
        kind = f"an integer number of {unit}" if unit else "an integer"
        return f'"{field}" must be {kind}, got {describe(value)}'
    if not low <= value <= high:
        units = f" {unit}" if unit else ""
        return f'"{field}" must be between {low} and {high}{units}, got {value}'
    return None


def _check_timestamp(value: Any) -> str | None:
    problem = (
        '"executedAt" must be an RFC 3339 UTC timestamp such as '
        f'"2026-10-20T15:00:02Z", got {describe(value)}'
    )
    if not isinstance(value, str):
        return problem
    match = TIMESTAMP_PATTERN.fullmatch(value)
    if match is None:
        return problem
    try:
        datetime(*(int(part) for part in match.groups()[:6]))
    except ValueError:
        return problem
    return None


def canonical(record: dict) -> str:
    """Return record as JSON text with sorted keys, for exact comparison."""
    return json.dumps(record, sort_keys=True)


def execution_problems(record: Any) -> list[str]:
    """Return every problem with one raw execution record (empty if valid)."""
    if not isinstance(record, dict):
        return [f"execution must be an object, got {describe(record)}"]

    problems = [
        f'missing required field "{field}"' for field in REQUIRED_FIELDS if field not in record
    ]

    for field in STRING_FIELDS:
        if field in record:
            value = record[field]
            if not isinstance(value, str) or value == "":
                problems.append(f'"{field}" must be a non-empty string, got {describe(value)}')

    if "sequence" in record:
        problem = _check_bounded_int(record["sequence"], "sequence", 1, MAX_SEQUENCE)
        if problem:
            problems.append(problem)
    if "executedAt" in record:
        problem = _check_timestamp(record["executedAt"])
        if problem:
            problems.append(problem)
    if "symbol" in record:
        symbol = record["symbol"]
        if not isinstance(symbol, str) or SYMBOL_PATTERN.fullmatch(symbol) is None:
            problems.append(
                f'"symbol" must be uppercase letters such as "AAPL", got {describe(symbol)}'
            )
    if "price" in record:
        problem = _check_bounded_int(
            record["price"], "price", MIN_PRICE_CENTS, MAX_PRICE_CENTS, "cents"
        )
        if problem:
            problems.append(problem)
    if "quantity" in record:
        problem = _check_bounded_int(
            record["quantity"], "quantity", MIN_QUANTITY, MAX_QUANTITY, "shares"
        )
        if problem:
            problems.append(problem)

    buy_order, sell_order = record.get("buyOrderId"), record.get("sellOrderId")
    if isinstance(buy_order, str) and buy_order and buy_order == sell_order:
        problems.append(
            f'"buyOrderId" and "sellOrderId" must differ, both are {json.dumps(buy_order)}'
        )

    return problems


def to_execution(record: dict[str, Any]) -> Execution:
    return Execution(
        id=record["id"],
        sequence=record["sequence"],
        symbol=record["symbol"],
        price_cents=record["price"],
        quantity=record["quantity"],
    )


# ---------------------------------------------------------------------------
# Collecting, deduplicating, and summarizing


def collect_executions(document: Any, source: str) -> tuple[list[Execution], int]:
    """Validate and deduplicate every execution in a document.

    Returns the unique executions (first-seen order) and the number of raw
    records read. Raises InputError listing every invalid record.
    """
    problems: list[str] = []
    seen_records: dict[str, dict[str, Any]] = {}
    seen_locations: dict[str, str] = {}
    id_by_sequence: dict[int, str] = {}
    unique: list[Execution] = []
    records_read = 0

    for location, raw_list in find_execution_lists(document, source):
        if not isinstance(raw_list, list):
            problems.append(f"{location}: must be an array, got {describe(raw_list)}")
            continue
        for index, record in enumerate(raw_list):
            records_read += 1
            where = f"{location}[{index}]"
            record_problems = execution_problems(record)
            if record_problems:
                problems.extend(f"{where}: {problem}" for problem in record_problems)
                continue

            execution_id = record["id"]
            if execution_id in seen_records:
                # Compare canonical JSON: Python's == treats true, 1, and 1.0
                # as equal, but they are different JSON values.
                if canonical(record) != canonical(seen_records[execution_id]):
                    problems.append(
                        f"{where}: execution {json.dumps(execution_id)} differs from the record "
                        f"with the same id at {seen_locations[execution_id]}"
                    )
                continue  # the same fill seen again: count it once

            sequence = record["sequence"]
            if sequence in id_by_sequence:
                problems.append(
                    f"{where}: sequence {sequence} is used by both "
                    f"{json.dumps(id_by_sequence[sequence])} and {json.dumps(execution_id)}"
                )
                continue

            seen_records[execution_id] = record
            seen_locations[execution_id] = where
            id_by_sequence[sequence] = execution_id
            unique.append(to_execution(record))

    if problems:
        count = len(problems)
        noun = "problem" if count == 1 else "problems"
        details = "".join(f"\n  {problem}" for problem in problems)
        raise InputError(f"{source}: {count} {noun} in execution records:{details}")
    return unique, records_read


def summarize(executions: Sequence[Execution]) -> tuple[SymbolActivity, ...]:
    """Aggregate unique executions into per-symbol rows sorted by symbol."""
    totals: dict[str, list[int]] = {}
    for execution in executions:
        row = totals.setdefault(execution.symbol, [0, 0, 0])
        row[0] += 1  # one execution is one fill, whichever side you look from
        row[1] += execution.quantity
        row[2] += execution.price_cents * execution.quantity
    return tuple(
        SymbolActivity(symbol, count, shares, notional)
        for symbol, (count, shares, notional) in sorted(totals.items())
    )


def build_report(document: Any, source: str) -> Report:
    executions, records_read = collect_executions(document, source)
    return Report(
        rows=summarize(executions),
        records_read=records_read,
        duplicates_ignored=records_read - len(executions),
    )


# ---------------------------------------------------------------------------
# Output


def format_report(report: Report, source: str) -> str:
    lines = [
        "TradeGrid trade activity",
        f"source: {source}",
        (
            f"execution records read: {report.records_read}; "
            f"unique executions: {report.unique_executions}; "
            f"duplicate records ignored: {report.duplicates_ignored}"
        ),
        "",
    ]
    if not report.rows:
        lines.append("No executions found.")
        return "\n".join(lines) + "\n"

    table = [COLUMNS] + [
        (
            row.symbol,
            str(row.execution_count),
            str(row.shares),
            str(row.notional_cents),
            str(row.rounded_average_price_cents),
        )
        for row in report.rows
    ]
    widths = [max(len(cells[i]) for cells in table) for i in range(len(COLUMNS))]

    def render(cells: Sequence[str]) -> str:
        first = cells[0].ljust(widths[0])
        rest = (cell.rjust(width) for cell, width in zip(cells[1:], widths[1:]))
        return "  ".join([first, *rest])

    lines.append(render(table[0]))
    lines.append(render(["-" * width for width in widths]))
    lines.extend(render(cells) for cells in table[1:])
    lines.append("")
    lines.append(UNITS_NOTE)
    return "\n".join(lines) + "\n"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="trade_activity.py",
        description=(
            "Print per-symbol trade activity (execution count, traded shares, "
            "traded notional in integer cents) from a TradeGrid executions JSON file."
        ),
        epilog=(
            'INPUT is either {"executions": [...]} (the GET /api/v1/executions shape) '
            "or a docs/fixtures scenario file. Exit status: 0 report printed, "
            "1 unreadable or invalid input, 2 bad command-line arguments."
        ),
    )
    parser.add_argument("input", metavar="INPUT", help="path to the JSON file to read")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        report = build_report(load_json(args.input), args.input)
    except InputError as exc:
        print(f"trade_activity: error: {exc}", file=sys.stderr)
        return EXIT_INVALID_INPUT
    sys.stdout.write(format_report(report, args.input))
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
