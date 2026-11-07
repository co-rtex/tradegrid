import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RecentTrades, { type ExecutionDto, type RecentTradesProps } from './RecentTrades'

// exe-1 from GET /api/v1/executions?symbol=AAPL in docs/fixtures/02-full-fill.json
// (also 03-partial-fill.json): Bob's buy fills 10 at Alice's resting $190.00.
// Expected text is written out here so the tests do not depend on the
// formatters they check.
const fixtureExecution: ExecutionDto = {
  id: 'exe-1',
  sequence: 1,
  executedAt: '2026-10-20T15:00:02Z',
  symbol: 'AAPL',
  price: 19000,
  quantity: 10,
  buyOrderId: 'ord-2',
  sellOrderId: 'ord-1',
  buyerUserId: 'bob',
  sellerUserId: 'alice',
}

function makeExecution(overrides: Partial<ExecutionDto> = {}): ExecutionDto {
  return { ...fixtureExecution, ...overrides }
}

// Newest first (descending sequence), as the contract says the server sends
// them. Two trades share a timestamp, which the contract expects.
const tape: ExecutionDto[] = [
  makeExecution({ id: 'exe-3', sequence: 3, executedAt: '2026-10-20T15:00:04Z', price: 19100, quantity: 1 }),
  makeExecution({ id: 'exe-2', sequence: 2, executedAt: '2026-10-20T15:00:03Z', price: 19050, quantity: 15 }),
  makeExecution({ id: 'exe-9', sequence: 9, executedAt: '2026-10-20T15:00:03Z', price: 19000, quantity: 5 }),
  fixtureExecution,
]

function renderTrades(props: Partial<RecentTradesProps> = {}) {
  return render(<RecentTrades symbol="AAPL" executions={[fixtureExecution]} {...props} />)
}

function rowTexts() {
  // slice(1) skips the header row
  return within(screen.getByRole('table'))
    .getAllByRole('row')
    .slice(1)
    .map((row) => within(row).getAllByRole('cell').map((cell) => cell.textContent))
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecentTrades', () => {
  it('shows the fixture execution time, price, and quantity', () => {
    renderTrades()

    expect(rowTexts()).toEqual([['Oct 20, 2026, 3:00:02 PM UTC', '$190.00', '10 shares']])
  })

  it('keeps the supplied order rather than sorting by time or sequence', () => {
    renderTrades({ executions: tape })

    expect(rowTexts()).toEqual([
      ['Oct 20, 2026, 3:00:04 PM UTC', '$191.00', '1 share'],
      ['Oct 20, 2026, 3:00:03 PM UTC', '$190.50', '15 shares'],
      ['Oct 20, 2026, 3:00:03 PM UTC', '$190.00', '5 shares'],
      ['Oct 20, 2026, 3:00:02 PM UTC', '$190.00', '10 shares'],
    ])
    expect(tape.map((execution) => execution.id)).toEqual(['exe-3', 'exe-2', 'exe-9', 'exe-1'])
  })

  it.each([
    [1, '$0.01'],
    [19050, '$190.50'],
    [100000000, '$1,000,000.00'],
  ])('formats a price of %d cents as %s', (price, text) => {
    renderTrades({ executions: [makeExecution({ price })] })

    expect(rowTexts()[0][1]).toBe(text)
  })

  it('formats large quantities with grouping', () => {
    renderTrades({ executions: [makeExecution({ quantity: 1000000 })] })

    expect(rowTexts()[0][2]).toBe('1,000,000 shares')
  })

  it('accepts timestamps with fractional seconds', () => {
    renderTrades({ executions: [makeExecution({ executedAt: '2026-10-20T15:00:02.123Z' })] })

    expect(rowTexts()[0][0]).toBe('Oct 20, 2026, 3:00:02 PM UTC')
  })

  it('labels the heading, table, and columns', () => {
    renderTrades()

    expect(screen.getByRole('heading', { level: 2, name: 'AAPL recent trades' })).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'AAPL recent trades' })
    const headers = within(table).getAllByRole('columnheader')
    expect(headers.map((header) => header.textContent)).toEqual(['Time', 'Price (USD)', 'Quantity'])
    for (const header of headers) {
      expect(header).toHaveAttribute('scope', 'col')
    }
    expect(screen.getByText('Newest first.')).toBeInTheDocument()
  })

  it('keeps the table scrollable from the keyboard', () => {
    renderTrades()

    expect(screen.getByRole('group', { name: 'AAPL recent trades' })).toHaveAttribute('tabindex', '0')
  })

  it.each([
    ['a malformed timestamp', { executedAt: 'not-a-time' }, 0, 'not-a-time'],
    ['a timestamp without a UTC suffix', { executedAt: '2026-10-20T15:00:02' }, 0, '2026-10-20T15:00:02'],
    ['fractional cents', { price: 1.5 }, 1, 1.5],
    ['fractional shares', { quantity: 1.5 }, 2, 1.5],
    ['negative shares', { quantity: -1 }, 2, -1],
  ])('shows a placeholder for %s rather than a misleading value', (_name, overrides, column, logged) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    renderTrades({ executions: [makeExecution(overrides)] })

    expect(rowTexts()[0][column]).toBe('—')
    expect(warn).toHaveBeenCalledWith('RecentTrades: could not format value', logged)
  })

  it('shows an empty state with no table when there are no trades', () => {
    renderTrades({ executions: [] })

    expect(screen.getByText('No trades yet for AAPL.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('announces loading and shows no trades, even when old trades are supplied', () => {
    renderTrades({ isLoading: true })

    expect(screen.getByRole('status')).toHaveTextContent('Loading recent trades…')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('announces an error and shows no trades, even when old trades are supplied', () => {
    renderTrades({ error: 'Server unavailable' })

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load recent trades: Server unavailable')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('No trades yet for AAPL.')).not.toBeInTheDocument()
  })

  it('gives each instance its own heading association', () => {
    render(
      <>
        <RecentTrades symbol="AAPL" executions={[fixtureExecution]} />
        <RecentTrades symbol="MSFT" executions={[makeExecution({ symbol: 'MSFT' })]} />
      </>,
    )

    expect(screen.getByRole('table', { name: 'AAPL recent trades' })).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'MSFT recent trades' })).toBeInTheDocument()
  })
})
