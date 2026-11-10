import { formatUsdCents } from '../lib/money'
import './PortfolioSummary.css'

export interface PortfolioSummaryProps {
  /** `totals.equity` from GET /api/v1/portfolio, in integer cents. */
  equity: number
  /** `cash.total`, in integer cents (reserved cash is still owned). */
  cashTotal: number
  /** Realized plus unrealized P&L, in integer cents, or `null` when it cannot be shown. */
  combinedPnl: number | null
  /** Whether the account holds any shares. */
  hasPositions: boolean
}

/** Formats a server amount; a malformed one shows a placeholder, never a guess. */
function usd(cents: number | null, options?: { showPlus?: boolean }): string {
  if (cents === null) return 'Unavailable'
  try {
    return formatUsdCents(cents, options)
  } catch {
    return '—'
  }
}

/** The portfolio card (#11, #23), showing server-calculated totals for one account. */
function PortfolioSummary({ equity, cashTotal, combinedPnl, hasPositions }: PortfolioSummaryProps) {
  return (
    <section className="portfolio-summary" aria-label="Portfolio summary">
      <h2 className="portfolio-heading">Portfolio</h2>

      <div className="portfolio-total">
        <div>
          <p className="portfolio-label">Total Portfolio Value</p>
          <p className="portfolio-value">{usd(equity)}</p>
        </div>
        <svg className="portfolio-chart" viewBox="0 0 240 100" aria-hidden="true" focusable="false">
          {/* Decorative only: there is no value history to chart. */}
          <path className="portfolio-chart-fill" d="M4 50 H236 V100 H4 Z" />
          <path className="portfolio-chart-line" d="M4 50 H236" />
        </svg>
      </div>

      <dl className="portfolio-metrics">
        <div>
          <dt className="portfolio-label">Cash Balance</dt>
          <dd>{usd(cashTotal)}</dd>
        </div>
        <div>
          <dt className="portfolio-label">P&amp;L</dt>
          <dd>{usd(combinedPnl, { showPlus: true })}</dd>
        </div>
      </dl>

      {!hasPositions && (
        <div className="portfolio-empty">
          <svg className="portfolio-icon" viewBox="0 0 48 48" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
            <rect x="7" y="15" width="34" height="26" rx="4" />
            <path d="M17 15 V11 A3 3 0 0 1 20 8 H28 A3 3 0 0 1 31 11 V15 M7 24 Q24 34 41 24 M21 27 H27 V32 H21 Z" />
          </svg>
          <h3>No positions yet</h3>
          <p>Place your first trade to start building your portfolio.</p>
        </div>
      )}
    </section>
  )
}

export default PortfolioSummary
