import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PositionsTable, { type PortfolioPositionDto, type PositionsTableProps } from './PositionsTable'

// Positions from "Reconciliation after the examples" in docs/portfolio.md.
// Bob's 4 reserved AAPL shares are the unfilled part of his resting order from
// example 3. Amounts are written out here so the tests do not depend on the
// formatters they check.
const aliceAapl: PortfolioPositionDto = {
  symbol: 'AAPL',
  total: 88,
  reserved: 0,
  available: 88,
  costBasis: 1672900,
  referencePrice: 19000,
  marketValue: 1672000,
  unrealizedPnl: -900,
  realizedPnl: -6000,
}
const aliceMsftClosed: PortfolioPositionDto = {
  symbol: 'MSFT',
  total: 0,
  reserved: 0,
  available: 0,
  costBasis: 0,
  referencePrice: 42000,
  marketValue: 0,
  unrealizedPnl: 0,
  realizedPnl: -10000,
}
const bobAapl: PortfolioPositionDto = {
  symbol: 'AAPL',
  total: 112,
  reserved: 4,
  available: 108,
  costBasis: 2122157,
  referencePrice: 19000,
  marketValue: 2128000,
  unrealizedPnl: 5843,
  realizedPnl: 1057,
}

function renderTable(props: Partial<PositionsTableProps> = {}) {
  return render(<PositionsTable positions={[]} {...props} />)
}

function bodyRows() {
  // slice(1) skips the header row
  return within(screen.getByRole('table')).getAllByRole('row').slice(1)
}

function rowTexts(row: HTMLElement) {
  return [within(row).getByRole('rowheader'), ...within(row).getAllByRole('cell')].map(
    (cell) => cell.textContent,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('PositionsTable', () => {
  it('shows every contracted column for a position', () => {
    renderTable({ positions: [bobAapl] })

    expect(rowTexts(bodyRows()[0])).toEqual([
      'AAPL',
      '112 shares',
      '4 shares',
      '108 shares',
      '$190.00',
      '$21,280.00',
      '$21,221.57',
      '+$58.43 gain',
    ])
  })

  it('labels columns and rows for assistive technology', () => {
    renderTable({ positions: [bobAapl] })

    const table = screen.getByRole('table', { name: 'Positions' })
    expect(within(table).getAllByRole('columnheader').map((header) => header.textContent)).toEqual([
      'Symbol',
      'Total',
      'Reserved',
      'Available',
      'Reference price',
      'Market value',
      'Cost basis',
      'Unrealized P&L',
    ])
    expect(within(table).getByRole('rowheader', { name: 'AAPL' })).toHaveAttribute('scope', 'row')
    for (const header of within(table).getAllByRole('columnheader')) {
      expect(header).toHaveAttribute('scope', 'col')
    }
  })

  it('keeps the table scrollable from the keyboard', () => {
    renderTable({ positions: [bobAapl] })

    expect(screen.getByRole('group', { name: 'Positions' })).toHaveAttribute('tabindex', '0')
  })

  it.each([
    ['positive', 5843, '+$58.43', 'gain'],
    ['negative', -900, '-$9.00', 'loss'],
    ['zero', 0, '$0.00', 'no gain or loss'],
    ['negative zero', -0, '$0.00', 'no gain or loss'],
  ])('makes %s P&L understandable without color', (_name, cents, amount, spoken) => {
    renderTable({ positions: [{ ...bobAapl, unrealizedPnl: cents }] })

    expect(rowTexts(bodyRows()[0])[7]).toBe(`${amount} ${spoken}`)
  })

  it('keeps the supplied row order rather than sorting', () => {
    const positions = [aliceMsftClosed, aliceAapl]
    renderTable({ positions })

    expect(bodyRows().map((row) => rowTexts(row)[0])).toEqual(['MSFT', 'AAPL'])
    expect(positions.map((position) => position.symbol)).toEqual(['MSFT', 'AAPL'])
  })

  it('shows a closed position with zero values instead of hiding it', () => {
    renderTable({ positions: [aliceMsftClosed] })

    expect(rowTexts(bodyRows()[0])).toEqual([
      'MSFT',
      '0 shares',
      '0 shares',
      '0 shares',
      '$420.00',
      '$0.00',
      '$0.00',
      '$0.00 no gain or loss',
    ])
  })

  it('uses the singular for one share', () => {
    renderTable({ positions: [{ ...bobAapl, total: 1, reserved: 1, available: 0 }] })

    expect(rowTexts(bodyRows()[0]).slice(1, 4)).toEqual(['1 share', '1 share', '0 shares'])
  })

  it('formats the largest contracted amounts exactly', () => {
    const max = Number.MAX_SAFE_INTEGER
    renderTable({ positions: [{ ...bobAapl, marketValue: max, costBasis: max, unrealizedPnl: -max }] })

    const texts = rowTexts(bodyRows()[0])
    expect(texts[5]).toBe('$90,071,992,547,409.91')
    expect(texts[6]).toBe('$90,071,992,547,409.91')
    expect(texts[7]).toBe('-$90,071,992,547,409.91 loss')
  })

  it.each([
    ['fractional cents', { marketValue: 1.5 }, 5],
    ['not a number', { costBasis: Number.NaN }, 6],
    ['fractional shares', { total: 1.5 }, 1],
    ['negative shares', { reserved: -1 }, 2],
    ['unsafe P&L', { unrealizedPnl: Number.MAX_SAFE_INTEGER + 1 }, 7],
  ])('shows a placeholder for %s rather than a misleading value', (_name, overrides, column) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    renderTable({ positions: [{ ...bobAapl, ...overrides }] })

    expect(rowTexts(bodyRows()[0])[column]).toBe('—')
    expect(warn).toHaveBeenCalled()
  })

  it('explains that values are simulated', () => {
    renderTable({ positions: [bobAapl] })

    expect(screen.getByText(/simulated reference prices, not live market prices/)).toBeInTheDocument()
  })

  it('shows an empty state with no table when there are no positions', () => {
    renderTable({ positions: [] })

    expect(screen.getByText('No positions to show.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('announces loading and shows no values, even when old positions are supplied', () => {
    renderTable({ positions: [bobAapl], isLoading: true })

    expect(screen.getByRole('status')).toHaveTextContent('Loading positions…')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('announces an error and shows no values, even when old positions are supplied', () => {
    renderTable({ positions: [bobAapl], error: 'Server unavailable' })

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load positions: Server unavailable')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.queryByText('No positions to show.')).not.toBeInTheDocument()
  })

  it('gives each instance its own heading association', () => {
    render(
      <>
        <PositionsTable positions={[bobAapl]} />
        <PositionsTable positions={[aliceAapl]} />
      </>,
    )

    const [first, second] = screen.getAllByRole('heading', { name: 'Positions' })
    expect(first.id).not.toBe(second.id)
    expect(screen.getAllByRole('table', { name: 'Positions' })).toHaveLength(2)
  })
})
