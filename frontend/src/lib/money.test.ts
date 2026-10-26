
import { describe, expect, it } from "vitest";

import {
  MAX_ORDER_PRICE_CENTS,
  MoneyInputError,
  formatDisplayTime,
  formatUsdCents,
  formatUtcTime,
  formatWholeShares,
  parseUsdToCents,
} from "./money";

describe("parseUsdToCents", () => {
  it.each([
    ["0.01", 1],
    ["0.1", 10],
    ["1", 100],
    ["10", 1000],
    ["10.0", 1000],
    ["10.25", 1025],
    ["10.00", 1000],
    ["99.99", 9999],
    ["007", 700],
    [" 10.25 ", 1025],
    ["999999.99", 99999999],
    ["1000000.00", MAX_ORDER_PRICE_CENTS],
  ])("converts %s into %i cents", (input, expected) => {
    expect(parseUsdToCents(input)).toBe(expected);
  });

  it.each([
    ["", "BLANK"],
    ["   ", "BLANK"],
    ["abc", "FORMAT"],
    [".5", "FORMAT"],
    ["5.", "FORMAT"],
    ["1,000", "FORMAT"],
    ["+1", "FORMAT"],
    ["-1", "FORMAT"],
    ["-0.01", "FORMAT"],
    ["1e2", "FORMAT"],
    ["1E2", "FORMAT"],
    ["10.256", "PRECISION"],
    ["1.005", "PRECISION"],
    ["0.001", "PRECISION"],
    ["0", "RANGE"],
    ["0.00", "RANGE"],
    ["1000000.01", "RANGE"],
    ["9007199254740992", "RANGE"],
    ["9".repeat(1000), "RANGE"],
  ] as const)(
    "rejects %s with error %s",
    (input, expectedCode) => {
      let caught: unknown;

      try {
        parseUsdToCents(input);
      } catch (error) {
        caught = error;
      }

      expect(caught).toBeInstanceOf(MoneyInputError);
      expect((caught as MoneyInputError).code).toBe(expectedCode);
    },
  );

  it("accepts the maximum contracted price", () => {
    expect(parseUsdToCents("1000000.00")).toBe(
      MAX_ORDER_PRICE_CENTS,
    );
  });

  it("rejects one cent above the maximum price", () => {
    expect(() => parseUsdToCents("1000000.01")).toThrow(
      MoneyInputError,
    );
  });

  it("does not silently round fractional cents", () => {
    expect(() => parseUsdToCents("10.999")).toThrow(
      MoneyInputError,
    );
  });

  it("returns exact integer cents", () => {
    const result = parseUsdToCents("10.25");

    expect(result).toBe(1025);
    expect(Number.isSafeInteger(result)).toBe(true);
  });
});

describe("formatUsdCents", () => {
  it.each([
    [0, "$0.00"],
    [1, "$0.01"],
    [1000, "$10.00"],
    [1025, "$10.25"],
    [10000000, "$100,000.00"],
    [-1025, "-$10.25"],
    [-1, "-$0.01"],
    [Number.MAX_SAFE_INTEGER, "$90,071,992,547,409.91"],
  ])("formats %i cents as %s", (input, expected) => {
    expect(formatUsdCents(input)).toBe(expected);
  });

  it("supports signed positive formatting", () => {
    expect(formatUsdCents(1025, { showPlus: true })).toBe(
      "+$10.25",
    );
  });

  it("does not add a plus sign to zero", () => {
    expect(formatUsdCents(0, { showPlus: true })).toBe(
      "$0.00",
    );
  });

  it("preserves negative formatting when showPlus is enabled", () => {
    expect(formatUsdCents(-1025, { showPlus: true })).toBe(
      "-$10.25",
    );
  });

  it("formats negative zero as zero", () => {
    expect(formatUsdCents(-0)).toBe("$0.00");
  });

  it("rejects unsafe and fractional values", () => {
    expect(() =>
      formatUsdCents(Number.MAX_SAFE_INTEGER + 1),
    ).toThrow(RangeError);

    expect(() => formatUsdCents(10.5)).toThrow(RangeError);
    expect(() => formatUsdCents(Number.NaN)).toThrow(RangeError);
    expect(() => formatUsdCents(Infinity)).toThrow(RangeError);
  });

  it("does not change the source value", () => {
    const cents = 1025;

    formatUsdCents(cents);

    expect(cents).toBe(1025);
  });

  it("produces consistent formatting across calls", () => {
    const first = formatUsdCents(123456);
    const second = formatUsdCents(123456);

    expect(first).toBe(second);
  });
});

describe("formatWholeShares", () => {
  it.each([
    [0, "0 shares"],
    [1, "1 share"],
    [25, "25 shares"],
    [1000, "1,000 shares"],
    [1000000, "1,000,000 shares"],
  ])("formats %i as %s", (quantity, expected) => {
    expect(formatWholeShares(quantity)).toBe(expected);
  });

  it("formats negative zero as zero shares", () => {
    expect(formatWholeShares(-0)).toBe("0 shares");
  });

  it("rejects negative quantities", () => {
    expect(() => formatWholeShares(-1)).toThrow(RangeError);
  });

  it("rejects fractional quantities", () => {
    expect(() => formatWholeShares(1.5)).toThrow(RangeError);
  });

  it("rejects unsafe integer quantities", () => {
    expect(() =>
      formatWholeShares(Number.MAX_SAFE_INTEGER + 1),
    ).toThrow(RangeError);
  });

  it("rejects non-finite quantities", () => {
    expect(() => formatWholeShares(Infinity)).toThrow(RangeError);
    expect(() => formatWholeShares(Number.NaN)).toThrow(RangeError);
  });

  it("does not change the source quantity", () => {
    const quantity = 25;

    formatWholeShares(quantity);

    expect(quantity).toBe(25);
  });
});

describe("UTC and display time formatting", () => {
  const timestamp = "2026-10-20T15:00:00Z";

  it("formats UTC consistently", () => {
    expect(formatUtcTime(timestamp)).toBe(
      "Oct 20, 2026, 3:00:00 PM UTC",
    );
  });

  it("supports fractional seconds", () => {
    expect(formatUtcTime("2026-10-20T15:00:00.123Z")).toBe(
      "Oct 20, 2026, 3:00:00 PM UTC",
    );
  });

  it("converts UTC to America/Chicago", () => {
    expect(
      formatDisplayTime(timestamp, "America/Chicago"),
    ).toBe("Oct 20, 2026, 10:00:00 AM CDT");
  });

  it("handles daylight saving time correctly", () => {
    expect(
      formatDisplayTime(
        "2026-12-20T15:00:00Z",
        "America/Chicago",
      ),
    ).toBe("Dec 20, 2026, 9:00:00 AM CST");
  });

  it("produces deterministic output", () => {
    const first = formatUtcTime(timestamp);
    const second = formatUtcTime(timestamp);

    expect(first).toBe(second);
  });

  it("does not depend on local machine time zone for UTC", () => {
    expect(formatDisplayTime(timestamp, "UTC")).toBe(
      formatUtcTime(timestamp),
    );
  });

  it.each([
    "",
    "invalid",
    "2026-02-30T15:00:00Z",
    "2026-10-20T24:00:00Z",
    "2026-10-20T15:00:00+00:00",
    "2026-10-20",
    "2026-13-20T15:00:00Z",
    "2026-10-20T25:00:00Z",
  ])("rejects invalid UTC timestamp %s", (value) => {
    expect(() => formatUtcTime(value)).toThrow(RangeError);
  });

  it("rejects invalid display time zones", () => {
    expect(() =>
      formatDisplayTime(timestamp, "Not/A_Time_Zone"),
    ).toThrow(RangeError);
  });
});
