/**
 * Small typed client for the Sprint 2 REST API (docs/api.md).
 *
 * Every failure is an `ApiError` whose `kind` says what went wrong:
 *
 * - `http`: the server answered with the contracted error body. `code`,
 *   `field`, and the server's `message` are available. For a POST this is a
 *   definite rejection: the contract says nothing was stored.
 * - `network`: no response arrived (server down, connection reset).
 * - `timeout`: no complete response arrived within `timeoutMs`.
 * - `invalid-response`: a response arrived but was not JSON or did not have
 *   the contracted shape (for example the dev proxy's empty 502).
 * - `aborted`: the caller's AbortSignal cancelled the request.
 *
 * `submitOrder` is never retried. When it cannot tell whether the server
 * accepted the order (network failure, timeout, unreadable response) it throws
 * `OrderOutcomeUnknownError` instead, so callers cannot mistake that case for
 * success or for a definite rejection.
 */
import type { OrderBookSnapshot } from '../components/OrderBook'
import type { OrderEntryRequest } from '../components/OrderEntryForm'
import type { OrderDto } from '../components/OrderList'
import type { PortfolioPositionDto } from '../components/PositionsTable'
import type { Execution } from '../components/RecentTrades'

export type { OrderDto, OrderEntryRequest }
/** `GET /api/v1/orderbook`, as the OrderBook component displays it. */
export type BookSnapshotDto = OrderBookSnapshot
/** One execution record, as the RecentTrades component displays it. */
export type ExecutionDto = Execution

export interface SymbolDto {
  symbol: string
  name: string
}

/** Integer cents for cash, whole shares for positions; `available = total - reserved` on the server. */
export interface BalanceDto {
  total: number
  reserved: number
  available: number
}

export interface PositionDto extends BalanceDto {
  symbol: string
}

export interface AccountDto {
  userId: string
  displayName: string
  cash: BalanceDto
  positions: PositionDto[]
}

export type { PortfolioPositionDto }

/** Sums over `positions`, plus `equity = cash.total + marketValue` (docs/portfolio.md). */
export interface PortfolioTotalsDto {
  costBasis: number
  marketValue: number
  unrealizedPnl: number
  realizedPnl: number
  equity: number
}

/**
 * `GET /api/v1/portfolio?userId=...`. Every amount is calculated by the
 * server in integer cents at fixed simulated reference prices; the browser
 * only formats them.
 */
export interface PortfolioDto {
  userId: string
  displayName: string
  /** Acceptance sequence of the last committed submission included. */
  asOfSequence: number
  cash: BalanceDto
  positions: PortfolioPositionDto[]
  totals: PortfolioTotalsDto
}

export interface SubmitOrderResult {
  order: OrderDto
  /** Fills this submission produced, in matching order; `[]` when the order only rested. */
  executions: ExecutionDto[]
}

export type ApiErrorKind = 'http' | 'network' | 'timeout' | 'invalid-response' | 'aborted'

export class ApiError extends Error {
  readonly kind: ApiErrorKind
  /** HTTP status when a response arrived, otherwise `null`. */
  readonly status: number | null
  /** Contracted error code, present only when `kind` is `http`. */
  readonly code: string | null
  /** Offending request field or query parameter, when the server named one. */
  readonly field: string | null

  constructor(
    kind: ApiErrorKind,
    message: string,
    details: { status?: number | null; code?: string | null; field?: string | null; cause?: unknown } = {},
  ) {
    super(message, details.cause === undefined ? undefined : { cause: details.cause })
    this.name = 'ApiError'
    this.kind = kind
    this.status = details.status ?? null
    this.code = details.code ?? null
    this.field = details.field ?? null
  }
}

/**
 * The order may or may not have been accepted. The only safe next step is to
 * read the account's orders again; resubmitting could place a duplicate.
 */
export class OrderOutcomeUnknownError extends Error {
  readonly reason: ApiError

  constructor(reason: ApiError) {
    super('Order outcome unknown — refresh to check', { cause: reason })
    this.name = 'OrderOutcomeUnknownError'
    this.reason = reason
  }
}

export interface RequestOptions {
  signal?: AbortSignal
}

export interface TradeGridClient {
  getSymbols(options?: RequestOptions): Promise<SymbolDto[]>
  getAccounts(options?: RequestOptions): Promise<AccountDto[]>
  getOrders(userId: string, options?: RequestOptions & { limit?: number }): Promise<OrderDto[]>
  getOrderBook(symbol: string, options?: RequestOptions & { depth?: number }): Promise<BookSnapshotDto>
  getExecutions(
    symbol: string,
    options?: RequestOptions & { userId?: string; limit?: number },
  ): Promise<ExecutionDto[]>
  /** The account's server-valued portfolio. A response for another account is an invalid response. */
  getPortfolio(userId: string, options?: RequestOptions): Promise<PortfolioDto>
  /** Sends exactly one POST. Never retried; see `OrderOutcomeUnknownError`. */
  submitOrder(request: OrderEntryRequest): Promise<SubmitOrderResult>
}

export interface ClientOptions {
  /** Defaults to `/api/v1`, served by the Vite proxy in development. */
  baseUrl?: string
  /** Defaults to the global `fetch`, looked up on each call. */
  fetch?: typeof fetch
  /** Per-request limit in milliseconds, including reading the body. `0` disables it. */
  timeoutMs?: number
}

export const DEFAULT_TIMEOUT_MS = 10_000

type Json = unknown
type Validate<T> = (body: Json) => T | undefined

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isArrayOfRecords(value: unknown): value is Record<string, unknown>[] {
  return Array.isArray(value) && value.every(isRecord)
}

/** Money and quantities are integers on the wire (docs/api.md); a fraction is not a valid amount. */
const isAmount = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value)

function isBalance(value: unknown): value is BalanceDto {
  return isRecord(value) && isAmount(value.total) && isAmount(value.reserved) && isAmount(value.available)
}

/** `{ "<key>": [ {...}, ... ] }` where every item passes `item`. */
function collection<T>(key: string, item: (value: Record<string, unknown>) => boolean = () => true): Validate<T[]> {
  return (body) => {
    if (!isRecord(body)) return undefined
    const items = body[key]
    return isArrayOfRecords(items) && items.every(item) ? (items as T[]) : undefined
  }
}

const validateSymbols = collection<SymbolDto>(
  'symbols',
  (s) => typeof s.symbol === 'string' && typeof s.name === 'string',
)

const validateAccounts = collection<AccountDto>(
  'accounts',
  (a) =>
    typeof a.userId === 'string' &&
    typeof a.displayName === 'string' &&
    isBalance(a.cash) &&
    Array.isArray(a.positions) &&
    a.positions.every((p) => isRecord(p) && typeof p.symbol === 'string' && isBalance(p)),
)

const isOrder = (o: Record<string, unknown>) =>
  typeof o.id === 'string' && typeof o.status === 'string' && typeof o.remaining === 'number'
const isExecution = (e: Record<string, unknown>) => typeof e.id === 'string' && typeof e.price === 'number'

const validateOrders = collection<OrderDto>('orders', isOrder)
const validateExecutions = collection<ExecutionDto>('executions', isExecution)

const validateBook: Validate<BookSnapshotDto> = (body) =>
  isRecord(body) &&
  typeof body.symbol === 'string' &&
  typeof body.asOfSequence === 'number' &&
  isArrayOfRecords(body.bids) &&
  isArrayOfRecords(body.asks)
    ? (body as unknown as BookSnapshotDto)
    : undefined

const portfolioPositionFields = [
  'total', 'reserved', 'available', 'costBasis', 'referencePrice', 'marketValue', 'unrealizedPnl', 'realizedPnl',
] as const
const portfolioTotalFields = ['costBasis', 'marketValue', 'unrealizedPnl', 'realizedPnl', 'equity'] as const

/** Requires every contracted field, as integers, and the account that was asked for. */
function validatePortfolio(userId: string): Validate<PortfolioDto> {
  return (body) =>
    isRecord(body) &&
    body.userId === userId &&
    typeof body.displayName === 'string' &&
    isAmount(body.asOfSequence) &&
    isBalance(body.cash) &&
    isArrayOfRecords(body.positions) &&
    body.positions.every((p) => typeof p.symbol === 'string' && portfolioPositionFields.every((f) => isAmount(p[f]))) &&
    isRecord(body.totals) &&
    portfolioTotalFields.every((f) => isAmount((body.totals as Record<string, unknown>)[f]))
      ? (body as unknown as PortfolioDto)
      : undefined
}

const validateSubmit: Validate<SubmitOrderResult> = (body) =>
  isRecord(body) &&
  isRecord(body.order) &&
  isOrder(body.order) &&
  isArrayOfRecords(body.executions) &&
  body.executions.every(isExecution)
    ? (body as unknown as SubmitOrderResult)
    : undefined

/** The contracted error body: `{ "error": { "code", "message", "field"? } }`. */
function parseErrorBody(body: Json): { code: string; message: string; field: string | null } | undefined {
  if (!isRecord(body) || !isRecord(body.error)) return undefined
  const { code, message, field } = body.error
  if (typeof code !== 'string' || code === '' || typeof message !== 'string') return undefined
  if (field !== undefined && typeof field !== 'string') return undefined
  return { code, message, field: field ?? null }
}

function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined) search.set(key, String(value))
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

export function createTradeGridClient(options: ClientOptions = {}): TradeGridClient {
  const baseUrl = (options.baseUrl ?? '/api/v1').replace(/\/+$/, '')
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS
  // Look the global up per call (and call it unbound from any object) so test
  // stubs installed after creation are used and browsers do not throw
  // "Illegal invocation".
  const fetchImpl: typeof fetch = options.fetch ?? ((input, init) => globalThis.fetch(input, init))

  async function send<T>(
    method: 'GET' | 'POST',
    path: string,
    validate: Validate<T>,
    init: { body?: string; signal?: AbortSignal } = {},
  ): Promise<T> {
    const label = `${method} ${path}`
    const controller = new AbortController()
    const external = init.signal
    const forwardAbort = () => controller.abort(external?.reason)
    if (external?.aborted) forwardAbort()
    else external?.addEventListener('abort', forwardAbort, { once: true })

    let timedOut = false
    const timer =
      timeoutMs > 0
        ? setTimeout(() => {
            timedOut = true
            controller.abort()
          }, timeoutMs)
        : undefined

    // Classifies a rejection from fetch() or from reading the body.
    const transportError = (cause: unknown) => {
      if (external?.aborted) return new ApiError('aborted', `${label} was cancelled`, { cause })
      if (timedOut) return new ApiError('timeout', `${label} did not complete within ${timeoutMs} ms`, { cause })
      return new ApiError('network', `${label} failed: the server could not be reached`, { cause })
    }

    try {
      let response: Response
      try {
        response = await fetchImpl(`${baseUrl}${path}`, {
          method,
          headers: init.body === undefined ? { Accept: 'application/json' } : {
            Accept: 'application/json',
            'Content-Type': 'application/json',
          },
          body: init.body,
          signal: controller.signal,
        })
      } catch (cause) {
        throw transportError(cause)
      }

      let text: string
      try {
        text = await response.text()
      } catch (cause) {
        throw transportError(cause)
      }

      let body: Json
      try {
        body = JSON.parse(text)
      } catch (cause) {
        throw new ApiError('invalid-response', `${label} returned HTTP ${response.status} with a non-JSON body`, {
          status: response.status,
          cause,
        })
      }

      if (!response.ok) {
        const error = parseErrorBody(body)
        if (!error) {
          throw new ApiError(
            'invalid-response',
            `${label} returned HTTP ${response.status} without the contracted error body`,
            { status: response.status },
          )
        }
        throw new ApiError('http', error.message, { status: response.status, code: error.code, field: error.field })
      }

      const value = validate(body)
      if (value === undefined) {
        throw new ApiError('invalid-response', `${label} returned an unexpected response shape`, {
          status: response.status,
        })
      }
      return value
    } finally {
      clearTimeout(timer)
      external?.removeEventListener('abort', forwardAbort)
    }
  }

  return {
    getSymbols: ({ signal } = {}) => send('GET', '/symbols', validateSymbols, { signal }),
    getAccounts: ({ signal } = {}) => send('GET', '/accounts', validateAccounts, { signal }),
    getOrders: (userId, { signal, limit } = {}) =>
      send('GET', `/orders${query({ userId, limit })}`, validateOrders, { signal }),
    getOrderBook: (symbol, { signal, depth } = {}) =>
      send('GET', `/orderbook${query({ symbol, depth })}`, validateBook, { signal }),
    getExecutions: (symbol, { signal, userId, limit } = {}) =>
      send('GET', `/executions${query({ symbol, userId, limit })}`, validateExecutions, { signal }),
    getPortfolio: (userId, { signal } = {}) =>
      send('GET', `/portfolio${query({ userId })}`, validatePortfolio(userId), { signal }),

    async submitOrder(request) {
      // Send only the six client-owned fields; anything else is UNKNOWN_FIELD.
      const { userId, symbol, side, type, price, quantity } = request
      const body = JSON.stringify({ userId, symbol, side, type, price, quantity })
      try {
        return await send('POST', '/orders', validateSubmit, { body })
      } catch (error) {
        // Only the contracted error body proves the order was not stored.
        if (error instanceof ApiError && error.kind === 'http') throw error
        const reason =
          error instanceof ApiError
            ? error
            : new ApiError('invalid-response', 'POST /orders failed unexpectedly', { cause: error })
        throw new OrderOutcomeUnknownError(reason)
      }
    },
  }
}

/** Shared client for the app; components accept another one for tests. */
export const tradeGridClient = createTradeGridClient()

/**
 * True when the error means the server is down or unreachable rather than
 * that it refused the request: no response, a timeout, or a 5xx that is not
 * the contracted error body (the dev proxy answers this way when the backend
 * is not running).
 */
export function isServerUnavailable(error: unknown): boolean {
  if (error instanceof OrderOutcomeUnknownError) return isServerUnavailable(error.reason)
  if (!(error instanceof ApiError)) return false
  if (error.kind === 'network' || error.kind === 'timeout') return true
  return error.kind === 'invalid-response' && error.status !== null && error.status >= 500
}
