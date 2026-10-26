
/**
 * Shared money, quantity, and time utilities for TradeGrid.
 *
 * API amounts remain integer cents and whole shares.
 * Conversion happens at input and display boundaries.
 *
 * Reference: docs/api.md, Sprint 2 contract (#30).
 */

export const MAX_ORDER_PRICE_CENTS = 100_000_000;
export const MAX_ORDER_QUANTITY = 1_000_000;

const MAX_INPUT_LENGTH = 32;

const integerFormatter = new Intl.NumberFormat("en-US", {
  useGrouping: true,
  maximumFractionDigits: 0,
});

export type MoneyErrorCode =
  | "BLANK"
  | "FORMAT"
  | "PRECISION"
  | "RANGE";

export class MoneyInputError extends Error {
  readonly code: MoneyErrorCode;

  constructor(code: MoneyErrorCode, message: string) {
    super(message);
    this.name = "MoneyInputError";
    this.code = code;
  }
}

function requireSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value)) {
    throw new RangeError(`${label} must be a safe integer`);
  }
}

/**
 * Converts USD decimal text into exact integer cents.
 *
 * Only positive limit-order prices within the Sprint 2
 * contract are accepted.
 *
 * No floating-point conversion or rounding is used.
 *
 * Examples:
 * "0.01"  -> 1
 * "10"    -> 1000
 * "10.25" -> 1025
 */
export function parseUsdToCents(value: string): number {
  const input = value.trim();

  if (input.length === 0) {
    throw new MoneyInputError(
      "BLANK",
      "Enter a price",
    );
  }

  if (input.length > MAX_INPUT_LENGTH) {
    throw new MoneyInputError(
      "RANGE",
      "Price input is too long",
    );
  }

  if (/^\d+\.\d{3,}$/.test(input)) {
    throw new MoneyInputError(
      "PRECISION",
      "Prices cannot contain fractional cents",
    );
  }

  if (!/^\d+(?:\.\d{1,2})?$/.test(input)) {
    throw new MoneyInputError(
      "FORMAT",
      "Enter a valid decimal price",
    );
  }

  const [dollars, fraction = ""] = input.split(".");
  const centsText = fraction.padEnd(2, "0");

  const total =
    BigInt(dollars) * 100n + BigInt(centsText);

  if (total < 1n || total > BigInt(MAX_ORDER_PRICE_CENTS)) {
    const maximum = formatUsdCents(MAX_ORDER_PRICE_CENTS);

    throw new MoneyInputError(
      "RANGE",
      `Price must be between $0.01 and ${maximum}`,
    );
  }

  return Number(total);
}

/**
 * Formats safe integer cents as USD.
 *
 * Signed formatting is display-only.
 * It does not calculate portfolio P&L.
 */
export function formatUsdCents(
  cents: number,
  options: { showPlus?: boolean } = {},
): string {
  requireSafeInteger(cents, "Cents");

  const absolute = Math.abs(cents);
  const dollars = Math.floor(absolute / 100);
  const remainder = absolute % 100;

  const sign =
    cents < 0 ? "-" : options.showPlus && cents > 0 ? "+" : "";

  return (
    `${sign}$${integerFormatter.format(dollars)}.` +
    remainder.toString().padStart(2, "0")
  );
}

/**
 * Formats a nonnegative integer quantity of shares.
 *
 * This helper formats quantities but does not enforce
 * the order-entry maximum. Order validation owns that rule.
 */
export function formatWholeShares(quantity: number): string {
  requireSafeInteger(quantity, "Share quantity");

  if (quantity < 0) {
    throw new RangeError("Share quantity cannot be negative");
  }

  const suffix = quantity === 1 ? "share" : "shares";

  return `${integerFormatter.format(Math.abs(quantity))} ${suffix}`;
}

/**
 * Cache formatters by time zone to avoid repeated creation.
 */
const timeFormatters = new Map<string, Intl.DateTimeFormat>();

function getTimeFormatter(timeZone: string): Intl.DateTimeFormat {
  const existing = timeFormatters.get(timeZone);

  if (existing) {
    return existing;
  }

  const formatter = new Intl.DateTimeFormat("en-US", {
    timeZone,
    timeZoneName: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
    hour12: true,
  });

  timeFormatters.set(timeZone, formatter);

  return formatter;
}

/**
 * Formats an RFC 3339 UTC timestamp for display.
 *
 * Explicit parts prevent locale-dependent whitespace
 * from changing expected output between environments.
 */
export function formatDisplayTime(
  timestamp: string,
  timeZone = "UTC",
): string {
  const utcPattern =
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/;

  if (!utcPattern.test(timestamp)) {
    throw new RangeError(
      "Timestamp must be an RFC 3339 UTC string",
    );
  }

  const date = new Date(timestamp);

  if (!Number.isFinite(date.getTime())) {
    throw new RangeError("Invalid UTC timestamp");
  }

  if (date.toISOString().slice(0, 19) !== timestamp.slice(0, 19)) {
    throw new RangeError("Invalid UTC calendar date or time");
  }

  const parts = getTimeFormatter(timeZone).formatToParts(date);

  const get = (type: Intl.DateTimeFormatPartTypes): string => {
    return parts.find((part) => part.type === type)?.value ?? "";
  };

  const month = get("month");
  const day = get("day");
  const year = get("year");
  const hour = get("hour");
  const minute = get("minute");
  const second = get("second");
  const period = get("dayPeriod");
  const zone = get("timeZoneName");

  return (
    `${month} ${day}, ${year}, ` +
    `${hour}:${minute}:${second} ${period} ${zone}`
  );
}

/**
 * Formats an API timestamp in UTC.
 */
export function formatUtcTime(timestamp: string): string {
  return formatDisplayTime(timestamp, "UTC");
}
