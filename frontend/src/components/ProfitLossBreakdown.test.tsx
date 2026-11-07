import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProfitLossBreakdown, { type ProfitLossBreakdownProps } from './ProfitLossBreakdown'

// Alice's totals from "Reconciliation after the examples" in docs/portfolio.md:
// AAPL -6000 and MSFT -10000 realized, -900 unrealized, -16900 combined.
const alice = { realizedPnl: -16000, unrealizedPnl: -900, combinedPnl: -16900 }
// Bob's: 1057 realized, 5843 + 10000 unrealized, 16900 combined.
const bob = { realizedPnl: 1057, unrealizedPnl: 15843, combinedPnl: 16900 }

function renderBreakdown(props: ProfitLossBreakdownProps = {}) {
  return render(<ProfitLossBreakdown {...props} />)
}

// The value shown under a label. Each row is one dt, a value dd, and a helper dd.
function valueOf(label: string) {
  const term = screen.getByText(label)
  return within(term.parentElement!).getAllByRole('definition')[0].textContent
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('ProfitLossBreakdown', () => {
  it('shows gains with a plus sign and a word', () => {
    renderBreakdown(bob)

    expect(valueOf('Realized P&L')).toBe('+$10.57 Gain')
    expect(valueOf('Unrealized P&L')).toBe('+$158.43 Gain')
    expect(valueOf('Combined P&L')).toBe('+$169.00 Gain')
  })

  it('shows losses with a minus sign and a word', () => {
    renderBreakdown(alice)

    expect(valueOf('Realized P&L')).toBe('-$160.00 Loss')
    expect(valueOf('Unrealized P&L')).toBe('-$9.00 Loss')
    expect(valueOf('Combined P&L')).toBe('-$169.00 Loss')
  })

  it.each([
    ['zero', 0],
    ['negative zero', -0],
  ])('shows genuine %s as $0.00 with no direction', (_name, cents) => {
    renderBreakdown({ realizedPnl: cents, unrealizedPnl: cents, combinedPnl: cents })

    expect(valueOf('Realized P&L')).toBe('$0.00 No change')
    expect(valueOf('Unrealized P&L')).toBe('$0.00 No change')
    expect(valueOf('Combined P&L')).toBe('$0.00 No change')
  })

  it('formats the largest contracted amounts exactly', () => {
    renderBreakdown({
      realizedPnl: Number.MAX_SAFE_INTEGER,
      unrealizedPnl: -Number.MAX_SAFE_INTEGER,
      combinedPnl: 0,
    })

    expect(valueOf('Realized P&L')).toBe('+$90,071,992,547,409.91 Gain')
    expect(valueOf('Unrealized P&L')).toBe('-$90,071,992,547,409.91 Loss')
  })

  it('displays the supplied combined amount without doing any arithmetic', () => {
    renderBreakdown({ realizedPnl: 100, unrealizedPnl: 200, combinedPnl: 999 })

    expect(valueOf('Combined P&L')).toBe('+$9.99 Gain')
  })

  it('does not calculate a missing combined amount from the others', () => {
    renderBreakdown({ realizedPnl: 100, unrealizedPnl: 200 })

    expect(valueOf('Realized P&L')).toBe('+$1.00 Gain')
    expect(valueOf('Combined P&L')).toBe('Unavailable')
  })

  it.each([
    ['undefined', {}],
    ['null', { realizedPnl: null, unrealizedPnl: null, combinedPnl: null }],
  ])('shows %s amounts as unavailable and never as zero', (_name, props) => {
    renderBreakdown(props)

    expect(valueOf('Realized P&L')).toBe('Unavailable')
    expect(valueOf('Unrealized P&L')).toBe('Unavailable')
    expect(valueOf('Combined P&L')).toBe('Unavailable')
    expect(screen.queryByText(/\$0\.00/)).not.toBeInTheDocument()
  })

  it.each([
    ['fractional cents', 1.5],
    ['not a number', Number.NaN],
    ['infinity', Number.POSITIVE_INFINITY],
    ['unsafe integers', Number.MAX_SAFE_INTEGER + 1],
  ])('shows %s as unavailable rather than a misleading amount', (_name, cents) => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    renderBreakdown({ ...bob, realizedPnl: cents })

    expect(valueOf('Realized P&L')).toBe('Unavailable')
    expect(valueOf('Unrealized P&L')).toBe('+$158.43 Gain')
    expect(warn).toHaveBeenCalled()
  })

  it('shows loading as distinct from zero and hides any old amounts', () => {
    renderBreakdown({ ...bob, isLoading: true })

    expect(screen.getByRole('status')).toHaveTextContent('Loading profit and loss…')
    expect(screen.getByRole('region', { name: 'Profit and loss' })).toHaveAttribute('aria-busy', 'true')
    expect(valueOf('Realized P&L')).toBe('Loading…')
    expect(valueOf('Combined P&L')).toBe('Loading…')
    expect(screen.queryByText(/\$/)).not.toBeInTheDocument()
  })

  it('keeps its live region in place so the loading message is announced', () => {
    const { rerender } = renderBreakdown(bob)
    const region = screen.getByRole('status')
    expect(region).toBeEmptyDOMElement()

    rerender(<ProfitLossBreakdown {...bob} isLoading />)

    expect(screen.getByRole('status')).toBe(region)
    expect(region).toHaveTextContent('Loading profit and loss…')
  })

  it('announces an error and does not show amounts that may be out of date', () => {
    renderBreakdown({ ...bob, error: 'Server unavailable' })

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load profit and loss: Server unavailable')
    expect(valueOf('Realized P&L')).toBe('Unavailable')
    expect(valueOf('Combined P&L')).toBe('Unavailable')
    expect(screen.queryByText(/\$/)).not.toBeInTheDocument()
  })

  it('prefers loading over an old error during a retry', () => {
    renderBreakdown({ error: 'Server unavailable', isLoading: true })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(valueOf('Realized P&L')).toBe('Loading…')
  })

  it('explains each amount and that prices are simulated', () => {
    renderBreakdown(bob)

    expect(screen.getByText(/shares already sold/)).toBeInTheDocument()
    expect(screen.getByText(/simulated reference prices, not live prices/)).toBeInTheDocument()
    expect(screen.getByText('Realized plus unrealized P&L.')).toBeInTheDocument()
  })

  it('labels the section and its amounts for assistive technology', () => {
    renderBreakdown(bob)

    expect(screen.getByRole('region', { name: 'Profit and loss' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Profit and loss' })).toBeInTheDocument()
    expect(screen.getAllByRole('term').map((term) => term.textContent)).toEqual([
      'Realized P&L',
      'Unrealized P&L',
      'Combined P&L',
    ])
  })
})
