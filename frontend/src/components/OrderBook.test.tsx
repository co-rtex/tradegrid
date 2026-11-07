import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OrderBook, { type BookSnapshotDto, type OrderBookProps } from './OrderBook'

// Snapshots from GET /api/v1/orderbook?symbol=AAPL in docs/fixtures. Expected
// text is written out here so the tests do not depend on the formatters they check.

// 00-seed.json: a fresh server.
const seedBook: BookSnapshotDto = { symbol: 'AAPL', asOfSequence: 0, bids: [], asks: [] }

// 01-no-fill.json: Alice's sell of 10 at $190.00 rests.
const noFillBook: BookSnapshotDto = {
  symbol: 'AAPL',
  asOfSequence: 1,
  bids: [],
  asks: [{ price: 19000, quantity: 10, orderCount: 1 }],
}

// 03-partial-fill.json: Bob bought 25 at $190.50, 10 filled, 15 remain resting.
const partialFillBook: BookSnapshotDto = {
  symbol: 'AAPL',
  asOfSequence: 2,
  bids: [{ price: 19050, quantity: 15, orderCount: 1 }],
  asks: [],
}

// A deeper two-sided book built from the same fixture prices, in the order the
// contract says the server sends it: bids highest first, asks lowest first.
const deepBook: BookSnapshotDto = {
  symbol: 'AAPL',
  asOfSequence: 7,
  bids: [
    { price: 19050, quantity: 15, orderCount: 1 },
    { price: 19000, quantity: 40, orderCount: 3 },
    { price: 18900, quantity: 1, orderCount: 1 },
  ],
  asks: [
    { price: 19100, quantity: 10, orderCount: 1 },
    { price: 19150, quantity: 2500, orderCount: 12 },
  ],
}

function renderBook(props: Partial<OrderBookProps> = {}) {
  return render(<OrderBook symbol="AAPL" book={deepBook} {...props} />)
}

function sideTable(name: 'Bids (buy orders)' | 'Asks (sell orders)') {
  return screen.getByRole('table', { name: `AAPL order book ${name}` })
}

function rowTexts(table: HTMLElement) {
  // slice(1) skips the header row
  return within(table)
    .getAllByRole('row')
    .slice(1)
    .map((row) =>
      [within(row).getByRole('rowheader'), ...within(row).getAllByRole('cell')].map((cell) => cell.textContent),
    )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('OrderBook', () => {
  it('shows bids and asks in the supplied order with the best price first', () => {
    renderBook()

    expect(rowTexts(sideTable('Bids (buy orders)'))).toEqual([
      ['Best bid', '$190.50', '15 shares', '1'],
      ['Bid 2', '$190.00', '40 shares', '3'],
      ['Bid 3', '$189.00', '1 share', '1'],
    ])
    expect(rowTexts(sideTable('Asks (sell orders)'))).toEqual([
      ['Best ask', '$191.00', '10 shares', '1'],
      ['Ask 2', '$191.50', '2,500 shares', '12'],
    ])
  })

  it('keeps the backend order rather than sorting by price', () => {
    const book: BookSnapshotDto = {
      ...deepBook,
      bids: [
        { price: 18900, quantity: 1, orderCount: 1 },
        { price: 19050, quantity: 15, orderCount: 1 },
      ],
      asks: [
        { price: 19150, quantity: 2500, orderCount: 12 },
        { price: 19100, quantity: 10, orderCount: 1 },
      ],
    }
    renderBook({ book })

    expect(rowTexts(sideTable('Bids (buy orders)')).map((row) => row[1])).toEqual(['$189.00', '$190.50'])
    expect(rowTexts(sideTable('Asks (sell orders)')).map((row) => row[1])).toEqual(['$191.50', '$191.00'])
    expect(book.bids.map((level) => level.price)).toEqual([18900, 19050])
  })

  it('shows the remaining quantity of a partly filled bid from the partial-fill fixture', () => {
    renderBook({ book: partialFillBook })

    expect(rowTexts(sideTable('Bids (buy orders)'))).toEqual([['Best bid', '$190.50', '15 shares', '1']])
  })

  it('shows a book with only bids, saying there are no asks', () => {
    renderBook({ book: partialFillBook })

    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('heading', { level: 3, name: 'Asks (sell orders)' })).toBeInTheDocument()
    expect(screen.getByText('No asks.')).toBeInTheDocument()
    expect(screen.queryByText('No bids.')).not.toBeInTheDocument()
  })

  it('shows a book with only asks, saying there are no bids', () => {
    renderBook({ book: noFillBook })

    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(rowTexts(sideTable('Asks (sell orders)'))).toEqual([['Best ask', '$190.00', '10 shares', '1']])
    expect(screen.getByText('No bids.')).toBeInTheDocument()
    expect(screen.queryByText('No asks.')).not.toBeInTheDocument()
  })

  it('shows an empty state with no tables for the seeded empty book', () => {
    renderBook({ book: seedBook })

    expect(screen.getByText('No resting orders for AAPL.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('shows the empty state when no snapshot is supplied', () => {
    renderBook({ book: null })

    expect(screen.getByText('No resting orders for AAPL.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it.each([
    [1, '$0.01'],
    [19050, '$190.50'],
    [100000000, '$1,000,000.00'],
  ])('formats a price of %d cents as %s', (price, text) => {
    renderBook({ book: { ...partialFillBook, bids: [{ price, quantity: 15, orderCount: 1 }] } })

    expect(rowTexts(sideTable('Bids (buy orders)'))[0][1]).toBe(text)
  })

  it('states the side in words, not only in color', () => {
    renderBook()

    expect(screen.getByRole('heading', { level: 3, name: 'Bids (buy orders)' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: 'Asks (sell orders)' })).toBeInTheDocument()
    expect(screen.getByRole('rowheader', { name: 'Best bid' })).toHaveAttribute('scope', 'row')
    expect(screen.getByRole('rowheader', { name: 'Best ask' })).toHaveAttribute('scope', 'row')
  })

  it('labels the heading, tables, and columns with units', () => {
    renderBook()

    expect(screen.getByRole('heading', { level: 2, name: 'AAPL order book' })).toBeInTheDocument()
    for (const name of ['Bids (buy orders)', 'Asks (sell orders)'] as const) {
      const table = sideTable(name)
      const headers = within(table).getAllByRole('columnheader')
      expect(headers.map((header) => header.textContent)).toEqual([
        'Level',
        'Price (USD)',
        'Remaining quantity',
        'Orders',
      ])
      for (const header of headers) {
        expect(header).toHaveAttribute('scope', 'col')
      }
    }
  })

  it('keeps each table scrollable from the keyboard', () => {
    renderBook()

    expect(screen.getByRole('group', { name: 'Bids (buy orders)' })).toHaveAttribute('tabindex', '0')
    expect(screen.getByRole('group', { name: 'Asks (sell orders)' })).toHaveAttribute('tabindex', '0')
  })

  it('shows which order sequence the snapshot reflects', () => {
    renderBook({ book: partialFillBook })

    expect(screen.getByText('Resting quantity by price, as of order sequence 2.')).toBeInTheDocument()
  })

  it.each([
    ['fractional cents', { price: 1.5 }, 1, 1.5],
    ['a price that is not a number', { price: Number.NaN }, 1, Number.NaN],
    ['fractional shares', { quantity: 1.5 }, 2, 1.5],
    ['negative shares', { quantity: -1 }, 2, -1],
    ['a fractional order count', { orderCount: 1.5 }, 3, 1.5],
  ])('shows a placeholder for %s rather than a misleading value', (_name, overrides, column, logged) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const level = { price: 19050, quantity: 15, orderCount: 1, ...overrides }
    renderBook({ book: { ...partialFillBook, bids: [level] } })

    const row = rowTexts(sideTable('Bids (buy orders)'))[0]
    expect(row[column]).toBe('—')
    expect(row[0]).toBe('Best bid')
    expect(warn).toHaveBeenCalledWith('OrderBook: could not format value', logged)
  })

  it('shows a placeholder for a malformed snapshot sequence', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    renderBook({ book: { ...partialFillBook, asOfSequence: 2.5 } })

    expect(screen.getByText('Resting quantity by price, as of order sequence —.')).toBeInTheDocument()
    expect(warn).toHaveBeenCalledWith('OrderBook: could not format value', 2.5)
  })

  it('announces loading and shows no levels, even when an old snapshot is supplied', () => {
    renderBook({ isLoading: true })

    expect(screen.getByRole('status')).toHaveTextContent('Loading order book…')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'AAPL order book' })).toBeInTheDocument()
  })

  it('announces an error and shows no levels, even when an old snapshot is supplied', () => {
    renderBook({ error: 'Server unavailable' })

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load the order book: Server unavailable')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('No resting orders for AAPL.')).not.toBeInTheDocument()
  })

  it('gives each instance its own heading association', () => {
    render(
      <>
        <OrderBook symbol="AAPL" book={deepBook} />
        <OrderBook symbol="MSFT" book={{ ...deepBook, symbol: 'MSFT' }} />
      </>,
    )

    expect(screen.getByRole('table', { name: 'AAPL order book Bids (buy orders)' })).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'MSFT order book Bids (buy orders)' })).toBeInTheDocument()
    expect(screen.getAllByRole('group', { name: 'Asks (sell orders)' })).toHaveLength(2)
  })
})
