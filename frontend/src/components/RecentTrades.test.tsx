import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import RecentTrades, { type Execution } from './RecentTrades'
import seed from '../../../docs/fixtures/00-seed.json'
import noFill from '../../../docs/fixtures/01-no-fill.json'
import fullFill from '../../../docs/fixtures/02-full-fill.json'
import partialFill from '../../../docs/fixtures/03-partial-fill.json'
import rejection from '../../../docs/fixtures/04-risk-rejection.json'

const executionsKey = 'GET /api/v1/executions?symbol=AAPL'
const execution: Execution = fullFill.after[executionsKey].executions[0]
function cells() {
  return within(screen.getByRole('table', { name: 'Recent executions · newest first' })).getAllByRole('row').slice(1).map(row =>
    within(row).getAllByRole('cell').map(cell => cell.textContent))
}

describe('RecentTrades', () => {
  it.each([seed, noFill, rejection])('shows no executions for $name', (fixture) => {
    render(<RecentTrades executions={fixture.after[executionsKey].executions} />)
    expect(screen.getByText('No recent trades.')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
  it.each([fullFill, partialFill])('shows the real execution for $name', (fixture) => {
    render(<RecentTrades executions={fixture.after[executionsKey].executions} />)
    expect(cells()).toEqual([['Oct 20, 2026, 3:00:02 PM UTC', 'AAPL', '$190.00', '10 shares']])
  })
  it('preserves sequence order with equal or out-of-order timestamps and frozen props', () => {
    const executions = [
      { ...execution, id: 'exe-3', sequence: 3, price: 19001, quantity: 1 },
      { ...execution, id: 'exe-2', sequence: 2, price: 19002, quantity: 1000 },
      { ...execution, id: 'exe-1', sequence: 1, price: 19003, executedAt: '2026-10-20T16:00:00Z', symbol: 'MSFT' },
    ]
    const original = structuredClone(executions)
    executions.forEach(Object.freeze)
    Object.freeze(executions)
    render(<RecentTrades executions={executions} />)
    expect(cells().map(row => row[2])).toEqual(['$190.01', '$190.02', '$190.03'])
    expect(cells()[0][3]).toBe('1 share')
    expect(cells()[1][3]).toBe('1,000 shares')
    expect(cells()[2][1]).toBe('MSFT')
    expect(executions).toEqual(original)
  })
  it('gives error precedence over loading and executions, then loading over executions or empty state', () => {
    const { rerender } = render(<RecentTrades executions={[execution]} isLoading error="Trades unavailable" />)
    expect(screen.getByRole('alert')).toHaveTextContent('Trades unavailable')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    rerender(<RecentTrades executions={[execution]} isLoading />)
    expect(screen.getByRole('status')).toHaveTextContent('Loading recent trades…')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    rerender(<RecentTrades executions={[]} isLoading />)
    expect(screen.queryByText('No recent trades.')).not.toBeInTheDocument()
    rerender(<RecentTrades executions={[]} error="" />)
    expect(screen.getByText('No recent trades.')).toBeInTheDocument()
  })
  it('provides semantic headings and scoped columns without inventing an execution side', () => {
    render(<RecentTrades executions={[execution]} />)
    expect(screen.getByRole('heading', { name: 'Recent trades', level: 2 })).toBeInTheDocument()
    const headers = screen.getAllByRole('columnheader')
    expect(headers.map(header => header.textContent)).toEqual(['Execution time (UTC)', 'Symbol', 'Price (USD)', 'Quantity (shares)'])
    headers.forEach(header => expect(header).toHaveAttribute('scope', 'col'))
    expect(screen.getByRole('region', { name: 'Recent trades table scroll area' })).toHaveAttribute('tabindex', '0')
    expect(screen.queryByText(/buy|sell|aggressor|bid|ask/i)).not.toBeInTheDocument()
  })
  it('formats cents and zero shares without dropping rows; malformed display values are placeholders', () => {
    render(<RecentTrades executions={[
      { ...execution, price: 1, quantity: 0 },
      { ...execution, id: 'invalid', price: Infinity, quantity: -1, executedAt: 'invalid' },
    ]} />)
    expect(cells()).toEqual([
      ['Oct 20, 2026, 3:00:02 PM UTC', 'AAPL', '$0.01', '0 shares'],
      ['—', 'AAPL', '—', '—'],
    ])
  })
  it('renders with no network or backend', () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Network forbidden'))
    try {
      render(<RecentTrades executions={[execution]} />)
      expect(cells()).toHaveLength(1)
      expect(fetch).not.toHaveBeenCalled()
    } finally { fetch.mockRestore() }
  })
})
