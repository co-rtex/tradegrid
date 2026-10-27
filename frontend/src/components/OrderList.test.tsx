import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import OrderList, { type OrderDto, type OrderListProps, type OrderStatus } from './OrderList'

function makeOrder(overrides: Partial<OrderDto> = {}): OrderDto {
  return {
    id: 'ord-1',
    userId: 'alice',
    symbol: 'AAPL',
    side: 'BUY',
    type: 'LIMIT',
    price: 19050,
    quantity: 10,
    remaining: 10,
    status: 'NEW',
    sequence: 1,
    createdAt: '2026-10-20T15:00:01Z',
    updatedAt: '2026-10-20T15:00:01Z',
    ...overrides,
  }
}

function renderList(props: Partial<OrderListProps> = {}) {
  render(<OrderList orders={[]} accountName="Alice (demo)" {...props} />)
}

function bodyRows() {
  // slice(1) skips the header row
  return within(screen.getByRole('table')).getAllByRole('row').slice(1)
}

function cellTexts(row: HTMLElement) {
  return within(row)
    .getAllByRole('cell')
    .map((cell) => cell.textContent)
}

// Column positions in each row
const SIDE = 1
const PRICE = 2
const ORIGINAL = 3
const REMAINING = 4
const FILLED = 5
const STATUS = 6
const ACCEPTED = 7

// Expected text for a share count, written out here so the test does not
// depend on the formatter it is checking.
function shares(count: number) {
  return `${count} ${count === 1 ? 'share' : 'shares'}`
}

const quantityCases: {
  status: OrderStatus
  quantity: number
  remaining: number
  filled: number
  label: string
}[] = [
  { status: 'NEW', quantity: 10, remaining: 10, filled: 0, label: 'New' },
  { status: 'PARTIALLY_FILLED', quantity: 25, remaining: 15, filled: 10, label: 'Partially filled' },
  { status: 'FILLED', quantity: 10, remaining: 0, filled: 10, label: 'Filled' },
  { status: 'CANCELED', quantity: 20, remaining: 12, filled: 8, label: 'Canceled' },
  { status: 'REJECTED', quantity: 5, remaining: 5, filled: 0, label: 'Rejected' },
  { status: 'PARTIALLY_FILLED', quantity: 2, remaining: 1, filled: 1, label: 'Partially filled' },
]

const sideCases: { side: OrderDto['side']; label: string }[] = [
  { side: 'BUY', label: 'Buy' },
  { side: 'SELL', label: 'Sell' },
]

const priceCases: { price: number; text: string }[] = [
  { price: 1, text: '$0.01' },
  { price: 19000, text: '$190.00' },
  { price: 19050, text: '$190.50' },
  { price: 100000000, text: '$1,000,000.00' },
]

describe('OrderList', () => {
  it.each(quantityCases)(
    'shows original $quantity, remaining $remaining and filled $filled for $status',
    ({ status, quantity, remaining, filled, label }) => {
      renderList({ orders: [makeOrder({ status, quantity, remaining })] })

      const cells = cellTexts(bodyRows()[0])
      expect(cells[ORIGINAL]).toBe(shares(quantity))
      expect(cells[REMAINING]).toBe(shares(remaining))
      expect(cells[FILLED]).toBe(shares(filled))
      expect(cells[STATUS]).toBe(label)
    },
  )

  it.each(sideCases)('labels a $side order as $label', ({ side, label }) => {
    renderList({ orders: [makeOrder({ side })] })

    expect(cellTexts(bodyRows()[0])[SIDE]).toBe(label)
  })

  it.each(priceCases)('formats a price of $price cents as $text', ({ price, text }) => {
    renderList({ orders: [makeOrder({ price })] })

    expect(cellTexts(bodyRows()[0])[PRICE]).toBe(text)
  })

  it('formats the acceptance time in UTC', () => {
    renderList({ orders: [makeOrder({ createdAt: '2026-10-20T15:00:01Z' })] })

    expect(cellTexts(bodyRows()[0])[ACCEPTED]).toBe('Oct 20, 2026, 3:00:01 PM UTC')
  })

  it('shows the raw value instead of crashing when a timestamp is malformed', () => {
    renderList({ orders: [makeOrder({ createdAt: 'not-a-time' })] })

    expect(cellTexts(bodyRows()[0])[ACCEPTED]).toBe('not-a-time')
  })

  it('shows the raw value instead of crashing when a price is not a whole number of cents', () => {
    renderList({ orders: [makeOrder({ price: 1.5 })] })

    expect(cellTexts(bodyRows()[0])[PRICE]).toBe('1.5')
  })

  it('shows the column headings in the contract order', () => {
    renderList({ orders: [makeOrder()] })

    const headings = screen.getAllByRole('columnheader').map((heading) => heading.textContent)
    expect(headings).toEqual([
      'Symbol',
      'Side',
      'Price',
      'Original qty',
      'Remaining qty',
      'Filled qty',
      'Status',
      'Accepted',
    ])
  })

  it('shows symbol text for each order', () => {
    renderList({
      orders: [makeOrder({ id: 'ord-1', symbol: 'AAPL' }), makeOrder({ id: 'ord-2', symbol: 'MSFT' })],
    })

    const symbols = bodyRows().map((row) => cellTexts(row)[0])
    expect(symbols).toEqual(['AAPL', 'MSFT'])
  })

  it('keeps the order the API supplied instead of sorting', () => {
    renderList({
      orders: [
        makeOrder({ id: 'ord-3', sequence: 3, quantity: 30, remaining: 30 }),
        makeOrder({ id: 'ord-1', sequence: 1, quantity: 10, remaining: 10 }),
        makeOrder({ id: 'ord-2', sequence: 2, quantity: 20, remaining: 20 }),
      ],
    })

    const originals = bodyRows().map((row) => cellTexts(row)[ORIGINAL])
    expect(originals).toEqual([shares(30), shares(10), shares(20)])
  })

  it('renders every domain status as readable text, including canceled and rejected', () => {
    const statuses: OrderStatus[] = ['NEW', 'PARTIALLY_FILLED', 'FILLED', 'CANCELED', 'REJECTED']
    renderList({
      orders: statuses.map((status, index) => makeOrder({ id: `ord-${index + 1}`, status })),
    })

    const labels = bodyRows().map((row) => cellTexts(row)[STATUS])
    expect(labels).toEqual(['New', 'Partially filled', 'Filled', 'Canceled', 'Rejected'])
  })

  it('offers no actions for any status, such as cancel buttons', () => {
    const statuses: OrderStatus[] = ['NEW', 'PARTIALLY_FILLED', 'FILLED', 'CANCELED', 'REJECTED']
    renderList({
      orders: statuses.map((status, index) => makeOrder({ id: `ord-${index + 1}`, status })),
    })

    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it('names the selected account in the heading', () => {
    renderList({ orders: [makeOrder()], accountName: 'Bob (demo)' })

    expect(screen.getByRole('heading', { level: 2, name: 'Orders for Bob (demo)' })).toBeInTheDocument()
  })

  it('shows an empty message with the account name when there are no orders', () => {
    renderList({ orders: [] })

    expect(screen.getByText('No orders yet for Alice (demo).')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows a loading message instead of the table while loading', () => {
    renderList({ isLoading: true, orders: [makeOrder()] })

    expect(screen.getByRole('status')).toHaveTextContent('Loading orders')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows the error message as an alert instead of the table', () => {
    renderList({ error: 'Network down', orders: [makeOrder()] })

    expect(screen.getByRole('alert')).toHaveTextContent('Network down')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})