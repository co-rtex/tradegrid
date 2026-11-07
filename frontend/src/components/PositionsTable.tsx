import { useId } from 'react'
import './PositionsTable.css'
import { formatUsdCents, formatWholeShares } from '../lib/money'

/**
 * One entry of `positions` in GET /api/v1/portfolio (docs/portfolio.md).
 * Amounts are integer cents and quantities are whole shares.
 */
export interface PortfolioPositionDto {
  symbol: string
  total: number
  reserved: number
  available: number
  costBasis: number
  referencePrice: number
  marketValue: number
  unrealizedPnl: number
  realizedPnl: number
}

export interface PositionsTableProps {
  positions: PortfolioPositionDto[]
  isLoading?: boolean
  error?: string | null
}

const PLACEHOLDER = '—'

// The shared formatters throw on malformed values. Show a placeholder instead
// of letting one bad position blank the whole page or read as a real amount.
function safeFormat(format: (value: number) => string, value: number): string {
  try {
    return format(value)
  } catch {
    console.warn('PositionsTable: could not format value', value)
    return PLACEHOLDER
  }
}

function formatSignedUsdCents(cents: number): string {
  return formatUsdCents(cents, { showPlus: true })
}

// Screen readers often skip "+" and "-", and color carries nothing for them,
// so the direction is also stated in words.
function pnlDirection(cents: number) {
  if (cents > 0) return { direction: 'gain', spoken: 'gain' }
  if (cents < 0) return { direction: 'loss', spoken: 'loss' }
  return { direction: 'flat', spoken: 'no gain or loss' }
}

function UnrealizedPnlCell({ cents }: { cents: number }) {
  const text = safeFormat(formatSignedUsdCents, cents)
  if (text === PLACEHOLDER) {
    return <td>{PLACEHOLDER}</td>
  }
  const { direction, spoken } = pnlDirection(cents)
  return (
    <td className="positions-pnl" data-direction={direction}>
      {text}
      <span className="positions-sr-only"> {spoken}</span>
    </td>
  )
}

export default function PositionsTable({
  positions,
  isLoading = false,
  error = null,
}: PositionsTableProps) {
  const titleId = useId()

  return (
    <section className="positions" aria-labelledby={titleId}>
      <h2 id={titleId} className="positions-title">
        Positions
      </h2>

      {isLoading ? (
        <p role="status">Loading positions…</p>
      ) : error ? (
        <p role="alert">Could not load positions: {error}</p>
      ) : positions.length === 0 ? (
        <p>No positions to show.</p>
      ) : (
        <>
          <p className="positions-note">
            Valued at fixed, simulated reference prices, not live market prices.
          </p>
          {/* Focusable so keyboard users can scroll the table on narrow screens. */}
          <div className="positions-scroll" role="group" aria-labelledby={titleId} tabIndex={0}>
            <table className="positions-table" aria-labelledby={titleId}>
              <thead>
                <tr>
                  <th scope="col">Symbol</th>
                  <th scope="col">Total</th>
                  <th scope="col">Reserved</th>
                  <th scope="col">Available</th>
                  <th scope="col">Reference price</th>
                  <th scope="col">Market value</th>
                  <th scope="col">Cost basis</th>
                  <th scope="col">Unrealized P&amp;L</th>
                </tr>
              </thead>
              <tbody>
                {positions.map((position) => (
                  <tr key={position.symbol}>
                    <th scope="row">{position.symbol}</th>
                    <td>{safeFormat(formatWholeShares, position.total)}</td>
                    <td>{safeFormat(formatWholeShares, position.reserved)}</td>
                    <td>{safeFormat(formatWholeShares, position.available)}</td>
                    <td>{safeFormat(formatUsdCents, position.referencePrice)}</td>
                    <td>{safeFormat(formatUsdCents, position.marketValue)}</td>
                    <td>{safeFormat(formatUsdCents, position.costBasis)}</td>
                    <UnrealizedPnlCell cents={position.unrealizedPnl} />
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  )
}
