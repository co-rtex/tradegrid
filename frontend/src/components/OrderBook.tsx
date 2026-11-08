import { formatUsdCents, formatWholeShares } from '../lib/money'
import './OrderBook.css'

export type PriceLevel = {
  price: number
  quantity: number
  orderCount: number
}

export type OrderBookSnapshot = {
  symbol: string
  asOfSequence: number
  bids: PriceLevel[]
  asks: PriceLevel[]
}

export interface OrderBookProps {
  data?: OrderBookSnapshot | null
  isLoading?: boolean
  error?: string | null
}

function displayPrice(price: number): string {
  return Number.isSafeInteger(price) && price > 0 ? formatUsdCents(price) : '—'
}

function displayShares(quantity: number): string {
  return Number.isSafeInteger(quantity) && quantity >= 0 ? formatWholeShares(quantity) : '—'
}

function BookSide({ levels, side, symbol }: {
  levels: PriceLevel[]
  side: 'bid' | 'ask'
  symbol: string
}) {
  const label = side === 'bid' ? 'BUY / BID' : 'SELL / ASK'
  return (
    <section className={`order-book-side order-book-${side}`}>
      <h3>{label}</h3>
      <p className="order-book-best">
        Best {side}: {levels[0] ? displayPrice(levels[0].price) : 'Unavailable'}
      </p>
      <div className="order-book-scroll" role="region" aria-label={`${label} table scroll area`} tabIndex={0}>
        <table>
          <caption>{symbol} {label} price levels</caption>
          <thead>
            <tr>
              <th scope="col">Price (USD)</th>
              <th scope="col">Remaining shares</th>
              <th scope="col">Orders</th>
            </tr>
          </thead>
          <tbody>
            {levels.length === 0 ? (
              <tr><td colSpan={3}>No {side === 'bid' ? 'bids' : 'asks'}.</td></tr>
            ) : levels.map((level, index) => (
              <tr key={`${level.price}-${index}`}>
                <td>{displayPrice(level.price)}</td>
                <td>{displayShares(level.quantity)}</td>
                <td>{Number.isSafeInteger(level.orderCount) && level.orderCount >= 0 ? level.orderCount : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/** Displays an API snapshot as supplied; aggregation and ordering belong to the server. */
export default function OrderBook({ data, isLoading = false, error }: OrderBookProps) {
  return (
    <section className="order-book" aria-label="Order book">
      <h2>Order book{data ? ` · ${data.symbol}` : ''}</h2>
      {error ? <p role="alert">{error}</p> : isLoading ? (
        <p role="status">Loading order book…</p>
      ) : !data ? <p>No order book data available.</p> : (
        <>
          {data.bids.length === 0 && data.asks.length === 0 && <p>No resting orders.</p>}
          <div className="order-book-sides">
            <BookSide levels={data.bids} side="bid" symbol={data.symbol} />
            <BookSide levels={data.asks} side="ask" symbol={data.symbol} />
          </div>
        </>
      )}
    </section>
  )
}
