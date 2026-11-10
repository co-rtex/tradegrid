"""Tests for trade_activity.py. Run from the repository root with:

    python3 -m unittest discover -s analytics -v
"""

from __future__ import annotations

import contextlib
import copy
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from fractions import Fraction
from pathlib import Path

import trade_activity as ta

ANALYTICS_DIR = Path(__file__).resolve().parent
REPO_ROOT = ANALYTICS_DIR.parent
FIXTURES_DIR = REPO_ROOT / "docs" / "fixtures"
SYNTHETIC_FIXTURE = ANALYTICS_DIR / "synthetic-executions.json"
SCRIPT = ANALYTICS_DIR / "trade_activity.py"


def execution(n: int, symbol: str = "AAPL", price: int = 19000, quantity: int = 10, **overrides):
    """A valid execution record; override any field by keyword."""
    record = {
        "id": f"exe-{n}",
        "sequence": n,
        "executedAt": "2026-10-20T15:00:02Z",
        "symbol": symbol,
        "price": price,
        "quantity": quantity,
        "buyOrderId": f"ord-{2 * n}",
        "sellOrderId": f"ord-{2 * n - 1}",
        "buyerUserId": "bob",
        "sellerUserId": "alice",
    }
    record.update(overrides)
    return record


def doc(*records):
    return {"executions": list(records)}


def report_for(document) -> ta.Report:
    return ta.build_report(document, "test.json")


def rows_as_tuples(report: ta.Report):
    return [
        (row.symbol, row.execution_count, row.shares, row.notional_cents) for row in report.rows
    ]


def run_main(*argv: str) -> tuple[int, str, str]:
    stdout, stderr = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
        try:
            code = ta.main(list(argv))
        except SystemExit as exc:  # argparse exits on usage errors
            code = exc.code
    return code, stdout.getvalue(), stderr.getvalue()


class TempFileMixin:
    def setUp(self):
        super().setUp()
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)

    def write(self, name: str, content) -> str:
        path = Path(self._tmp.name) / name
        if isinstance(content, bytes):
            path.write_bytes(content)
        elif isinstance(content, str):
            path.write_text(content, encoding="utf-8")
        else:
            path.write_text(json.dumps(content), encoding="utf-8")
        return str(path)


class SummaryTests(unittest.TestCase):
    def test_empty_input_has_no_rows(self):
        report = report_for(doc())
        self.assertEqual(report.rows, ())
        self.assertEqual(report.records_read, 0)
        self.assertEqual(report.unique_executions, 0)

    def test_multiple_symbols_are_sorted_by_symbol_whatever_the_input_order(self):
        records = [
            execution(1, "MSFT", 41000, 3),
            execution(2, "AAPL", 19000, 10),
            execution(3, "MSFT", 41015, 4),
        ]
        expected = [("AAPL", 1, 10, 190000), ("MSFT", 2, 7, 287060)]
        self.assertEqual(rows_as_tuples(report_for(doc(*records))), expected)
        self.assertEqual(rows_as_tuples(report_for(doc(*reversed(records)))), expected)

    def test_partial_fills_in_separate_records_are_separate_executions(self):
        # One resting sell of 25 filled by three different buys.
        records = [
            execution(1, quantity=10, sellOrderId="ord-1", buyOrderId="ord-2"),
            execution(2, quantity=7, sellOrderId="ord-1", buyOrderId="ord-3"),
            execution(3, quantity=8, price=19050, sellOrderId="ord-1", buyOrderId="ord-4"),
        ]
        report = report_for(doc(*records))
        self.assertEqual(rows_as_tuples(report), [("AAPL", 3, 25, 17 * 19000 + 8 * 19050)])

    def test_each_execution_counts_once_not_once_per_side(self):
        # A buyer and a seller both take part in every fill; the report counts
        # the fill, not the participants.
        report = report_for(doc(execution(1, quantity=10), execution(2, quantity=5)))
        (row,) = report.rows
        self.assertEqual(row.execution_count, 2)
        self.assertEqual(row.shares, 15)
        self.assertEqual(row.notional_cents, 15 * 19000)

    def test_same_account_trade_counts_once(self):
        record = execution(1, buyerUserId="alice", sellerUserId="alice")
        self.assertEqual(rows_as_tuples(report_for(doc(record))), [("AAPL", 1, 10, 190000)])

    def test_money_is_exact_integer_cents_beyond_float_precision(self):
        # 101 near-maximal fills: the odd total exceeds 2**53, so a float cannot
        # hold it and float arithmetic would silently lose cents.
        price, quantity = ta.MAX_PRICE_CENTS - 1, ta.MAX_QUANTITY - 1
        records = [execution(n, price=price, quantity=quantity) for n in range(1, 102)]
        (row,) = report_for(doc(*records)).rows
        self.assertIs(type(row.notional_cents), int)
        self.assertEqual(row.notional_cents, 10099989799000101)
        self.assertNotEqual(float(row.notional_cents), row.notional_cents)
        self.assertEqual(row.average_price_cents, Fraction(price))
        self.assertIs(type(row.rounded_average_price_cents), int)

    def test_average_price_is_exact_and_rounds_half_up(self):
        cases = [
            # (price, quantity) pairs -> exact average, rounded average
            ([(100, 1), (101, 1)], Fraction(201, 2), 101),  # exactly half: up
            ([(102, 1), (103, 1)], Fraction(205, 2), 103),  # half-even would give 102
            ([(100, 2), (101, 1)], Fraction(301, 3), 100),  # 100.33 -> down
            ([(100, 1), (101, 2)], Fraction(302, 3), 101),  # 100.67 -> up
            ([(19000, 10)], Fraction(19000), 19000),
        ]
        for fills, exact, rounded in cases:
            with self.subTest(fills=fills):
                records = [execution(n, price=p, quantity=q) for n, (p, q) in enumerate(fills, 1)]
                (row,) = report_for(doc(*records)).rows
                self.assertEqual(row.average_price_cents, exact)
                self.assertEqual(row.rounded_average_price_cents, rounded)

    def test_round_half_up(self):
        self.assertEqual(ta.round_half_up(Fraction(5, 2)), 3)
        self.assertEqual(ta.round_half_up(Fraction(7, 2)), 4)
        self.assertEqual(ta.round_half_up(Fraction(249, 100)), 2)
        self.assertEqual(ta.round_half_up(Fraction(0)), 0)
        with self.assertRaises(ValueError):
            ta.round_half_up(Fraction(-1, 2))


class DuplicateCountingTests(unittest.TestCase):
    def test_same_execution_in_response_and_after_counts_once(self):
        record = execution(1)
        scenario = {
            "response": {"status": 201, "body": {"order": {}, "executions": [record]}},
            "after": {
                "GET /api/v1/executions?symbol=AAPL": {"executions": [copy.deepcopy(record)]},
                "GET /api/v1/executions?symbol=AAPL&userId=bob": {"executions": [record]},
                "GET /api/v1/executions?symbol=AAPL&userId=alice": {"executions": [record]},
            },
        }
        report = report_for(scenario)
        self.assertEqual(report.records_read, 4)
        self.assertEqual(report.duplicates_ignored, 3)
        self.assertEqual(rows_as_tuples(report), [("AAPL", 1, 10, 190000)])

    def test_repeated_record_in_one_list_counts_once(self):
        report = report_for(doc(execution(1), execution(1), execution(2)))
        self.assertEqual(report.duplicates_ignored, 1)
        self.assertEqual(rows_as_tuples(report), [("AAPL", 2, 20, 380000)])

    def test_conflicting_records_with_the_same_id_are_rejected(self):
        with self.assertRaises(ta.InputError) as ctx:
            report_for(doc(execution(1, quantity=10), execution(1, quantity=11)))
        self.assertIn('execution "exe-1" differs from the record', str(ctx.exception))
        self.assertIn("executions[0]", str(ctx.exception))

    def test_two_ids_with_the_same_sequence_are_rejected(self):
        with self.assertRaises(ta.InputError) as ctx:
            report_for(doc(execution(1), execution(2, sequence=1)))
        self.assertIn('sequence 1 is used by both "exe-1" and "exe-2"', str(ctx.exception))

    def test_checked_in_fill_fixtures_report_one_execution(self):
        for name in ("02-full-fill.json", "03-partial-fill.json"):
            with self.subTest(fixture=name):
                path = FIXTURES_DIR / name
                report = ta.build_report(ta.load_json(str(path)), name)
                self.assertEqual(report.records_read, 2)
                self.assertEqual(report.duplicates_ignored, 1)
                self.assertEqual(rows_as_tuples(report), [("AAPL", 1, 10, 190000)])

    def test_checked_in_fixtures_without_fills_are_empty(self):
        for name in ("00-seed.json", "01-no-fill.json", "04-risk-rejection.json"):
            with self.subTest(fixture=name):
                path = FIXTURES_DIR / name
                report = ta.build_report(ta.load_json(str(path)), name)
                self.assertEqual(report.rows, ())

    def test_every_checked_in_fixture_is_accepted(self):
        paths = sorted(FIXTURES_DIR.glob("*.json"))
        self.assertGreaterEqual(len(paths), 5)
        for path in paths:
            with self.subTest(fixture=path.name):
                ta.build_report(ta.load_json(str(path)), path.name)


class ValidationTests(unittest.TestCase):
    def assert_rejected(self, record, *expected_fragments):
        with self.assertRaises(ta.InputError) as ctx:
            report_for(doc(record))
        message = str(ctx.exception)
        self.assertIn("executions[0]", message)
        for fragment in expected_fragments:
            self.assertIn(fragment, message)

    def test_valid_record_is_accepted(self):
        self.assertEqual(ta.execution_problems(execution(1)), [])

    def test_unknown_fields_are_ignored(self):
        self.assertEqual(ta.execution_problems(execution(1, venue="demo")), [])

    def test_fractional_seconds_are_accepted(self):
        record = execution(1, executedAt="2026-10-20T15:00:02.123456789Z")
        self.assertEqual(ta.execution_problems(record), [])

    def test_missing_required_fields(self):
        for field in ta.REQUIRED_FIELDS:
            with self.subTest(field=field):
                record = execution(1)
                del record[field]
                self.assert_rejected(record, f'missing required field "{field}"')

    def test_price_must_be_integer_cents(self):
        cases = [
            (190.5, 'got number 190.5'),
            (19000.0, "got number 19000.0"),
            ("19000", 'got string "19000"'),
            (True, "got boolean true"),
            (None, "got null"),
        ]
        for value, fragment in cases:
            with self.subTest(price=value):
                self.assert_rejected(
                    execution(1, price=value),
                    '"price" must be an integer number of cents',
                    fragment,
                )

    def test_price_bounds(self):
        for value in (0, -1, ta.MAX_PRICE_CENTS + 1):
            with self.subTest(price=value):
                self.assert_rejected(
                    execution(1, price=value), '"price" must be between 1 and 100000000 cents'
                )
        for value in (ta.MIN_PRICE_CENTS, ta.MAX_PRICE_CENTS):
            with self.subTest(price=value):
                self.assertEqual(ta.execution_problems(execution(1, price=value)), [])

    def test_quantity_must_be_positive_integer_shares(self):
        for value in (0, -5, ta.MAX_QUANTITY + 1):
            with self.subTest(quantity=value):
                self.assert_rejected(
                    execution(1, quantity=value), '"quantity" must be between 1 and 1000000 shares'
                )
        for value in (2.5, "10", False, [10]):
            with self.subTest(quantity=value):
                self.assert_rejected(
                    execution(1, quantity=value), '"quantity" must be an integer number of shares'
                )

    def test_sequence_must_be_positive_integer(self):
        for value in (0, -1, 1.0, "1", True, 2**53):
            with self.subTest(sequence=value):
                self.assert_rejected(execution(1, sequence=value), '"sequence" must be')

    def test_symbol_must_be_uppercase_letters(self):
        for value in ("aapl", "", "AAPL ", 7, None):
            with self.subTest(symbol=value):
                self.assert_rejected(execution(1, symbol=value), '"symbol" must be uppercase')

    def test_identifier_fields_must_be_non_empty_strings(self):
        for field in ta.STRING_FIELDS:
            for value in ("", 1, None):
                with self.subTest(field=field, value=value):
                    self.assert_rejected(
                        execution(1, **{field: value}), f'"{field}" must be a non-empty string'
                    )

    def test_executed_at_must_be_rfc3339_utc(self):
        for value in (
            "2026-10-20 15:00:02Z",
            "2026-10-20T15:00:02+00:00",
            "2026-10-20T15:00:02",
            "2026-13-01T15:00:02Z",
            "2026-02-30T15:00:02Z",
            1760972402,
        ):
            with self.subTest(executedAt=value):
                self.assert_rejected(execution(1, executedAt=value), '"executedAt" must be')

    def test_buy_and_sell_order_must_differ(self):
        self.assert_rejected(
            execution(1, buyOrderId="ord-1", sellOrderId="ord-1"),
            '"buyOrderId" and "sellOrderId" must differ',
        )

    def test_record_must_be_an_object(self):
        for value in (5, "exe-1", None, [execution(1)]):
            with self.subTest(record=value):
                self.assert_rejected(value, "execution must be an object")

    def test_every_invalid_record_is_reported(self):
        records = [execution(1, price=-1), execution(2), execution(3, quantity=0.5)]
        with self.assertRaises(ta.InputError) as ctx:
            report_for(doc(*records))
        message = str(ctx.exception)
        self.assertIn("2 problems", message)
        self.assertIn('executions[0]: "price"', message)
        self.assertIn('executions[2]: "quantity"', message)
        self.assertNotIn("executions[1]", message)

    def test_a_bad_record_fails_the_whole_report(self):
        # No partial report: one invalid record means no rows at all.
        with self.assertRaises(ta.InputError):
            report_for(doc(execution(1), execution(2, price=0)))


class DocumentShapeTests(unittest.TestCase):
    def test_executions_document_ignores_other_top_level_keys(self):
        report = report_for({"description": "x", "executions": [execution(1)]})
        self.assertEqual(rows_as_tuples(report), [("AAPL", 1, 10, 190000)])

    def test_scenario_ignores_non_execution_reads(self):
        scenario = {
            "response": {"status": 201, "body": {"executions": []}},
            "after": {
                "GET /api/v1/orders?userId=alice": {"orders": [{"price": "not checked"}]},
                "GET /api/v1/executions?symbol=AAPL": {"executions": [execution(1)]},
            },
        }
        self.assertEqual(rows_as_tuples(report_for(scenario)), [("AAPL", 1, 10, 190000)])

    def test_error_response_without_executions_is_allowed(self):
        scenario = {
            "response": {"status": 422, "body": {"error": {"code": "INSUFFICIENT_CASH"}}},
            "after": {"GET /api/v1/executions?symbol=AAPL": {"executions": []}},
        }
        self.assertEqual(report_for(scenario).rows, ())

    def test_rejected_shapes(self):
        cases = [
            ([execution(1)], "expected a JSON object"),
            ("executions", "expected a JSON object"),
            ({"orders": []}, "no execution list found"),
            ({"executions": None}, "executions: must be an array, got null"),
            ({"executions": {"exe-1": execution(1)}}, "executions: must be an array, got object"),
            ({"response": {"status": 422, "body": {}}}, "scenario fixture has no execution list"),
            (
                {"after": {"GET /api/v1/executions?symbol=AAPL": []}},
                'after["GET /api/v1/executions?symbol=AAPL"]: expected an object',
            ),
        ]
        for document, fragment in cases:
            with self.subTest(document=document):
                with self.assertRaises(ta.InputError) as ctx:
                    report_for(document)
                self.assertIn(fragment, str(ctx.exception))


class LoadJsonTests(TempFileMixin, unittest.TestCase):
    def assert_load_fails(self, path: str, fragment: str):
        with self.assertRaises(ta.InputError) as ctx:
            ta.load_json(path)
        self.assertIn(fragment, str(ctx.exception))
        self.assertIn(path, str(ctx.exception))

    def test_malformed_json_reports_line_and_column(self):
        path = self.write("bad.json", '{\n  "executions": [\n    {"id": "exe-1",}\n  ]\n}\n')
        self.assert_load_fails(path, "malformed JSON at line 3 column")

    def test_truncated_and_empty_files_are_malformed(self):
        for content in ('{"executions": [', ""):
            with self.subTest(content=content):
                self.assert_load_fails(self.write("bad.json", content), "malformed JSON")

    def test_nan_and_infinity_are_rejected(self):
        for constant in ("NaN", "Infinity", "-Infinity"):
            with self.subTest(constant=constant):
                path = self.write("bad.json", '{"executions": [{"price": %s}]}' % constant)
                self.assert_load_fails(path, f"{constant} is not valid JSON")

    def test_duplicate_object_keys_are_rejected(self):
        path = self.write("dup.json", '{"executions": [{"price": 1, "price": 2}]}')
        self.assert_load_fails(path, 'duplicate object key "price"')

    def test_missing_file(self):
        self.assert_load_fails(str(Path(self._tmp.name) / "missing.json"), "file not found")

    def test_directory(self):
        self.assert_load_fails(self._tmp.name, "is a directory")

    def test_non_utf8_file(self):
        self.assert_load_fails(self.write("latin1.json", b'{"executions": ["\xe9"]}'), "UTF-8")


class CommandLineTests(TempFileMixin, unittest.TestCase):
    maxDiff = None

    def test_synthetic_fixture_report(self):
        code, out, err = run_main(str(SYNTHETIC_FIXTURE))
        self.assertEqual(code, 0)
        self.assertEqual(err, "")
        expected = (
            "TradeGrid trade activity\n"
            f"source: {SYNTHETIC_FIXTURE}\n"
            "execution records read: 6; unique executions: 6; duplicate records ignored: 0\n"
            "\n"
            "symbol  executions  shares  notional_cents  avg_price_cents\n"
            "------  ----------  ------  --------------  ---------------\n"
            "AAPL             4      24          455750            18990\n"
            "MSFT             2       7          287060            41009\n"
            "\n" + ta.UNITS_NOTE + "\n"
        )
        self.assertEqual(out, expected)

    def test_output_is_deterministic(self):
        first = run_main(str(SYNTHETIC_FIXTURE))
        reordered = json.loads(SYNTHETIC_FIXTURE.read_text(encoding="utf-8"))
        reordered["executions"].reverse()
        path = self.write("reordered.json", reordered)
        second = run_main(path)
        self.assertEqual(first[0], second[0])
        self.assertEqual(
            first[1].replace(str(SYNTHETIC_FIXTURE), "SRC"), second[1].replace(path, "SRC")
        )

    def test_real_partial_fill_fixture(self):
        code, out, err = run_main(str(FIXTURES_DIR / "03-partial-fill.json"))
        self.assertEqual((code, err), (0, ""))
        self.assertIn(
            "execution records read: 2; unique executions: 1; duplicate records ignored: 1", out
        )
        self.assertIn("AAPL             1      10          190000            19000\n", out)

    def test_empty_input(self):
        code, out, err = run_main(self.write("empty.json", {"executions": []}))
        self.assertEqual((code, err), (0, ""))
        self.assertTrue(out.endswith("duplicate records ignored: 0\n\nNo executions found.\n"))

    def test_invalid_record_exits_1_with_message_on_stderr(self):
        path = self.write("bad.json", doc(execution(1, price=19000.5)))
        code, out, err = run_main(path)
        self.assertEqual(code, 1)
        self.assertEqual(out, "")
        self.assertTrue(err.startswith(f"trade_activity: error: {path}: 1 problem"))
        self.assertIn('"price" must be an integer number of cents, got number 19000.5', err)

    def test_malformed_json_exits_1(self):
        code, out, err = run_main(self.write("bad.json", "{not json"))
        self.assertEqual((code, out), (1, ""))
        self.assertIn("malformed JSON at line 1 column 2", err)

    def test_missing_file_exits_1(self):
        code, out, err = run_main("does-not-exist.json")
        self.assertEqual((code, out), (1, ""))
        self.assertIn("does-not-exist.json: file not found", err)

    def test_missing_argument_exits_2(self):
        code, out, err = run_main()
        self.assertEqual((code, out), (2, ""))
        self.assertIn("usage:", err)

    def test_script_runs_as_a_subprocess(self):
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
        ok = subprocess.run(
            [sys.executable, str(SCRIPT), str(SYNTHETIC_FIXTURE)],
            capture_output=True, text=True, env=env, check=False,
        )
        self.assertEqual((ok.returncode, ok.stderr), (0, ""))
        self.assertIn("MSFT             2       7          287060            41009", ok.stdout)

        bad = subprocess.run(
            [sys.executable, str(SCRIPT), self.write("bad.json", '{"executions": [1]}')],
            capture_output=True, text=True, env=env, check=False,
        )
        self.assertEqual((bad.returncode, bad.stdout), (1, ""))
        self.assertIn("execution must be an object, got integer 1", bad.stderr)


if __name__ == "__main__":
    unittest.main()
