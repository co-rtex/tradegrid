import { useId } from 'react'
import './OrderBook.css'
import { formatUsdCents, formatWholeShares } from '../lib/money'

/**
 * One aggregated price level in GET /api/v1/orderbook (docs/api.md).
 * `price` is integer cents; `quantity` is the resting remaining quantity in
 * whole shares, summed over `orderCount` orders at that price.
 */
export interface PriceLevelDto {
  price: number
  quantity: number
  orderCount: number
}

/** Response body of GET /api/v1/orderbook (docs/api.md). */
export interface BookSnapshotDto {
  symbol: string
  asOfSequence: number
  bids: PriceLevelDto[] // highest price first, as sent by the server
  asks: PriceLevelDto[] // lowest price first, as sent by the server
}

export interface OrderBookProps {
  /** The symbol the book was requested for; names the heading in every state. */
  symbol: string
  book?: BookSnapshotDto | null
  isLoading?: boolean
  error?: string | null
}

type Side = 'bid' | 'ask'

const sides: Record<Side, { title: string; best: string; level: string; empty: string }> = {
  bid: { title: 'Bids (buy orders)', best: 'Best bid', level: 'Bid', empty: 'No bids.' },
  ask: { title: 'Asks (sell orders)', best: 'Best ask', level: 'Ask', empty: 'No asks.' },
}

const PLACEHOLDER = '—'

const countFormatter = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 })

function formatCount(count: number): string {
  if (!Number.isSafeInteger(count) || count < 0) {
    throw new RangeError('Count must be a nonnegative safe integer')
  }
  return countFormatter.format(count)
}

// The shared formatters throw on malformed values. Show a placeholder instead
// of letting one bad level blank the whole page or read as a real amount.
function safeFormat(format: (value: number) => string, value: number): string {
  try {
    return format(value)
  } catch {
    console.warn('OrderBook: could not format value', value)
    return PLACEHOLDER
  }
}

function BookSide({ side, levels, titleId }: { side: Side; levels: PriceLevelDto[]; titleId: string }) {
  const sideId = useId()
  const labels = sides[side]

  return (
    <div className="order-book-side" data-side={side}>
      <h3 id={sideId} className="order-book-side-title">
        {labels.title}
      </h3>
      {levels.length === 0 ? (
        <p>{labels.empty}</p>
      ) : (
        // Focusable so keyboard users can scroll the table on narrow screens.
        <div className="order-book-scroll" role="group" aria-labelledby={sideId} tabIndex={0}>
          <table className="order-book-table" aria-labelledby={`${titleId} ${sideId}`}>
            <thead>
              <tr>
                <th scope="col">Level</th>
                <th scope="col">Price (USD)</th>
                <th scope="col">Remaining quantity</th>
                <th scope="col">Orders</th>
              </tr>
            </thead>
            <tbody>
              {/* Rows stay in the order the server sent: best price first. */}
              {levels.map((level, index) => (
                <tr key={`${index}:${level.price}`}>
                  <th scope="row" className="order-book-level">
                    {index === 0 ? labels.best : `${labels.level} ${index + 1}`}
                  </th>
                  <td>{safeFormat(formatUsdCents, level.price)}</td>
                  <td>{safeFormat(formatWholeShares, level.quantity)}</td>
                  <td>{safeFormat(formatCount, level.orderCount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

export default function OrderBook({ symbol, book = null, isLoading = false, error = null }: OrderBookProps) {
  const titleId = useId()

  return (
    <section className="order-book" aria-labelledby={titleId}>
      <h2 id={titleId} className="order-book-title">
        {symbol} order book
      </h2>

      {isLoading ? (
        <p role="status">Loading order book…</p>
      ) : error ? (
        <p role="alert">Could not load the order book: {error}</p>
      ) : !book || (book.bids.length === 0 && book.asks.length === 0) ? (
        <p>No resting orders for {symbol}.</p>
      ) : (
        <>
          <p className="order-book-note">
            Resting quantity by price, as of order sequence {safeFormat(formatCount, book.asOfSequence)}.
          </p>
          <div className="order-book-sides">
            <BookSide side="bid" levels={book.bids} titleId={titleId} />
            <BookSide side="ask" levels={book.asks} titleId={titleId} />
          </div>
        </>
      )}
    </section>
  )
}
