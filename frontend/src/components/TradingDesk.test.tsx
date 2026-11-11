import { act, cleanup as cleanupDesk, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import seedFixture from '../../../docs/fixtures/00-seed.json'
import noFill from '../../../docs/fixtures/01-no-fill.json'
import partialFill from '../../../docs/fixtures/03-partial-fill.json'
import riskRejection from '../../../docs/fixtures/04-risk-rejection.json'
import {
  ApiError,
  OrderOutcomeUnknownError,
  createTradeGridClient,
  type AccountDto,
  type BookSnapshotDto,
  type ExecutionDto,
  type OrderDto,
  type SymbolDto,
  type SubmitOrderResult,
  type TradeGridClient,
} from '../api/client'
import type { DemoAccount } from '../hooks/useDemoAccounts'
import { changed, heartbeat, hello, mockSocketFactory } from '../test/mockWebSocket'
import TradingDesk from './TradingDesk'

// ---------------------------------------------------------------------------
// Fixtures and a fake server

const symbols = seedFixture.after['GET /api/v1/symbols'].symbols as SymbolDto[]
const seedAccounts = seedFixture.after['GET /api/v1/accounts'].accounts as AccountDto[]
const emptyBook = (symbol: string): BookSnapshotDto => ({ symbol, asOfSequence: 0, bids: [], asks: [] })

interface ServerState {
  accounts: AccountDto[]
  orders: Record<string, OrderDto[]>
  books: Record<string, BookSnapshotDto>
  executions: Record<string, ExecutionDto[]>
}

const seedState = (): ServerState => ({
  accounts: seedAccounts,
  orders: { alice: [], bob: [] },
  books: { AAPL: emptyBook('AAPL'), MSFT: emptyBook('MSFT') },
  executions: { AAPL: [], MSFT: [] },
})

/** State after Alice's resting SELL (the setup step of the partial-fill fixture). */
const afterAliceSell = (): ServerState => ({
  ...seedState(),
  accounts: noFill.after['GET /api/v1/accounts'].accounts as AccountDto[],
  orders: { alice: [noFill.response.body.order as OrderDto], bob: [] },
  books: { AAPL: noFill.after['GET /api/v1/orderbook?symbol=AAPL'] as BookSnapshotDto, MSFT: emptyBook('MSFT') },
})

const afterPartialFill = (): ServerState => ({
  accounts: partialFill.after['GET /api/v1/accounts'].accounts as AccountDto[],
  orders: {
    alice: partialFill.after['GET /api/v1/orders?userId=alice'].orders as OrderDto[],
    bob: partialFill.after['GET /api/v1/orders?userId=bob'].orders as OrderDto[],
  },
  books: { AAPL: partialFill.after['GET /api/v1/orderbook?symbol=AAPL'] as BookSnapshotDto, MSFT: emptyBook('MSFT') },
  executions: {
    AAPL: partialFill.after['GET /api/v1/executions?symbol=AAPL'].executions as ExecutionDto[],
    MSFT: [],
  },
})

function fakeClient(initial: ServerState = seedState()) {
  const server = { state: initial }
  const client = {
    getSymbols: vi.fn(async () => symbols),
    getAccounts: vi.fn(async () => server.state.accounts),
    getOrders: vi.fn(async (userId: string) => server.state.orders[userId] ?? []),
    getOrderBook: vi.fn(async (symbol: string) => server.state.books[symbol]),
    getExecutions: vi.fn(async (symbol: string) => server.state.executions[symbol] ?? []),
    submitOrder: vi.fn<TradeGridClient['submitOrder']>(async () => {
      throw new Error('submitOrder not stubbed')
    }),
  } satisfies TradeGridClient
  return { client, server }
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

const directory: DemoAccount[] = seedAccounts.map(({ userId, displayName }) => ({ userId, displayName }))

/** Sockets the desk opened in the current test; the test plays the server. */
let sockets = mockSocketFactory()
beforeEach(() => {
  sockets = mockSocketFactory()
})

/** The desk as App mounts it: the account selection lives one level up. */
function Desk({ client, onBalanceChange }: { client: TradeGridClient; onBalanceChange?: (b: string | undefined) => void }) {
  const [userId, setUserId] = useState('alice')
  return (
    <TradingDesk
      client={client}
      accounts={directory}
      userId={userId}
      onUserChange={setUserId}
      onBalanceChange={onBalanceChange}
      realtime={{ url: 'ws://test/api/v1/stream', createSocket: sockets.create }}
    />
  )
}

const networkError = () => new ApiError('network', 'GET failed: the server could not be reached')

async function renderDesk(client: TradeGridClient) {
  const onBalanceChange = vi.fn()
  const user = userEvent.setup()
  render(<Desk client={client} onBalanceChange={onBalanceChange} />)
  await screen.findByRole('heading', { name: /^Orders for / })
  await initialLoadsSettled()
  return { user, onBalanceChange }
}

/** Every panel's first read has answered (balances, orders, book, and trades load independently). */
async function initialLoadsSettled() {
  await waitFor(() => expect(screen.queryByText(/^Loading (account balances|orders|order book|recent trades)/)).toBeNull())
}

async function placeOrder(
  user: ReturnType<typeof userEvent.setup>,
  { side, quantity, price }: { side: 'BUY' | 'SELL'; quantity: string; price: string },
) {
  await user.click(screen.getByRole('radio', { name: side }))
  await user.clear(screen.getByLabelText('Quantity (shares)'))
  await user.type(screen.getByLabelText('Quantity (shares)'), quantity)
  await user.clear(screen.getByLabelText('Limit price (USD)'))
  await user.type(screen.getByLabelText('Limit price (USD)'), price)
  await user.click(screen.getByRole('button', { name: 'Submit limit order' }))
}

const balances = () => within(screen.getByRole('region', { name: /balances$/ }))
const cashValue = (label: string) => balances().getByText(label).nextElementSibling?.textContent

// ---------------------------------------------------------------------------

describe('TradingDesk', () => {
  it('shows a pending state, then the first account and symbol from the server', async () => {
    const { client } = fakeClient()
    const onBalanceChange = vi.fn()
    render(<Desk client={client} onBalanceChange={onBalanceChange} />)

    expect(screen.getByText('Loading tradable symbols…')).toHaveAttribute('role', 'status')

    expect(await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })).toBeInTheDocument()
    await initialLoadsSettled()
    expect(screen.getByLabelText('Demo account')).toHaveValue('alice')
    expect(screen.getByLabelText('Market')).toHaveValue('AAPL')
    expect(screen.getByRole('option', { name: 'MSFT — Microsoft Corporation' })).toBeInTheDocument()
    expect(screen.getByText('No orders yet for Alice (demo).')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Order book · AAPL' })).toBeInTheDocument()
    expect(screen.getByText('No recent trades.')).toBeInTheDocument()

    expect(cashValue('Cash available')).toBe('$100,000.00')
    expect(cashValue('Cash reserved')).toBe('$0.00')
    expect(onBalanceChange).toHaveBeenLastCalledWith('$100,000.00 available')

    expect(client.getOrders).toHaveBeenCalledWith('alice', expect.anything())
    expect(client.getOrderBook).toHaveBeenCalledWith('AAPL', expect.anything())
    expect(client.getExecutions).toHaveBeenCalledWith('AAPL', expect.anything())
  })

  it('states that accounts are simulated and reset when the server restarts', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    const notice = screen.getByText(/Simulated demo accounts\./).closest('p')
    expect(notice).toHaveTextContent('No real money')
    expect(notice).toHaveTextContent('not a sign-in')
    expect(notice).toHaveTextContent('reset when the server restarts')
  })

  it('switches account: loads that account’s orders and shows its server balance', async () => {
    const { client } = fakeClient(afterAliceSell())
    const { user, onBalanceChange } = await renderDesk(client)
    expect(cashValue('Cash available')).toBe('$100,000.00')
    expect(balances().getByRole('row', { name: /AAPL/ })).toHaveTextContent('90 shares10 shares100 shares')

    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')

    expect(await screen.findByText('No orders yet for Bob (demo).')).toBeInTheDocument()
    expect(client.getOrders).toHaveBeenLastCalledWith('bob', expect.anything())
    expect(screen.getByRole('heading', { name: 'Bob (demo) balances' })).toBeInTheDocument()
    expect(balances().getByRole('row', { name: /AAPL/ })).toHaveTextContent('100 shares0 shares100 shares')
    expect(onBalanceChange).toHaveBeenLastCalledWith('$100,000.00 available')
    expect(screen.getByText('Demo account:').querySelector('strong')).toHaveTextContent('bob')
  })

  it('switches symbol from the selector or the form and keeps them in step', async () => {
    const { client } = fakeClient()
    const { user } = await renderDesk(client)

    await user.selectOptions(screen.getByLabelText('Market'), 'MSFT')
    expect(await screen.findByRole('heading', { name: 'Order book · MSFT' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Order book · MSFT' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Symbol' })).toHaveValue('MSFT')
    expect(client.getOrderBook).toHaveBeenLastCalledWith('MSFT', expect.anything())
    expect(client.getExecutions).toHaveBeenLastCalledWith('MSFT', expect.anything())

    const symbolInput = screen.getByRole('textbox', { name: 'Symbol' })
    await user.clear(symbolInput)
    await user.type(symbolInput, 'AAPL')
    expect(await screen.findByRole('heading', { name: 'Order book · AAPL' })).toBeInTheDocument()
    expect(screen.getByLabelText('Market')).toHaveValue('AAPL')
  })

  it('submits once, shows the server result, and reloads authoritative snapshots', async () => {
    const { client, server } = fakeClient(afterAliceSell())
    client.submitOrder.mockImplementation(async () => {
      server.state = afterPartialFill()
      return partialFill.response.body as SubmitOrderResult
    })
    const { user, onBalanceChange } = await renderDesk(client)
    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    await screen.findByText('No orders yet for Bob (demo).')
    const callsBefore = [client.getAccounts, client.getOrders, client.getOrderBook, client.getExecutions].map(
      (fn) => fn.mock.calls.length,
    )

    await placeOrder(user, { side: 'BUY', quantity: '25', price: '190.50' })

    expect(client.submitOrder.mock.calls).toEqual([[partialFill.request.body]])
    expect(
      await screen.findByText(
        'Order ord-2 accepted for Bob (demo): partially filled, 15 shares remaining. 1 fill: 10 shares at $190.00.',
      ),
    ).toBeInTheDocument()

    // Every relevant snapshot is read again after the POST.
    await waitFor(() => expect(cashValue('Cash available')).toBe('$95,242.50'))
    expect(
      [client.getAccounts, client.getOrders, client.getOrderBook, client.getExecutions].map(
        (fn) => fn.mock.calls.length,
      ),
    ).toEqual(callsBefore.map((count) => count + 1))
    expect(client.getOrders).toHaveBeenLastCalledWith('bob', expect.anything())

    // Displayed values are the server's, not computed in the browser.
    expect(cashValue('Cash reserved')).toBe('$2,857.50')
    expect(cashValue('Cash total')).toBe('$98,100.00')
    expect(onBalanceChange).toHaveBeenLastCalledWith('$95,242.50 available')
    const orderRow = within(screen.getByRole('table', { name: 'Orders for Bob (demo)' })).getAllByRole('row')[1]
    expect(orderRow).toHaveTextContent('Partially filled')
    expect(orderRow).toHaveTextContent('15 shares')
    expect(screen.getByText('Best bid: $190.50')).toBeInTheDocument()
    expect(within(screen.getByRole('table', { name: 'Recent executions · newest first' })).getAllByRole('row')).toHaveLength(2)
  })

  it('shows a contracted rejection with the server message and does not reload or retry', async () => {
    const { client } = fakeClient()
    const { code, message } = riskRejection.response.body.error
    client.submitOrder.mockRejectedValue(new ApiError('http', message, { status: 422, code }))
    const { user } = await renderDesk(client)
    const accountsCalls = client.getAccounts.mock.calls.length

    await placeOrder(user, { side: 'BUY', quantity: '600', price: '200.00' })

    expect(await screen.findByText(`Order rejected by the server: ${message} (${code}). No order was created.`))
      .toHaveAttribute('role', 'alert')
    expect(client.submitOrder).toHaveBeenCalledTimes(1)
    expect(client.getAccounts).toHaveBeenCalledTimes(accountsCalls)
    expect(screen.queryByText(/accepted/)).not.toBeInTheDocument()
  })

  it('marks the form field the server rejected', async () => {
    const { client } = fakeClient()
    const message = 'price must be between 1 and 100000000 cents'
    client.submitOrder.mockRejectedValue(
      new ApiError('http', message, { status: 422, code: 'INVALID_PRICE', field: 'price' }),
    )
    const { user } = await renderDesk(client)

    await placeOrder(user, { side: 'SELL', quantity: '1', price: '10.00' })

    expect(await screen.findByText(message)).toHaveAttribute('role', 'alert')
    expect(screen.getByLabelText('Limit price (USD)')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Limit price (USD)')).toHaveAccessibleDescription(expect.stringContaining(message))
    expect(screen.getByText(/Order rejected by the server \(INVALID_PRICE\)/)).toBeInTheDocument()

    // Editing the field clears the server's feedback.
    await user.type(screen.getByLabelText('Limit price (USD)'), '1')
    expect(screen.queryByText(message)).not.toBeInTheDocument()
  })

  it('reports an ambiguous POST failure as outcome unknown, never success, and does not retry', async () => {
    const { client } = fakeClient()
    client.submitOrder.mockRejectedValue(new OrderOutcomeUnknownError(networkError()))
    const { user } = await renderDesk(client)
    const accountsCalls = client.getAccounts.mock.calls.length

    await placeOrder(user, { side: 'SELL', quantity: '10', price: '190.00' })

    const alert = await screen.findByText(/^Order outcome unknown — refresh to check\./)
    expect(alert).toHaveAttribute('role', 'alert')
    expect(alert).toHaveTextContent(
      'Your SELL order for 10 shares of AAPL at $190.00 (Alice (demo)) may or may not have been accepted: ' +
        'the connection failed before a response arrived.',
    )
    expect(screen.queryByText(/accepted for/)).not.toBeInTheDocument()
    expect(screen.queryByText(/rejected/i)).not.toBeInTheDocument()
    expect(client.submitOrder).toHaveBeenCalledTimes(1)
    expect(client.getAccounts).toHaveBeenCalledTimes(accountsCalls)

    // The warning survives edits; only the next submission replaces it.
    await user.type(screen.getByLabelText('Quantity (shares)'), '0')
    expect(screen.getByText(/^Order outcome unknown/)).toBeInTheDocument()
  })

  it('with the real client, sends one POST on a network failure and shows outcome unknown', async () => {
    const seedResponses: Record<string, unknown> = {
      '/api/v1/symbols': { symbols },
      '/api/v1/accounts': { accounts: seedAccounts },
      '/api/v1/orders?userId=alice': { orders: [] },
      '/api/v1/orderbook?symbol=AAPL': emptyBook('AAPL'),
      '/api/v1/executions?symbol=AAPL': { executions: [] },
    }
    const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') throw new TypeError('Failed to fetch')
      return new Response(JSON.stringify(seedResponses[String(input)]), { status: 200 })
    })
    const { user } = await renderDesk(createTradeGridClient({ fetch, timeoutMs: 0 }))

    await placeOrder(user, { side: 'BUY', quantity: '1', price: '1.00' })

    expect(await screen.findByText(/^Order outcome unknown — refresh to check\./)).toBeInTheDocument()
    expect(fetch.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
    expect(screen.queryByText(/accepted for/)).not.toBeInTheDocument()
  })

  it('disables submission and selection while a POST is pending', async () => {
    const { client } = fakeClient()
    const response = deferred<SubmitOrderResult>()
    client.submitOrder.mockReturnValue(response.promise)
    const { user } = await renderDesk(client)

    await placeOrder(user, { side: 'SELL', quantity: '10', price: '190.00' })

    expect(screen.getByRole('button', { name: 'Submitting…' })).toBeDisabled()
    expect(screen.getByLabelText('Demo account')).toBeDisabled()
    expect(screen.getByLabelText('Market')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeDisabled()

    await act(async () => response.resolve(noFill.response.body as SubmitOrderResult))
    expect(await screen.findByText(/^Order ord-1 accepted for Alice \(demo\): resting on the book/)).toBeInTheDocument()
    expect(client.submitOrder).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText('Demo account')).toBeEnabled()
  })

  it('reloads accounts, orders, book, and trades on Refresh', async () => {
    const { client, server } = fakeClient(seedState())
    const { user } = await renderDesk(client)
    expect(screen.getByRole('heading', { name: 'Order book · AAPL' })).toBeInTheDocument()

    // Another window placed an order in the meantime.
    server.state = afterAliceSell()
    await user.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(await screen.findByText('Best ask: $190.00')).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'Orders for Alice (demo)' })).toHaveTextContent('New')
    expect(balances().getByRole('row', { name: /AAPL/ })).toHaveTextContent('90 shares10 shares100 shares')
    for (const fn of [client.getAccounts, client.getOrders, client.getOrderBook, client.getExecutions]) {
      expect(fn).toHaveBeenCalledTimes(2)
    }
    expect(client.getSymbols).toHaveBeenCalledTimes(1)
  })

  describe('stale responses', () => {
    it('ignores orders for the previous account when they arrive after the switch', async () => {
      const { client } = fakeClient()
      const aliceOrders = deferred<OrderDto[]>()
      const bobOrders = deferred<OrderDto[]>()
      client.getOrders.mockImplementation((userId: string) =>
        userId === 'alice' ? aliceOrders.promise : bobOrders.promise,
      )
      const user = userEvent.setup()
      render(<Desk client={client} />)
      await screen.findByText('Loading orders…')

      await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
      // Bob's answer first, then Alice's late one.
      await act(async () => bobOrders.resolve(partialFill.after['GET /api/v1/orders?userId=bob'].orders as OrderDto[]))
      await act(async () => aliceOrders.resolve(noFill.after['GET /api/v1/orders?userId=alice'].orders as OrderDto[]))

      const table = screen.getByRole('table', { name: 'Orders for Bob (demo)' })
      const rows = within(table).getAllByRole('row').slice(1)
      expect(rows).toHaveLength(1)
      expect(rows[0]).toHaveTextContent('Buy')
      expect(rows[0]).toHaveTextContent('Partially filled')
    })

    it('keeps loading the new account when the old account answers first', async () => {
      const { client } = fakeClient()
      const aliceOrders = deferred<OrderDto[]>()
      const bobOrders = deferred<OrderDto[]>()
      client.getOrders.mockImplementation((userId: string) =>
        userId === 'alice' ? aliceOrders.promise : bobOrders.promise,
      )
      const user = userEvent.setup()
      render(<Desk client={client} />)
      await screen.findByText('Loading orders…')

      await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
      await act(async () => aliceOrders.resolve(noFill.after['GET /api/v1/orders?userId=alice'].orders as OrderDto[]))

      expect(screen.getByText('Loading orders…')).toBeInTheDocument()
      expect(screen.queryByRole('table', { name: /^Orders for/ })).not.toBeInTheDocument()

      await act(async () => bobOrders.resolve([]))
      expect(screen.getByText('No orders yet for Bob (demo).')).toBeInTheDocument()
    })

    it('ignores a book and trades for the previous symbol', async () => {
      const { client } = fakeClient()
      const aaplBook = deferred<BookSnapshotDto>()
      const aaplTrades = deferred<ExecutionDto[]>()
      client.getOrderBook.mockImplementation(async (symbol: string) =>
        symbol === 'AAPL' ? aaplBook.promise : emptyBook('MSFT'),
      )
      client.getExecutions.mockImplementation(async (symbol: string) => (symbol === 'AAPL' ? aaplTrades.promise : []))
      const user = userEvent.setup()
      render(<Desk client={client} />)
      await screen.findByText('Loading order book…')

      await user.selectOptions(screen.getByLabelText('Market'), 'MSFT')
      await screen.findByRole('heading', { name: 'Order book · MSFT' })
      await act(async () => {
        aaplBook.resolve(partialFill.after['GET /api/v1/orderbook?symbol=AAPL'] as BookSnapshotDto)
        aaplTrades.resolve(partialFill.after['GET /api/v1/executions?symbol=AAPL'].executions as ExecutionDto[])
      })

      expect(screen.getByRole('heading', { name: 'Order book · MSFT' })).toBeInTheDocument()
      expect(screen.getByText('No recent trades.')).toBeInTheDocument()
      expect(screen.queryByText('$190.50')).not.toBeInTheDocument()
    })

    it('keeps the newest refresh when an older one answers last', async () => {
      const { client } = fakeClient()
      const { user } = await renderDesk(client)
      const older = deferred<AccountDto[]>()
      const newer = deferred<AccountDto[]>()
      client.getAccounts.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

      await user.click(screen.getByRole('button', { name: 'Refresh' }))
      await user.click(screen.getByRole('button', { name: 'Refresh' }))
      await act(async () => newer.resolve(partialFill.after['GET /api/v1/accounts'].accounts as AccountDto[]))
      await act(async () => older.resolve(seedAccounts))

      expect(cashValue('Cash available')).toBe('$101,900.00')
    })
  })

  describe('offline server', () => {
    it('explains a failed first load and recovers on Refresh', async () => {
      const { client } = fakeClient()
      client.getSymbols.mockRejectedValueOnce(networkError())
      client.getAccounts.mockRejectedValueOnce(networkError())
      const user = userEvent.setup()
      render(<Desk client={client} />)

      const alert = await screen.findByRole('alert')
      expect(alert).toHaveTextContent('Could not load the trading desk: the server could not be reached.')
      expect(alert).toHaveTextContent('Check that the backend is running')
      // The account selection is app-level, so it stays usable.
      expect(screen.getByLabelText('Demo account')).toHaveValue('alice')

      await user.click(screen.getByRole('button', { name: 'Refresh' }))
      expect(await screen.findByRole('heading', { name: 'Orders for Alice (demo)' })).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('shows an unavailable proxy (empty 502) as a server outage', async () => {
      const { client } = fakeClient()
      const proxyError = () =>
        new ApiError('invalid-response', 'GET /symbols returned HTTP 502 with a non-JSON body', { status: 502 })
      client.getSymbols.mockRejectedValueOnce(proxyError())
      client.getAccounts.mockRejectedValueOnce(proxyError())
      render(<Desk client={client} />)

      expect(await screen.findByRole('alert')).toHaveTextContent(
        'Could not load the trading desk: the server is unavailable (HTTP 502).',
      )
    })

    it('flags an outage after loading and drops values it can no longer confirm', async () => {
      const { client } = fakeClient()
      const { user, onBalanceChange } = await renderDesk(client)
      for (const fn of [client.getAccounts, client.getOrders, client.getOrderBook, client.getExecutions]) {
        fn.mockRejectedValueOnce(networkError())
      }

      await user.click(screen.getByRole('button', { name: 'Refresh' }))

      expect(await screen.findByText('The TradeGrid server is not responding.')).toBeInTheDocument()
      expect(screen.getByText('Could not load orders: the server could not be reached')).toBeInTheDocument()
      expect(screen.getByText('Could not load the order book: the server could not be reached')).toBeInTheDocument()
      expect(screen.getByText('Could not load recent trades: the server could not be reached')).toBeInTheDocument()
      expect(
        screen.getByText('Could not load account balances: the server could not be reached'),
      ).toBeInTheDocument()
      expect(onBalanceChange).toHaveBeenLastCalledWith(undefined)
      // The selectors keep working so the user can retry.
      expect(screen.getByLabelText('Demo account')).toHaveValue('alice')

      await user.click(screen.getByRole('button', { name: 'Refresh' }))
      await waitFor(() => expect(screen.queryByText('The TradeGrid server is not responding.')).not.toBeInTheDocument())
      expect(cashValue('Cash available')).toBe('$100,000.00')
    })

    it('shows a contracted GET error with its code', async () => {
      const { client } = fakeClient()
      client.getOrders.mockRejectedValue(
        new ApiError('http', 'unknown user "alice"', { status: 404, code: 'UNKNOWN_USER', field: 'userId' }),
      )
      render(<Desk client={client} />)
      expect(
        await screen.findByText('Could not load orders: unknown user "alice" (UNKNOWN_USER)'),
      ).toBeInTheDocument()
      expect(screen.queryByText('The TradeGrid server is not responding.')).not.toBeInTheDocument()
    })
  })

  it('shows an empty state when the server lists no symbols', async () => {
    const { client } = fakeClient()
    client.getSymbols.mockResolvedValue([])
    render(<Desk client={client} />)
    expect(
      await screen.findByText('The server returned no tradable symbols, so there is nothing to trade.'),
    ).toBeInTheDocument()
    expect(client.getOrderBook).not.toHaveBeenCalled()
  })
})

// ---------------------------------------------------------------------------
// Live refresh (#77)

describe('TradingDesk live refresh', () => {
  const server = (fn: () => void) => act(fn)
  const callCounts = (client: ReturnType<typeof fakeClient>['client']) =>
    [client.getAccounts, client.getOrders, client.getOrderBook, client.getExecutions].map((fn) => fn.mock.calls.length)

  it('opens one stream, shows its state, and re-reads every snapshot on hello', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    expect(sockets.sockets).toHaveLength(1)
    expect(sockets.latest().url).toBe('ws://test/api/v1/stream')
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connecting…')

    const before = callCounts(client)
    server(() => sockets.latest().send(hello(0)))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connected')
    await waitFor(() => expect(callCounts(client)).toEqual(before.map((n) => n + 1)))
    expect(client.getOrders).toHaveBeenLastCalledWith('alice', expect.anything())
    expect(client.getOrderBook).toHaveBeenLastCalledWith('AAPL', expect.anything())
  })

  it('shows another window’s trade without Refresh, from authoritative reads', async () => {
    const { client, server: backend } = fakeClient(afterAliceSell())
    const { onBalanceChange } = await renderDesk(client)
    server(() => sockets.latest().send(hello(1)))
    expect(await screen.findByText('Best ask: $190.00')).toBeInTheDocument()

    // Bob, in another window, buys 25 at $190.50: 10 fill against Alice.
    backend.state = afterPartialFill()
    server(() =>
      sockets.latest().send(changed(2, { symbol: 'AAPL', userId: 'bob', orderId: 'ord-2', executionCount: 1, affectedUserIds: ['alice', 'bob'] })),
    )
    expect(await screen.findByText('Best bid: $190.50')).toBeInTheDocument()
    expect(within(screen.getByRole('table', { name: 'Recent executions · newest first' })).getAllByRole('row')).toHaveLength(2)
    expect(within(screen.getByRole('table', { name: 'Orders for Alice (demo)' })).getAllByRole('row')[1]).toHaveTextContent('Filled')
    expect(screen.queryByRole('button', { name: 'Refresh' })).toBeEnabled()
    expect(onBalanceChange).toHaveBeenLastCalledWith('$101,900.00 available')
  })

  it('re-reads only what a change affects, and nothing for another symbol and account', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(2))

    let before = callCounts(client)
    server(() => sockets.latest().send(changed(1, { symbol: 'MSFT', userId: 'bob', affectedUserIds: ['bob'] })))
    // A later relevant change proves the first one was handled and ignored.
    server(() => sockets.latest().send(changed(2, { symbol: 'AAPL', userId: 'bob', affectedUserIds: ['bob'] })))
    await waitFor(() => expect(client.getOrderBook.mock.calls.length).toBe(before[2] + 1))
    expect(callCounts(client)).toEqual([before[0], before[1], before[2] + 1, before[3] + 1])

    before = callCounts(client)
    server(() => sockets.latest().send(changed(3, { symbol: 'MSFT', userId: 'alice', affectedUserIds: ['alice'] })))
    await waitFor(() => expect(client.getOrders.mock.calls.length).toBe(before[1] + 1))
    expect(callCounts(client)).toEqual([before[0] + 1, before[1] + 1, before[2], before[3]])
  })

  it('coalesces a burst of changes into one read per view', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(2))
    const before = callCounts(client)
    server(() => {
      for (let seq = 1; seq <= 5; seq++) sockets.latest().send(changed(seq, { affectedUserIds: ['alice', 'bob'] }))
    })
    await waitFor(() => expect(callCounts(client)).toEqual(before.map((n) => n + 1)))
  })

  it('never shows the previous account’s orders when a live read returns after a switch', async () => {
    const { client } = fakeClient(afterPartialFill())
    const { user } = await renderDesk(client)
    server(() => sockets.latest().send(hello(2)))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(2))

    const lateAlice = deferred<OrderDto[]>()
    client.getOrders.mockImplementationOnce(() => lateAlice.promise)
    server(() => sockets.latest().send(changed(3, { affectedUserIds: ['alice'] })))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(3))

    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    expect(await screen.findByRole('table', { name: 'Orders for Bob (demo)' })).toBeInTheDocument()
    await act(async () => lateAlice.resolve(partialFill.after['GET /api/v1/orders?userId=alice'].orders as OrderDto[]))
    expect(screen.getByRole('heading', { name: 'Orders for Bob (demo)' })).toBeInTheDocument()
    const rows = within(screen.getByRole('table', { name: 'Orders for Bob (demo)' })).getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(rows[1]).toHaveTextContent('Buy')
  })

  it('discards both earlier account reads after switching Alice to Bob and back', async () => {
    const { client } = fakeClient()
    const { user } = await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(2))

    const oldAlice = deferred<OrderDto[]>()
    const oldBob = deferred<OrderDto[]>()
    client.getOrders.mockImplementationOnce(() => oldAlice.promise)
    server(() => sockets.latest().send(changed(1, { affectedUserIds: ['alice'] })))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(3))
    client.getOrders.mockImplementationOnce(() => oldBob.promise)
    await user.selectOptions(screen.getByLabelText('Demo account'), 'bob')
    await user.selectOptions(screen.getByLabelText('Demo account'), 'alice')
    await screen.findByText('No orders yet for Alice (demo).')

    await act(async () => {
      oldBob.resolve(partialFill.after['GET /api/v1/orders?userId=bob'].orders as OrderDto[])
      oldAlice.resolve(noFill.after['GET /api/v1/orders?userId=alice'].orders as OrderDto[])
    })
    expect(screen.getByLabelText('Demo account')).toHaveValue('alice')
    expect(screen.getByText('No orders yet for Alice (demo).')).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: /^Orders for/ })).not.toBeInTheDocument()
    expect(sockets.sockets).toHaveLength(1)
  })

  it('discards a live book and executions read after the market changes', async () => {
    const { client } = fakeClient()
    const { user } = await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await waitFor(() => expect(client.getOrderBook).toHaveBeenCalledTimes(2))

    const oldBook = deferred<BookSnapshotDto>()
    const oldTrades = deferred<ExecutionDto[]>()
    client.getOrderBook.mockImplementationOnce(() => oldBook.promise)
    client.getExecutions.mockImplementationOnce(() => oldTrades.promise)
    server(() => sockets.latest().send(changed(1)))
    await waitFor(() => expect(client.getOrderBook).toHaveBeenCalledTimes(3))
    await user.selectOptions(screen.getByLabelText('Market'), 'MSFT')
    await screen.findByRole('heading', { name: 'Order book · MSFT' })

    await act(async () => {
      oldBook.resolve(partialFill.after['GET /api/v1/orderbook?symbol=AAPL'] as BookSnapshotDto)
      oldTrades.resolve(partialFill.after['GET /api/v1/executions?symbol=AAPL'].executions as ExecutionDto[])
    })
    expect(screen.getByRole('heading', { name: 'Order book · MSFT' })).toBeInTheDocument()
    expect(screen.getByText('No recent trades.')).toBeInTheDocument()
    expect(screen.queryByText('Best bid: $190.50')).not.toBeInTheDocument()
  })

  it('re-reads everything after a reconnect and after a missed event', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await waitFor(() => expect(client.getOrders).toHaveBeenCalledTimes(2))

    // A heartbeat ahead of what this client saw means events were missed.
    let before = callCounts(client)
    server(() => sockets.latest().send(heartbeat(3)))
    await waitFor(() => expect(callCounts(client)).toEqual(before.map((n) => n + 1)))

    before = callCounts(client)
    server(() => sockets.latest().serverClose(1001))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Reconnecting…')
    await waitFor(() => expect(sockets.sockets).toHaveLength(2), { timeout: 2000 })
    server(() => sockets.latest().send(hello(3)))
    await waitFor(() => expect(callCounts(client)).toEqual(before.map((n) => n + 1)))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connected')
  })

  it('drops every value from a restarted server before showing the new ones', async () => {
    const { client, server: backend } = fakeClient(afterPartialFill())
    await renderDesk(client)
    server(() => sockets.latest().send(hello(2)))
    expect(await screen.findByText('Best bid: $190.50')).toBeInTheDocument()

    server(() => sockets.latest().serverClose(1001))
    await waitFor(() => expect(sockets.sockets).toHaveLength(2), { timeout: 2000 })
    backend.state = seedState()
    const book = deferred<BookSnapshotDto>()
    client.getOrderBook.mockImplementationOnce(() => book.promise)
    server(() => sockets.latest().send(hello(0))) // lower than before: a restart
    await waitFor(() => expect(screen.queryByText('Best bid: $190.50')).not.toBeInTheDocument())
    expect(screen.getByText('Loading order book…')).toBeInTheDocument()
    await act(async () => book.resolve(emptyBook('AAPL')))
    expect(await screen.findByText('No resting orders.')).toBeInTheDocument()
    expect(cashValue('Cash available')).toBe('$100,000.00')
  })

  it('keeps manual Refresh and offers a reconnect after a stream error', async () => {
    const { client } = fakeClient()
    const { user } = await renderDesk(client)
    server(() => sockets.latest().send({ ...hello(0), version: 2 }))
    expect(screen.getByText('Live updates:').querySelector('strong')).toHaveTextContent('Connection error')
    expect(screen.getByText(/This does not mean an order failed/)).toBeInTheDocument()

    const before = callCounts(client)
    await user.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(callCounts(client)).toEqual(before.map((n) => n + 1)))

    await user.click(screen.getByRole('button', { name: 'Reconnect live updates' }))
    expect(sockets.sockets).toHaveLength(2)
    server(() => sockets.latest().send(hello(0)))
    expect(screen.queryByRole('button', { name: 'Reconnect live updates' })).not.toBeInTheDocument()
  })

  it('never retries an order POST when the stream reconnects', async () => {
    const { client } = fakeClient()
    client.submitOrder.mockRejectedValue(new OrderOutcomeUnknownError(new ApiError('network', 'POST failed')))
    const { user } = await renderDesk(client)
    server(() => sockets.latest().send(hello(0)))
    await placeOrder(user, { side: 'BUY', quantity: '1', price: '190.00' })
    expect(await screen.findByText(/Order outcome unknown/)).toBeInTheDocument()

    server(() => sockets.latest().serverClose(1006))
    await waitFor(() => expect(sockets.sockets).toHaveLength(2), { timeout: 2000 })
    server(() => sockets.latest().send(hello(1)))
    await waitFor(() => expect(client.getOrders.mock.calls.length).toBeGreaterThan(2))
    expect(client.submitOrder).toHaveBeenCalledTimes(1)
    expect(screen.getByText(/Order outcome unknown/)).toBeInTheDocument()
  })

  it('closes its stream when unmounted', async () => {
    const { client } = fakeClient()
    await renderDesk(client)
    const socket = sockets.latest()
    cleanupDesk()
    expect(socket.closedByClient).toBe(true)
    expect(socket.listenerCount()).toBe(0)
  })
})
