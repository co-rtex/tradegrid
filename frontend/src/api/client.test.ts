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

  describe('rejects bodies that break the integer-cents contract', () => {
    const after = partialFill.after
    const bobOrders = after['GET /api/v1/orders?userId=bob'].orders
    const trades = after['GET /api/v1/executions?symbol=AAPL'].executions
    const accounts = after['GET /api/v1/accounts'].accounts
    const book = after['GET /api/v1/orderbook?symbol=AAPL']

    it('accepts the fixture bodies unchanged', async () => {
      const { client } = setup((url) =>
        jsonResponse(200, url.includes('/orders') ? { orders: bobOrders }
          : url.includes('/executions') ? { executions: trades }
          : url.includes('/orderbook') ? book : { accounts }))
      expect(await client.getOrders('bob')).toEqual(bobOrders)
      expect(await client.getExecutions('AAPL')).toEqual(trades)
      expect(await client.getOrderBook('AAPL')).toEqual(book)
      expect(await client.getAccounts()).toEqual(accounts)
    })

    it.each([
      ['a fractional price', { price: 19050.5 }],
      ['an unsafe quantity', { quantity: 2 ** 53 }],
      ['remaining above quantity', { remaining: 26 }],
      ['an unknown status', { status: 'OPEN' }],
      ['a missing sequence', { sequence: undefined }],
    ])('an order with %s', async (_name, change) => {
      const { client } = setup(() => jsonResponse(200, { orders: [{ ...bobOrders[0], ...change }] }))
      expect(await rejection(client.getOrders('bob'))).toMatchObject({ kind: 'invalid-response', status: 200 })
    })

    it('an execution with a string quantity', async () => {
      const { client } = setup(() => jsonResponse(200, { executions: [{ ...trades[0], quantity: '10' }] }))
      expect(await rejection(client.getExecutions('AAPL'))).toMatchObject({ kind: 'invalid-response' })
    })

    it('a book level with a fractional quantity', async () => {
      const bids = [{ ...book.bids[0], quantity: 1.5 }]
      const { client } = setup(() => jsonResponse(200, { ...book, bids }))
      expect(await rejection(client.getOrderBook('AAPL'))).toMatchObject({ kind: 'invalid-response' })
    })

    it('a balance whose available is not total minus reserved', async () => {
      const broken = [{ ...accounts[0], cash: { ...accounts[0].cash, available: accounts[0].cash.available + 1 } }]
      const { client } = setup(() => jsonResponse(200, { accounts: broken }))
      expect(await rejection(client.getAccounts())).toMatchObject({ kind: 'invalid-response' })
    })
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
