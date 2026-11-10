import { afterEach, describe, expect, it, vi } from 'vitest'
import seedFixture from '../../../docs/fixtures/00-seed.json'
import partialFill from '../../../docs/fixtures/03-partial-fill.json'
import riskRejection from '../../../docs/fixtures/04-risk-rejection.json'
import {
  ApiError,
  OrderOutcomeUnknownError,
  createTradeGridClient,
  isServerUnavailable,
  type OrderEntryRequest,
} from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
  })
}

function setup(respond: (url: string, init: RequestInit) => Response | Promise<Response>, timeoutMs = 0) {
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => Promise.resolve(respond(String(input), init ?? {})))
  return { fetch, client: createTradeGridClient({ fetch, timeoutMs }) }
}

async function rejection<T>(promise: Promise<T>): Promise<unknown> {
  try {
    await promise
  } catch (error) {
    return error
  }
  throw new Error('expected the promise to reject')
}

const seed = {
  symbols: seedFixture.after['GET /api/v1/symbols'],
  accounts: seedFixture.after['GET /api/v1/accounts'],
  orderbook: seedFixture.after['GET /api/v1/orderbook?symbol=AAPL'],
}
const order = partialFill.request.body as OrderEntryRequest

describe('createTradeGridClient GET requests', () => {
  it('reads the seeded symbols and accounts from /api/v1', async () => {
    const { client, fetch } = setup((url) =>
      jsonResponse(200, url.endsWith('/symbols') ? seed.symbols : seed.accounts),
    )
    await expect(client.getSymbols()).resolves.toEqual(seed.symbols.symbols)
    await expect(client.getAccounts()).resolves.toEqual(seed.accounts.accounts)
    expect(fetch.mock.calls.map(([url, init]) => [url, init?.method])).toEqual([
      ['/api/v1/symbols', 'GET'],
      ['/api/v1/accounts', 'GET'],
    ])
  })

  it('encodes query parameters for orders, order book, and executions', async () => {
    const { client, fetch } = setup((url) => {
      if (url.startsWith('/api/v1/orders')) return jsonResponse(200, { orders: [] })
      if (url.startsWith('/api/v1/orderbook')) return jsonResponse(200, seed.orderbook)
      return jsonResponse(200, { executions: [] })
    })
    await expect(client.getOrders('bob', { limit: 50 })).resolves.toEqual([])
    await expect(client.getOrderBook('AAPL')).resolves.toEqual(seed.orderbook)
    await expect(client.getExecutions('MSFT', { userId: 'a&b' })).resolves.toEqual([])
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      '/api/v1/orders?userId=bob&limit=50',
      '/api/v1/orderbook?symbol=AAPL',
      '/api/v1/executions?symbol=MSFT&userId=a%26b',
    ])
  })

  it('turns a contracted 404 into an http ApiError with code, field, and message', async () => {
    const { client } = setup(() =>
      jsonResponse(404, { error: { code: 'UNKNOWN_USER', message: 'unknown user "carol"', field: 'userId' } }),
    )
    const error = await rejection(client.getOrders('carol'))
    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      kind: 'http',
      status: 404,
      code: 'UNKNOWN_USER',
      field: 'userId',
      message: 'unknown user "carol"',
    })
    expect(isServerUnavailable(error)).toBe(false)
  })

  it.each([500, 502])('reports a non-JSON %i (the dev proxy with no backend) as an unavailable server', async (status) => {
    const { client } = setup(() => new Response('', { status, headers: { 'Content-Type': 'text/plain' } }))
    const error = await rejection(client.getAccounts())
    expect(error).toMatchObject({ kind: 'invalid-response', status, code: null })
    expect(isServerUnavailable(error)).toBe(true)
  })

  it('rejects a 200 whose body is not the contracted shape', async () => {
    const { client } = setup(() => jsonResponse(200, { orders: null }))
    expect(await rejection(client.getOrders('alice'))).toMatchObject({ kind: 'invalid-response', status: 200 })
  })

  it('rejects an error status whose JSON body is not the contracted error', async () => {
    const { client } = setup(() => jsonResponse(502, { message: 'bad gateway' }))
    expect(await rejection(client.getSymbols())).toMatchObject({ kind: 'invalid-response', status: 502, code: null })
  })

  it('reports a fetch rejection as a network error', async () => {
    const { client } = setup(() => Promise.reject(new TypeError('Failed to fetch')))
    const error = await rejection(client.getSymbols())
    expect(error).toMatchObject({ kind: 'network', status: null })
    expect(isServerUnavailable(error)).toBe(true)
  })

  it('reports a caller abort as aborted, not as a network failure', async () => {
    const fetch = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    const client = createTradeGridClient({ fetch, timeoutMs: 0 })
    const controller = new AbortController()
    const pending = rejection(client.getOrders('alice', { signal: controller.signal }))
    controller.abort()
    expect(await pending).toMatchObject({ kind: 'aborted' })
  })

  it('times out a request that never answers', async () => {
    const fetch = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    const client = createTradeGridClient({ fetch, timeoutMs: 5 })
    expect(await rejection(client.getAccounts())).toMatchObject({ kind: 'timeout' })
  })
})

describe('createTradeGridClient submitOrder', () => {
  it('POSTs exactly the six client-owned fields as JSON and returns the server result', async () => {
    const { client, fetch } = setup(() => jsonResponse(201, partialFill.response.body))
    const result = await client.submitOrder({ ...order, extra: 'dropped' } as OrderEntryRequest)
    expect(result).toEqual(partialFill.response.body)
    expect(fetch).toHaveBeenCalledTimes(1)
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/v1/orders')
    expect(init?.method).toBe('POST')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
    expect(JSON.parse(String(init?.body))).toEqual(partialFill.request.body)
  })

  it.each([
    [422, riskRejection.response.body.error],
    [422, { code: 'INVALID_PRICE', message: 'price must be between 1 and 100000000 cents', field: 'price' }],
    [400, { code: 'UNKNOWN_FIELD', message: 'unknown field "id"', field: 'id' }],
    [404, { code: 'UNKNOWN_USER', message: 'unknown user', field: 'userId' }],
    [500, { code: 'INTERNAL', message: 'internal error' }],
  ])('treats a contracted %i %o as a definite rejection, sent once', async (status, error) => {
    const { client, fetch } = setup(() => jsonResponse(status, { error }))
    const thrown = await rejection(client.submitOrder(order))
    expect(thrown).toBeInstanceOf(ApiError)
    expect(thrown).not.toBeInstanceOf(OrderOutcomeUnknownError)
    expect(thrown).toMatchObject({ kind: 'http', status, code: error.code, message: error.message })
    expect((thrown as ApiError).field).toBe('field' in error ? error.field : null)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('surfaces a network failure as outcome unknown and never retries', async () => {
    const { client, fetch } = setup(() => Promise.reject(new TypeError('Failed to fetch')))
    const thrown = await rejection(client.submitOrder(order))
    expect(thrown).toBeInstanceOf(OrderOutcomeUnknownError)
    expect((thrown as OrderOutcomeUnknownError).message).toBe('Order outcome unknown — refresh to check')
    expect((thrown as OrderOutcomeUnknownError).reason.kind).toBe('network')
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['a non-JSON 500', () => new Response('<html>proxy error</html>', { status: 500 })],
    ['a non-JSON 201', () => new Response('ok', { status: 201 })],
    ['a 201 without the order', () => jsonResponse(201, { executions: [] })],
    ['a 504 without the error body', () => jsonResponse(504, {})],
  ])('surfaces %s as outcome unknown and never retries', async (_name, respond) => {
    const { client, fetch } = setup(respond)
    const thrown = await rejection(client.submitOrder(order))
    expect(thrown).toBeInstanceOf(OrderOutcomeUnknownError)
    expect((thrown as OrderOutcomeUnknownError).reason.kind).toBe('invalid-response')
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('surfaces a timed-out POST as outcome unknown', async () => {
    const fetch = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    const client = createTradeGridClient({ fetch, timeoutMs: 5 })
    const thrown = await rejection(client.submitOrder(order))
    expect(thrown).toBeInstanceOf(OrderOutcomeUnknownError)
    expect((thrown as OrderOutcomeUnknownError).reason.kind).toBe('timeout')
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})

describe('default fetch', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('uses the global fetch at call time', async () => {
    const client = createTradeGridClient()
    const fetch = vi.fn(() => Promise.resolve(jsonResponse(200, seed.symbols)))
    vi.stubGlobal('fetch', fetch)
    await expect(client.getSymbols()).resolves.toEqual(seed.symbols.symbols)
    expect(fetch).toHaveBeenCalledWith('/api/v1/symbols', expect.objectContaining({ method: 'GET' }))
  })
})

describe('createTradeGridClient getPortfolio', () => {
  // Alice after docs/portfolio.md worked example 1, exactly as documented.
  const alice = {
    userId: 'alice',
    displayName: 'Alice (demo)',
    asOfSequence: 2,
    cash: { total: 10185000, reserved: 0, available: 10185000 },
    positions: [
      { symbol: 'AAPL', total: 90, reserved: 0, available: 90, costBasis: 1710000, referencePrice: 19000,
        marketValue: 1710000, unrealizedPnl: 0, realizedPnl: -5000 },
      { symbol: 'MSFT', total: 100, reserved: 0, available: 100, costBasis: 4200000, referencePrice: 42000,
        marketValue: 4200000, unrealizedPnl: 0, realizedPnl: 0 },
    ],
    totals: { costBasis: 5910000, marketValue: 5910000, unrealizedPnl: 0, realizedPnl: -5000, equity: 16095000 },
  }

  it('reads one account’s portfolio as the server sent it', async () => {
    const { client, fetch } = setup(() => jsonResponse(200, alice))
    await expect(client.getPortfolio('alice')).resolves.toEqual(alice)
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/v1/portfolio?userId=alice')
  })

  it('rejects a response for another account instead of showing it', async () => {
    const { client } = setup(() => jsonResponse(200, { ...alice, userId: 'bob' }))
    const error = await rejection(client.getPortfolio('alice'))
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).kind).toBe('invalid-response')
  })

  it.each([
    ['a missing total', { ...alice, totals: { ...alice.totals, equity: undefined } }],
    ['a fractional amount', { ...alice, cash: { ...alice.cash, total: 10185000.5, available: 10185000.5 } }],
    ['a position amount as a string', { ...alice, positions: [{ ...alice.positions[0], marketValue: '1710000' }] }],
    ['no positions array', { ...alice, positions: null }],
  ])('rejects %s', async (_, body) => {
    const { client } = setup(() => jsonResponse(200, body))
    const error = await rejection(client.getPortfolio('alice'))
    expect((error as ApiError).kind).toBe('invalid-response')
  })

  it('reports a contracted UNKNOWN_USER as an http error', async () => {
    const { client } = setup(() =>
      jsonResponse(404, { error: { code: 'UNKNOWN_USER', message: 'unknown user "carol"', field: 'userId' } }),
    )
    const error = (await rejection(client.getPortfolio('carol'))) as ApiError
    expect([error.kind, error.status, error.code, error.field]).toEqual(['http', 404, 'UNKNOWN_USER', 'userId'])
  })
})
