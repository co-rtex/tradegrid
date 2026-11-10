import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, type PortfolioDto, type TradeGridClient } from '../api/client'
import type { DemoAccount } from '../hooks/useDemoAccounts'
import { changed, heartbeat, hello, mockSocketFactory } from '../test/mockWebSocket'
import PortfolioPage from './PortfolioPage'

// ---------------------------------------------------------------------------
// Fixtures from docs/portfolio.md

const position = (symbol: string, referencePrice: number, total: number, costBasis: number, realizedPnl = 0, reserved = 0) => ({
  symbol,
  total,
  reserved,
  available: total - reserved,
  costBasis,
  referencePrice,
  marketValue: total * referencePrice,
  unrealizedPnl: total * referencePrice - costBasis,
  realizedPnl,
})

function portfolio(userId: 'alice' | 'bob', overrides: Partial<PortfolioDto> = {}): PortfolioDto {
  const positions = overrides.positions ?? [position('AAPL', 19000, 100, 1900000), position('MSFT', 42000, 100, 4200000)]
  const cash = overrides.cash ?? { total: 10000000, reserved: 0, available: 10000000 }
  const sum = (key: 'costBasis' | 'marketValue' | 'unrealizedPnl' | 'realizedPnl') =>
    positions.reduce((total, p) => total + p[key], 0)
  return {
    userId,
    displayName: userId === 'alice' ? 'Alice (demo)' : 'Bob (demo)',
    asOfSequence: 0,
    cash,
    positions,
    totals: {
      costBasis: sum('costBasis'),
      marketValue: sum('marketValue'),
      unrealizedPnl: sum('unrealizedPnl'),
      realizedPnl: sum('realizedPnl'),
      equity: cash.total + sum('marketValue'),
    },
    ...overrides,
  }
}

/** Alice after worked example 1: sold 10 AAPL at $185.00. */
const aliceAfterExample1 = portfolio('alice', {
  asOfSequence: 2,
  cash: { total: 10185000, reserved: 0, available: 10185000 },
  positions: [position('AAPL', 19000, 90, 1710000, -5000), position('MSFT', 42000, 100, 4200000)],
})

/** Bob after worked example 1, with 4 AAPL reserved by a resting SELL. */
const bobAfterExample1 = portfolio('bob', {
  asOfSequence: 3,
  cash: { total: 9815000, reserved: 0, available: 9815000 },
  positions: [position('AAPL', 19000, 110, 2085000, 0, 4), position('MSFT', 42000, 100, 4200000)],
})

const directory: DemoAccount[] = [
  { userId: 'alice', displayName: 'Alice (demo)' },
  { userId: 'bob', displayName: 'Bob (demo)' },
]

function fakeClient(byUser: Record<string, PortfolioDto> = { alice: portfolio('alice'), bob: portfolio('bob') }) {
  const unused = async () => {
    throw new Error('the portfolio page reads only portfolios')
  }
  return {
    getSymbols: vi.fn(unused),
    getAccounts: vi.fn(unused),
    getOrders: vi.fn(unused),
    getOrderBook: vi.fn(unused),
    getExecutions: vi.fn(unused),
    submitOrder: vi.fn(unused),
    getPortfolio: vi.fn<TradeGridClient['getPortfolio']>(async (userId: string) => byUser[userId]),
  } satisfies TradeGridClient
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

/** Sockets opened in the current test; the test plays the server. */
let sockets = mockSocketFactory()
beforeEach(() => {
  sockets = mockSocketFactory()
})

/** The page as App mounts it: the account selection lives one level up. */
function Page({
  client,
  onBalanceChange,
  initialUser = 'alice',
  factory = sockets,
}: {
  client: TradeGridClient
  onBalanceChange?: (b: string | undefined) => void
  initialUser?: string
  factory?: ReturnType<typeof mockSocketFactory>
}) {
  const [userId, setUserId] = useState(initialUser)
  return (
    <PortfolioPage
      client={client}
      accounts={directory}
      userId={userId}
      onUserChange={setUserId}
      onBalanceChange={onBalanceChange}
      realtime={{ url: 'ws://test/api/v1/stream', createSocket: factory.create }}
    />
  )
}

const summary = () => within(screen.getByRole('region', { name: 'Portfolio summary' }))
const pnl = () => within(screen.getByRole('region', { name: 'Profit and loss' }))
const cash = () => within(screen.getByRole('region', { name: 'Cash' }))
const positionRow = (symbol: string) =>
  within(screen.getByRole('table', { name: 'Positions' })).getByRole('row', { name: new RegExp(`^${symbol}`) })

// ---------------------------------------------------------------------------

describe('PortfolioPage', () => {
  it('shows the seeded portfolio exactly as the server values it', async () => {
    const client = fakeClient()
    const onBalanceChange = vi.fn()
    render(<Page client={client} onBalanceChange={onBalanceChange} />)

    expect(screen.getByText('Loading the portfolio…')).toHaveAttribute('role', 'status')
    expect(screen.getByRole('heading', { name: 'Alice (demo) portfolio' })).toBeInTheDocument()
    expect(await screen.findByText('$161,000.00')).toBeInTheDocument()
    expect(summary().getByText('$100,000.00')).toBeInTheDocument()
    expect(positionRow('AAPL')).toHaveTextContent('100 shares')
    expect(positionRow('AAPL')).toHaveTextContent('$190.00')
    expect(positionRow('AAPL')).toHaveTextContent('$19,000.00')
    expect(positionRow('MSFT')).toHaveTextContent('$42,000.00')
    expect(pnl().getAllByText('No change')).toHaveLength(3)
    expect(screen.getByText(/Positions are valued at fixed, simulated reference prices/)).toBeInTheDocument()
    expect(screen.getByText(/As of exchange sequence 0\./)).toBeInTheDocument()
    expect(onBalanceChange).toHaveBeenLastCalledWith('$100,000.00 available')
    expect(client.getPortfolio).toHaveBeenCalledWith('alice', expect.anything())
  })

  it('shows realized losses, reserved shares, and equity after trades without recomputing them', async () => {
    const client = fakeClient({ alice: aliceAfterExample1, bob: bobAfterExample1 })
    const user = userEvent.setup()
    render(<Page client={client} />)

    expect(await screen.findByText('$160,950.00')).toBeInTheDocument()
    expect(pnl().getByText('Realized P&L').nextElementSibling).toHaveTextContent('-$50.00 Loss')
    expect(pnl().getByText('Unrealized P&L').nextElementSibling).toHaveTextContent('$0.00 No change')
    expect(pnl().getByText('Combined P&L').nextElementSibling).toHaveTextContent('-$50.00 Loss')
    expect(positionRow('AAPL')).toHaveTextContent('90 shares')
    expect(positionRow('AAPL')).toHaveTextContent('$17,100.00')

    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    expect(screen.getByRole('heading', { name: 'Bob (demo) portfolio' })).toBeInTheDocument()
    expect(await screen.findByText('$161,050.00')).toBeInTheDocument()
    expect(pnl().getByText('Unrealized P&L').nextElementSibling).toHaveTextContent('+$50.00 Gain')
    // 4 of Bob's 110 AAPL are reserved by a resting SELL and are still valued.
    expect(positionRow('AAPL')).toHaveTextContent(/110 shares\s*4 shares\s*106 shares/)
    expect(cash().getByText('Reserved by open BUY orders').nextElementSibling).toHaveTextContent('$0.00')
  })

  it('never shows the previous account’s values under the new one, even when its response is late', async () => {
    const client = fakeClient()
    const lateAlice = deferred<PortfolioDto>()
    const bob = deferred<PortfolioDto>()
    client.getPortfolio.mockImplementationOnce(() => lateAlice.promise).mockImplementationOnce(() => bob.promise)
    const user = userEvent.setup()
    render(<Page client={client} />)

    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    expect(screen.getByRole('heading', { name: 'Bob (demo) portfolio' })).toBeInTheDocument()
    lateAlice.resolve(aliceAfterExample1)
    await waitFor(() => expect(client.getPortfolio).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('region', { name: 'Portfolio summary' })).not.toBeInTheDocument()
    expect(screen.getByText('Loading the portfolio…')).toBeInTheDocument()

    bob.resolve(bobAfterExample1)
    expect(await screen.findByText('$161,050.00')).toBeInTheDocument()
    expect(screen.queryByText('$160,950.00')).not.toBeInTheDocument()
  })

  it('clears the previous account’s values as soon as the selection changes', async () => {
    const client = fakeClient({ alice: aliceAfterExample1, bob: bobAfterExample1 })
    const pendingBob = deferred<PortfolioDto>()
    const user = userEvent.setup()
    render(<Page client={client} />)
    await screen.findByText('$160,950.00')

    client.getPortfolio.mockImplementationOnce(() => pendingBob.promise)
    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    expect(screen.queryByText('$160,950.00')).not.toBeInTheDocument()
    expect(screen.getByText('Loading the portfolio…')).toBeInTheDocument()
    pendingBob.resolve(bobAfterExample1)
    expect(await screen.findByText('$161,050.00')).toBeInTheDocument()
  })

  it('refreshes on request, keeping current values visible until the new ones arrive', async () => {
    const client = fakeClient()
    const user = userEvent.setup()
    render(<Page client={client} />)
    await screen.findByText('$161,000.00')

    const next = deferred<PortfolioDto>()
    client.getPortfolio.mockImplementationOnce(() => next.promise)
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(screen.getByText('Refreshing from the server…')).toBeInTheDocument()
    expect(summary().getByText('$161,000.00')).toBeInTheDocument()

    next.resolve(aliceAfterExample1)
    expect(await screen.findByText('$160,950.00')).toBeInTheDocument()
    expect(screen.queryByText('Refreshing from the server…')).not.toBeInTheDocument()
  })

  it('explains a failure without showing old values, and recovers on Refresh', async () => {
    const client = fakeClient()
    const user = userEvent.setup()
    render(<Page client={client} />)
    await screen.findByText('$161,000.00')

    client.getPortfolio.mockRejectedValueOnce(new ApiError('network', 'GET failed'))
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Could not load the portfolio: the server could not be reached.')
    expect(alert).toHaveTextContent('Check that the backend is running')
    expect(screen.queryByRole('region', { name: 'Portfolio summary' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('$161,000.00')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a contracted server error with its code', async () => {
    const client = fakeClient()
    client.getPortfolio.mockRejectedValue(new ApiError('http', 'internal error', { status: 500, code: 'INTERNAL' }))
    render(<Page client={client} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the portfolio: internal error (INTERNAL).')
  })

  it('shows the empty state when the account holds no shares', async () => {
    const empty = portfolio('alice', {
      cash: { total: 16100000, reserved: 0, available: 16100000 },
      positions: [position('AAPL', 19000, 0, 0, 1000), position('MSFT', 42000, 0, 0, -1000)],
    })
    render(<Page client={fakeClient({ alice: empty })} />)
    expect(await screen.findByText('No positions yet')).toBeInTheDocument()
    // A closed position keeps its realized P&L.
    expect(pnl().getByText('Realized P&L').nextElementSibling).toHaveTextContent('$0.00 No change')
    expect(positionRow('AAPL')).toHaveTextContent('0 shares')
  })
})

// ---------------------------------------------------------------------------
// Live refresh (#96)

describe('PortfolioPage live refresh', () => {
  const server = (fn: () => void) => act(fn)

  async function renderLive(client: ReturnType<typeof fakeClient>) {
    const user = userEvent.setup()
    render(<Page client={client} />)
    await screen.findByRole('region', { name: 'Portfolio summary' })
    server(() => sockets.latest().send(hello(0)))
    // hello re-reads the portfolio once.
    await waitFor(() => expect(client.getPortfolio).toHaveBeenCalledTimes(2))
    return user
  }

  it('re-reads the portfolio when a committed trade involves the selected account', async () => {
    const byUser = { alice: portfolio('alice'), bob: portfolio('bob') }
    const client = fakeClient(byUser)
    await renderLive(client)
    expect(sockets.sockets).toHaveLength(1)
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connected')

    byUser.alice = aliceAfterExample1
    server(() => sockets.latest().send(changed(1, { userId: 'bob', affectedUserIds: ['alice', 'bob'] })))
    expect(await screen.findByText('$160,950.00')).toBeInTheDocument()
    expect(client.getPortfolio).toHaveBeenLastCalledWith('alice', expect.anything())
  })

  it('ignores trades that do not involve the selected account, whatever their symbol', async () => {
    const client = fakeClient()
    await renderLive(client)
    server(() => sockets.latest().send(changed(1, { symbol: 'AAPL', userId: 'bob', affectedUserIds: ['bob'] })))
    server(() => sockets.latest().send(changed(2, { symbol: 'MSFT', userId: 'bob', affectedUserIds: ['bob'] })))
    // A later relevant change proves the earlier ones were handled and ignored.
    server(() => sockets.latest().send(changed(3, { userId: 'alice', affectedUserIds: ['alice'] })))
    await waitFor(() => expect(client.getPortfolio).toHaveBeenCalledTimes(3))
  })

  it('updates both counterparties’ portfolios in separate windows after one cross-account fill', async () => {
    const byUser = { alice: portfolio('alice'), bob: portfolio('bob') }
    const client = fakeClient(byUser)
    const aliceWindow = mockSocketFactory()
    const bobWindow = mockSocketFactory()
    render(
      <>
        <section aria-label="Alice window">
          <Page client={client} initialUser="alice" factory={aliceWindow} />
        </section>
        <section aria-label="Bob window">
          <Page client={client} initialUser="bob" factory={bobWindow} />
        </section>
      </>,
    )
    const alice = within(screen.getByRole('region', { name: 'Alice window' }))
    const bob = within(screen.getByRole('region', { name: 'Bob window' }))
    await alice.findAllByText('$161,000.00')
    await bob.findAllByText('$161,000.00')
    server(() => {
      aliceWindow.latest().send(hello(0))
      bobWindow.latest().send(hello(0))
    })

    byUser.alice = aliceAfterExample1
    byUser.bob = bobAfterExample1
    const fill = changed(1, { userId: 'bob', executionCount: 1, affectedUserIds: ['alice', 'bob'] })
    server(() => {
      aliceWindow.latest().send(fill)
      bobWindow.latest().send(fill)
    })
    expect(await alice.findByText('$160,950.00')).toBeInTheDocument()
    expect(await bob.findByText('$161,050.00')).toBeInTheDocument()
  })

  it('re-reads after a reconnect and after a missed event, and clears values from a restarted server', async () => {
    const client = fakeClient({ alice: aliceAfterExample1 })
    await renderLive(client)

    server(() => sockets.latest().send(heartbeat(4)))
    await waitFor(() => expect(client.getPortfolio).toHaveBeenCalledTimes(3))

    server(() => sockets.latest().serverClose(1001))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Reconnecting…')
    await waitFor(() => expect(sockets.sockets).toHaveLength(2), { timeout: 2000 })
    const fresh = deferred<PortfolioDto>()
    client.getPortfolio.mockImplementationOnce(() => fresh.promise)
    server(() => sockets.latest().send(hello(0))) // lower than 4: the server restarted
    await waitFor(() => expect(screen.queryByText('$160,950.00')).not.toBeInTheDocument())
    expect(screen.getByText('Loading the portfolio…')).toBeInTheDocument()
    await act(async () => fresh.resolve(portfolio('alice')))
    expect(await screen.findByText('$161,000.00')).toBeInTheDocument()
  })

  it('drops a live read for the previous account after a rapid switch', async () => {
    const client = fakeClient({ alice: aliceAfterExample1, bob: bobAfterExample1 })
    const user = await renderLive(client)

    const lateAlice = deferred<PortfolioDto>()
    client.getPortfolio.mockImplementationOnce(() => lateAlice.promise)
    server(() => sockets.latest().send(changed(1, { affectedUserIds: ['alice'] })))
    await waitFor(() => expect(client.getPortfolio).toHaveBeenCalledTimes(3))

    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    expect(await screen.findByText('$161,050.00')).toBeInTheDocument()
    await act(async () => lateAlice.resolve(aliceAfterExample1))
    expect(screen.getByRole('heading', { name: 'Bob (demo) portfolio' })).toBeInTheDocument()
    expect(screen.queryByText('$160,950.00')).not.toBeInTheDocument()
  })

  it('keeps manual Refresh working while live updates are stopped, without implying an order failed', async () => {
    const client = fakeClient()
    const user = userEvent.setup()
    render(<Page client={client} />)
    await screen.findByText('$161,000.00')
    server(() => sockets.latest().send('not json'))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connection error')
    expect(screen.getByText(/This does not mean an order failed/)).toBeInTheDocument()

    client.getPortfolio.mockResolvedValueOnce(aliceAfterExample1)
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('$160,950.00')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Reconnect live updates' }))
    expect(sockets.sockets).toHaveLength(2)
  })

  it('closes its stream when unmounted', async () => {
    render(<Page client={fakeClient()} />)
    await screen.findByText('$161,000.00')
    const socket = sockets.latest()
    cleanup()
    expect(socket.closedByClient).toBe(true)
    expect(socket.listenerCount()).toBe(0)
  })
})
