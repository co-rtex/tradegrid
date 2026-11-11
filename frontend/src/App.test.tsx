import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import seed from '../../docs/fixtures/00-seed.json'
import noFill from '../../docs/fixtures/01-no-fill.json'
import App from './App'
import { SELECTED_ACCOUNT_KEY } from './hooks/useDemoAccounts'
import { mockSocketFactory } from './test/mockWebSocket'

let sockets = mockSocketFactory()

// The app reads the API through the global fetch; answer as a server that
// already holds Alice's resting SELL, so her available cash is unchanged and
// her AAPL shares are partly reserved.
const responses: Record<string, unknown> = {
  '/api/v1/symbols': seed.after['GET /api/v1/symbols'],
  '/api/v1/accounts': noFill.after['GET /api/v1/accounts'],
  '/api/v1/orders?userId=alice': noFill.after['GET /api/v1/orders?userId=alice'],
  '/api/v1/orders?userId=bob': noFill.after['GET /api/v1/orders?userId=bob'],
  '/api/v1/orderbook?symbol=AAPL': noFill.after['GET /api/v1/orderbook?symbol=AAPL'],
  '/api/v1/executions?symbol=AAPL': noFill.after['GET /api/v1/executions?symbol=AAPL'],
}

function serve(overrides: Record<string, () => Response> = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (overrides[url]) return overrides[url]()
      const body = responses[url]
      return body === undefined
        ? new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'no such route' } }), { status: 404 })
        : new Response(JSON.stringify(body), { status: 200 })
    }),
  )
}

beforeEach(() => {
  sockets = mockSocketFactory()
  window.sessionStorage.clear()
  serve()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('starts on Dashboard and follows navigation clicks', async () => {
    const user = userEvent.setup()
    render(<App realtime={{ createSocket: sockets.create }} />)
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
    render(<App realtime={{ createSocket: sockets.create }} />)
    const balance = screen.getByText('Balance').parentElement!
    expect(balance).toHaveTextContent('—')
    expect(await within(balance).findByText('$100,000.00 available')).toBeInTheDocument()
  })

  it('keeps the sample portfolio card off the live desk and labels it as sample data', async () => {
    const user = userEvent.setup()
    render(<App realtime={{ createSocket: sockets.create }} />)
    await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })
    expect(screen.queryByRole('region', { name: 'Portfolio summary' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Portfolio' }))
    expect(screen.getByRole('region', { name: 'Portfolio summary' })).toBeInTheDocument()
    expect(screen.getByText('Design preview with sample values.')).toBeVisible()
    expect(screen.getByText(/not connected to your demo account/)).toBeVisible()
    // Only the active view is mounted, and the desk's balance is not left in
    // the navigation bar.
    expect(screen.queryByRole('region', { name: 'Trading desk' })).not.toBeInTheDocument()
    expect(screen.getByText('Balance').parentElement).toHaveTextContent('—')
  })

  it('loads the demo accounts once for the whole app and keeps the selection across views', async () => {
    const user = userEvent.setup()
    render(<App realtime={{ createSocket: sockets.create }} />)
    await user.selectOptions(await screen.findByLabelText('Demo account'), 'bob')
    expect(await screen.findByRole('heading', { name: 'Orders for Bob (demo)' })).toBeInTheDocument()
    expect(window.sessionStorage.getItem(SELECTED_ACCOUNT_KEY)).toBe('bob')

    await user.click(screen.getByRole('button', { name: 'Portfolio' }))
    await user.click(screen.getByRole('button', { name: 'Dashboard' }))
    expect(await screen.findByRole('heading', { name: 'Orders for Bob (demo)' })).toBeInTheDocument()
    expect(screen.getByLabelText('Demo account')).toHaveValue('bob')
  })

  it('restores this tab’s account after a reload and ignores an unknown remembered account', async () => {
    window.sessionStorage.setItem(SELECTED_ACCOUNT_KEY, 'bob')
    const { unmount } = render(<App realtime={{ createSocket: sockets.create }} />)
    expect(await screen.findByRole('heading', { name: 'Orders for Bob (demo)' })).toBeInTheDocument()
    unmount()

    window.sessionStorage.setItem(SELECTED_ACCOUNT_KEY, 'carol')
    render(<App realtime={{ createSocket: sockets.create }} />)
    expect(await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })).toBeInTheDocument()
    expect(screen.getByLabelText('Demo account')).toHaveValue('alice')
  })

  it('explains a failed account load and retries on request', async () => {
    let fail = true
    serve({
      '/api/v1/accounts': () =>
        fail ? new Response('', { status: 502 }) : new Response(JSON.stringify(responses['/api/v1/accounts'])),
    })
    const user = userEvent.setup()
    render(<App realtime={{ createSocket: sockets.create }} />)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Could not load the demo accounts: the server is unavailable (HTTP 502).')
    expect(alert).toHaveTextContent('Check that the backend is running')
    expect(screen.queryByRole('region', { name: 'Trading desk' })).not.toBeInTheDocument()

    fail = false
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })).toBeInTheDocument()
  })

  it('shows an empty state when the server lists no demo accounts', async () => {
    serve({ '/api/v1/accounts': () => new Response(JSON.stringify({ accounts: [] })) })
    render(<App realtime={{ createSocket: sockets.create }} />)
    expect(
      await screen.findByText('The server returned no demo accounts, so there is nothing to show.'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Trading desk' })).not.toBeInTheDocument()
  })
})
