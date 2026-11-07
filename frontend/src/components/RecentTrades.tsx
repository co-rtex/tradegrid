import { useId } from 'react'
import './RecentTrades.css'
import { formatUsdCents, formatUtcTime, formatWholeShares } from '../lib/money'

/**
 * One execution from GET /api/v1/executions (docs/api.md). `price` is integer
 * cents (always the resting order's price); `quantity` is whole shares.
 */
export interface ExecutionDto {
  id: string
  sequence: number
  executedAt: string
  symbol: string
  price: number
  quantity: number
  buyOrderId: string
  sellOrderId: string
  buyerUserId: string
  sellerUserId: string
}

export interface RecentTradesProps {
  /** The symbol the executions were requested for; names the heading in every state. */
  symbol: string
  executions: ExecutionDto[] // newest first, as sent by the server
  isLoading?: boolean
  error?: string | null
}

// The shared formatters throw on malformed values. Show a placeholder instead
// of letting one bad trade blank the whole page or read as a real amount.
function safeFormat<T>(format: (value: T) => string, value: T): string {
  try {
    return format(value)
  } catch {
    console.warn('RecentTrades: could not format value', value)
    return '—'
  }
}

export default function RecentTrades({ symbol, executions, isLoading = false, error = null }: RecentTradesProps) {
  const titleId = useId()

  return (
    <section className="recent-trades" aria-labelledby={titleId}>
      <h2 id={titleId} className="recent-trades-title">
        {symbol} recent trades
      </h2>

      {isLoading ? (
        <p role="status">Loading recent trades…</p>
      ) : error ? (
        <p role="alert">Could not load recent trades: {error}</p>
      ) : executions.length === 0 ? (
        <p>No trades yet for {symbol}.</p>
      ) : (
        <>
          <p className="recent-trades-note">Newest first.</p>
          {/* Focusable so keyboard users can scroll the table on narrow screens. */}
          <div className="recent-trades-scroll" role="group" aria-labelledby={titleId} tabIndex={0}>
            <table className="recent-trades-table" aria-labelledby={titleId}>
              <thead>
                <tr>
                  <th scope="col">Time</th>
                  <th scope="col">Price (USD)</th>
                  <th scope="col">Quantity</th>
                </tr>
              </thead>
              <tbody>
                {/* Rows stay in the order the server sent; sequence, not time, defines it. */}
                {executions.map((execution) => (
                  <tr key={execution.id}>
                    <td>{safeFormat(formatUtcTime, execution.executedAt)}</td>
                    <td>{safeFormat(formatUsdCents, execution.price)}</td>
                    <td>{safeFormat(formatWholeShares, execution.quantity)}</td>
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
