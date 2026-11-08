import { formatUsdCents, formatWholeShares, formatUtcTime } from '../lib/money'
import './RecentTrades.css'

export type Execution = {
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
  executions: Execution[]
  isLoading?: boolean
  error?: string | null
}

function displayTime(timestamp: string): string {
  try {
    return formatUtcTime(timestamp)
  } catch {
    return '—'
  }
}

/** Executions remain in supplied server-sequence order, with no inferred trade side. */
export default function RecentTrades({ executions, isLoading = false, error }: RecentTradesProps) {
  return (
    <section className="recent-trades" aria-label="Recent trades">
      <h2>Recent trades</h2>
      {error ? <p role="alert">{error}</p> : isLoading ? (
        <p role="status">Loading recent trades…</p>
      ) : executions.length === 0 ? <p>No recent trades.</p> : (
        <div className="recent-trades-scroll" role="region" aria-label="Recent trades table scroll area" tabIndex={0}>
          <table>
            <caption>Recent executions · newest first</caption>
            <thead>
              <tr>
                <th scope="col">Execution time (UTC)</th>
                <th scope="col">Symbol</th>
                <th scope="col">Price (USD)</th>
                <th scope="col">Quantity (shares)</th>
              </tr>
            </thead>
            <tbody>
              {executions.map((execution) => (
                <tr key={execution.id}>
                  <td>{displayTime(execution.executedAt)}</td>
                  <td>{execution.symbol}</td>
                  <td>{Number.isSafeInteger(execution.price) && execution.price > 0 ? formatUsdCents(execution.price) : '—'}</td>
                  <td>{Number.isSafeInteger(execution.quantity) && execution.quantity >= 0 ? formatWholeShares(execution.quantity) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
