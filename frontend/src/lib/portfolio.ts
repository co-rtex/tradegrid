import type { PortfolioTotalsDto } from '../api/client'

/**
 * Realized plus unrealized P&L, for display. GET /api/v1/portfolio returns the
 * two server-calculated totals but no combined field (docs/portfolio.md), so
 * the page shows their exact integer sum. It returns `null`, shown as
 * unavailable, if either value or the sum is not a safe integer: it never
 * guesses.
 */
export function combinedPnl(totals: Pick<PortfolioTotalsDto, 'realizedPnl' | 'unrealizedPnl'>): number | null {
  const { realizedPnl, unrealizedPnl } = totals
  if (!Number.isSafeInteger(realizedPnl) || !Number.isSafeInteger(unrealizedPnl)) return null
  const sum = realizedPnl + unrealizedPnl
  return Number.isSafeInteger(sum) ? sum : null
}
