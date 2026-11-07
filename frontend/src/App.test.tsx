import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import seed from '../../docs/fixtures/00-seed.json'
import noFill from '../../docs/fixtures/01-no-fill.json'
import App from './App'

// The desk reads the API through the global fetch; answer as a server that
// already holds Alice's resting SELL, so her available cash is unchanged and
// her AAPL shares are partly reserved.
const responses: Record<string, unknown> = {
  '/api/v1/symbols': seed.after['GET /api/v1/symbols'],
  '/api/v1/accounts': noFill.after['GET /api/v1/accounts'],
  '/api/v1/orders?userId=alice': noFill.after['GET /api/v1/orders?userId=alice'],
  '/api/v1/orderbook?symbol=AAPL': noFill.after['GET /api/v1/orderbook?symbol=AAPL'],
  '/api/v1/executions?symbol=AAPL': noFill.after['GET /api/v1/executions?symbol=AAPL'],
}

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const body = responses[String(input)]
      return body === undefined
        ? new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'no such route' } }), { status: 404 })
        : new Response(JSON.stringify(body), { status: 200 })
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('starts on Dashboard and follows navigation clicks', async () => {
    const user = userEvent.setup()
    render(<App />)
    const nav = screen.getByRole('navigation', { name: 'Primary navigation' })

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Dashboard')
    expect(await screen.findByRole('region', { name: 'Trading desk' })).toBeVisible()

    await user.click(within(nav).getByRole('button', { name: 'Orders' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Orders')
    expect(within(nav).getByRole('button', { name: 'Orders' })).toHaveAttribute('aria-current', 'page')
    expect(within(nav).getByRole('button', { name: 'Dashboard' })).not.toHaveAttribute('aria-current')

    await user.click(screen.getByRole('button', { name: 'TradeGrid dashboard' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Dashboard')
  })

  it('shows the selected demo account’s server cash in the navigation balance', async () => {
    render(<App />)
    const balance = screen.getByText('Balance').parentElement!
    expect(balance).toHaveTextContent('—')
    expect(await within(balance).findByText('$100,000.00 available')).toBeInTheDocument()
  })

  it('keeps the sample portfolio card off the live desk and labels it as sample data', async () => {
    const user = userEvent.setup()
    render(<App />)
    await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })
    expect(screen.queryByRole('region', { name: 'Portfolio summary' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Portfolio' }))
    expect(screen.getByRole('region', { name: 'Portfolio summary' })).toBeInTheDocument()
    expect(screen.getByText('Design preview with sample values.')).toBeVisible()
    expect(screen.getByText(/not connected to your demo account/)).toBeVisible()
    expect(screen.queryByRole('region', { name: 'Trading desk' })).not.toBeInTheDocument()

    // The desk stays mounted (hidden) and does not reload on return.
    const fetchCalls = vi.mocked(fetch).mock.calls.length
    await user.click(screen.getByRole('button', { name: 'Dashboard' }))
    expect(screen.getByRole('region', { name: 'Trading desk' })).toBeVisible()
    expect(vi.mocked(fetch).mock.calls.length).toBe(fetchCalls)
  })
})
