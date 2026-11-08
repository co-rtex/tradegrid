import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import OrderBook, { type OrderBookSnapshot } from './OrderBook'
import seed from '../../../docs/fixtures/00-seed.json'
import noFill from '../../../docs/fixtures/01-no-fill.json'
import fullFill from '../../../docs/fixtures/02-full-fill.json'
import partialFill from '../../../docs/fixtures/03-partial-fill.json'
import rejection from '../../../docs/fixtures/04-risk-rejection.json'

const bookKey = 'GET /api/v1/orderbook?symbol=AAPL'
const populated: OrderBookSnapshot = {
  symbol: 'AAPL', asOfSequence: 7,
  bids: [{ price: 19050, quantity: 15, orderCount: 2 }, { price: 19001, quantity: 1, orderCount: 1 }],
  asks: [{ price: 19101, quantity: 20, orderCount: 3 }, { price: 19200, quantity: 1000, orderCount: 4 }],
}
function table(side: string) {
  return screen.getByRole('table', { name: `AAPL ${side} price levels` })
}
function cells(side: string) {
  return within(table(side)).getAllByRole('row').slice(1).map(row =>
    within(row).getAllByRole('cell').map(cell => cell.textContent))
}

describe('OrderBook', () => {
  it.each([seed, fullFill, rejection])('shows the empty book for $name without price rows', (fixture) => {
    render(<OrderBook data={fixture.after[bookKey]} />)
    expect(screen.getByText('No resting orders.')).toBeInTheDocument()
    expect(cells('BUY / BID')).toEqual([['No bids.']])
    expect(cells('SELL / ASK')).toEqual([['No asks.']])
    expect(screen.getByText('Best bid: Unavailable')).toBeInTheDocument()
    expect(screen.getByText('Best ask: Unavailable')).toBeInTheDocument()
    expect(screen.queryByText('$190.00')).not.toBeInTheDocument()
  })
  it('shows the no-fill ask and unavailable bid independently', () => {
    render(<OrderBook data={noFill.after[bookKey]} />)
    expect(cells('SELL / ASK')).toEqual([['$190.00', '10 shares', '1']])
    expect(screen.getByText('Best ask: $190.00')).toBeInTheDocument()
    expect(screen.getByText('Best bid: Unavailable')).toBeInTheDocument()
    expect(screen.queryByText('No resting orders.')).not.toBeInTheDocument()
  })
  it('shows only the partial-fill remaining 15 shares, not the original 25', () => {
    render(<OrderBook data={partialFill.after[bookKey]} />)
    expect(cells('BUY / BID')).toEqual([['$190.50', '15 shares', '1']])
    expect(screen.queryByText('25 shares')).not.toBeInTheDocument()
    expect(screen.getByText('Best bid: $190.50')).toBeInTheDocument()
    expect(screen.getByText('Best ask: Unavailable')).toBeInTheDocument()
  })
  it('preserves all levels without mutating frozen props and summarizes first levels', () => {
    const data = structuredClone(populated)
    for (const levels of [data.bids, data.asks]) {
      levels.forEach(Object.freeze)
      Object.freeze(levels)
    }
    Object.freeze(data)
    render(<OrderBook data={data} />)
    expect(cells('BUY / BID')).toEqual([['$190.50', '15 shares', '2'], ['$190.01', '1 share', '1']])
    expect(cells('SELL / ASK')).toEqual([['$191.01', '20 shares', '3'], ['$192.00', '1,000 shares', '4']])
    expect(screen.getByText('Best bid: $190.50')).toBeInTheDocument()
    expect(screen.getByText('Best ask: $191.01')).toBeInTheDocument()
    expect(data).toEqual(populated)
  })
  it('uses supplied order even if levels are unsorted', () => {
    render(<OrderBook data={{ ...populated, bids: [...populated.bids].reverse(), asks: [...populated.asks].reverse() }} />)
    expect(cells('BUY / BID')[0][0]).toBe('$190.01')
    expect(cells('SELL / ASK')[0][0]).toBe('$192.00')
    expect(screen.getByText('Best bid: $190.01')).toBeInTheDocument()
    expect(screen.getByText('Best ask: $192.00')).toBeInTheDocument()
  })
  it.each([undefined, null])('handles missing data (%s)', (data) => {
    render(<OrderBook data={data} />)
    expect(screen.getByText('No order book data available.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
  it('gives error precedence over loading and data, then loading over data or empty state', () => {
    const { rerender } = render(<OrderBook data={populated} isLoading error="Book unavailable" />)
    expect(screen.getByRole('alert')).toHaveTextContent('Book unavailable')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    rerender(<OrderBook data={populated} isLoading />)
    expect(screen.getByRole('status')).toHaveTextContent('Loading order book…')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    rerender(<OrderBook isLoading />)
    expect(screen.queryByText('No order book data available.')).not.toBeInTheDocument()
    rerender(<OrderBook data={populated} error="" />)
    expect(screen.getAllByRole('table')).toHaveLength(2)
  })
  it('labels headings, visible sides, columns and keyboard-accessible scroll regions', () => {
    render(<OrderBook data={populated} />)
    expect(screen.getByRole('heading', { name: 'Order book · AAPL', level: 2 })).toBeInTheDocument()
    for (const side of ['BUY / BID', 'SELL / ASK']) {
      expect(screen.getByRole('heading', { name: side, level: 3 })).toBeInTheDocument()
      expect(screen.getByRole('region', { name: `${side} table scroll area` })).toHaveAttribute('tabindex', '0')
      for (const header of within(table(side)).getAllByRole('columnheader')) expect(header).toHaveAttribute('scope', 'col')
    }
    expect(within(table('BUY / BID')).getByRole('columnheader', { name: 'Remaining shares' })).toBeInTheDocument()
  })
  it('keeps zero rows and uses placeholders for malformed values', () => {
    render(<OrderBook data={{ ...populated, bids: [
      { price: 1, quantity: 0, orderCount: 0 },
      { price: NaN, quantity: 1.5, orderCount: -1 },
    ], asks: [] }} />)
    expect(cells('BUY / BID')).toEqual([['$0.01', '0 shares', '0'], ['—', '—', '—']])
  })
  it('renders with no network or backend', () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Network forbidden'))
    try {
      render(<OrderBook data={populated} />)
      expect(cells('BUY / BID')).toHaveLength(2)
      expect(fetch).not.toHaveBeenCalled()
    } finally { fetch.mockRestore() }
  })
})
