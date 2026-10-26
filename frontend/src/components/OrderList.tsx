import './OrderList.css'
import { formatUsdCents, formatUtcTime, formatWholeShares } from '../lib/money'

export type OrderStatus =
  | 'NEW'
  | 'PARTIALLY_FILLED'
  | 'FILLED'
  | 'CANCELED'
  | 'REJECTED'

export interface OrderDto {
  id: string
  userId: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: 'LIMIT' | 'MARKET'
  price: number // whole cents
  quantity: number // original quantity
  remaining: number
  status: OrderStatus
  sequence: number
  createdAt: string
  updatedAt: string
}

const statusLabels: Record<OrderStatus, string> = {
  NEW: 'New',
  PARTIALLY_FILLED: 'Partially filled',
  FILLED: 'Filled',
  CANCELED: 'Canceled',
  REJECTED: 'Rejected',
}

export interface OrderListProps {
  orders: OrderDto[]
  accountName: string
  isLoading?: boolean
  error?: string | null
}

// Filled = original quantity minus remaining (confirmed OK by maintainers;
// the API does not send a separate filled field).
function filledQuantity(order: OrderDto) {
  return order.quantity - order.remaining
}

// The shared formatters throw on malformed values. Show the raw value instead
// of letting one bad order blank the whole page.
function safeFormat<T>(format: (value: T) => string, value: T): string {
  try {
    return format(value)
  } catch {
    return String(value)
  }
}

export default function OrderList({
  orders,
  accountName,
  isLoading = false,
  error = null,
}: OrderListProps) {
  return (
    <section className="order-list" aria-labelledby="order-list-title">
      <h2 id="order-list-title" className="order-list-title">
        Orders for {accountName}
      </h2>

      {isLoading ? (
        <p role="status">Loading orders…</p>
      ) : error ? (
        <p role="alert">Could not load orders: {error}</p>
      ) : orders.length === 0 ? (
        <p>No orders yet for {accountName}.</p>
      ) : (
        <table className="order-table">
          <thead>
            <tr>
              <th scope="col">Symbol</th>
              <th scope="col">Side</th>
              <th scope="col">Price</th>
              <th scope="col">Original qty</th>
              <th scope="col">Remaining qty</th>
              <th scope="col">Filled qty</th>
              <th scope="col">Status</th>
              <th scope="col">Accepted</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((order) => (
              <tr key={order.id}>
                <td>{order.symbol}</td>
                <td className={order.side === 'BUY' ? 'side-buy' : 'side-sell'}>
                  {order.side === 'BUY' ? 'Buy' : 'Sell'}
                </td>
                {/* TODO: use the shared formatters from #40 for price and time */}
                <td>{safeFormat(formatUsdCents, order.price)}</td>
                <td>{safeFormat(formatWholeShares, order.quantity)}</td>
                <td>{safeFormat(formatWholeShares, order.remaining)}</td>
                <td>{safeFormat(formatWholeShares, filledQuantity(order))}</td>
                <td>
                  <span className="status-badge" data-status={order.status}>
                    {statusLabels[order.status]}
                  </span>
                </td>
                <td>{safeFormat(formatUtcTime, order.createdAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
