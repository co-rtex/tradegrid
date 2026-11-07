import { useCallback, useEffect, useEffectEvent, useId, useRef, useState } from 'react'
import {
  ApiError,
  OrderOutcomeUnknownError,
  isServerUnavailable,
  tradeGridClient,
  type AccountDto,
  type BookSnapshotDto,
  type ExecutionDto,
  type OrderDto,
  type SubmitOrderResult,
  type SymbolDto,
  type TradeGridClient,
} from '../api/client'
import { formatUsdCents, formatWholeShares } from '../lib/money'
import OrderBook from './OrderBook'
import OrderEntryForm, {
  type OrderEntryFieldErrors,
  type OrderEntryRequest,
  type OrderEntryValues,
} from './OrderEntryForm'
import OrderList from './OrderList'
import RecentTrades from './RecentTrades'
import './TradingDesk.css'

export interface TradingDeskProps {
  /** REST client; tests pass a stub. */
  client?: TradeGridClient
  /**
   * Called with the selected account's available cash, formatted for the
   * navigation bar, or `undefined` while no server value is known.
   */
  onBalanceChange?: (balance: string | undefined) => void
}

// ---------------------------------------------------------------------------
// Snapshots

interface Snapshot<T> {
  data: T | null
  loading: boolean
  error: unknown
}

const emptySnapshot = { data: null, loading: false, error: null }

/**
 * One server snapshot (accounts, orders, book, or executions) and its loader.
 *
 * Starting a load aborts the previous one, and only the latest load may write
 * state: a response that arrives after the user switched account or symbol, or
 * after a newer refresh, is dropped even if the fetch ignored the abort.
 */
function useSnapshot<T>() {
  const [snapshot, setSnapshot] = useState<Snapshot<T>>(emptySnapshot)
  const latest = useRef<AbortController | null>(null)

  const load = useCallback(
    async (fetcher: (signal: AbortSignal) => Promise<T>, { keepData = false } = {}): Promise<T | undefined> => {
      latest.current?.abort()
      const controller = new AbortController()
      latest.current = controller
      setSnapshot((previous) => ({ data: keepData ? previous.data : null, loading: true, error: null }))
      try {
        const data = await fetcher(controller.signal)
        if (latest.current !== controller) return undefined // stale
        setSnapshot({ data, loading: false, error: null })
        return data
      } catch (error) {
        if (latest.current !== controller) return undefined // stale
        // Drop old values on failure rather than show them as current.
        setSnapshot({ data: null, loading: false, error })
        return undefined
      }
    },
    [],
  )

  const cancel = useCallback(() => {
    latest.current?.abort()
    latest.current = null
  }, [])

  return [snapshot, load, cancel] as const
}

// ---------------------------------------------------------------------------
// Text

/** Formats a server value; a malformed one shows a placeholder, never a guess. */
function safe(format: (value: number) => string, value: number): string {
  try {
    return format(value)
  } catch {
    return '—'
  }
}

const usd = (cents: number) => safe(formatUsdCents, cents)
const shares = (quantity: number) => safe(formatWholeShares, quantity)

/** Completes "Could not load …: ". */
function describeLoadError(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.kind) {
      case 'http':
        return `${error.message} (${error.code})`
      case 'network':
        return 'the server could not be reached'
      case 'timeout':
        return 'the server did not respond in time'
      case 'invalid-response':
        return error.status !== null && error.status >= 500
          ? `the server is unavailable (HTTP ${error.status})`
          : `the server sent an unexpected response (HTTP ${error.status ?? 'unknown'})`
      case 'aborted':
        return 'the request was cancelled'
    }
  }
  return 'an unexpected error occurred'
}

function describeUnknownReason(error: unknown): string {
  const reason = error instanceof OrderOutcomeUnknownError ? error.reason : error
  if (reason instanceof ApiError) {
    if (reason.kind === 'network') return 'the connection failed before a response arrived'
    if (reason.kind === 'timeout') return 'the server did not respond in time'
    if (reason.kind === 'invalid-response') {
      return `the response could not be read (HTTP ${reason.status ?? 'unknown'})`
    }
  }
  return 'the request failed unexpectedly'
}

const acceptedStatus: Record<string, string> = {
  NEW: 'resting on the book with no fills',
  PARTIALLY_FILLED: 'partially filled',
  FILLED: 'filled',
}

/** Describes the server's result as sent; nothing is computed here. */
function describeAccepted({ order, executions }: SubmitOrderResult, accountName: string): string {
  const status = acceptedStatus[order.status] ?? order.status
  const fills =
    executions.length === 0
      ? ''
      : ` ${executions.length === 1 ? '1 fill' : `${executions.length} fills`}: ` +
        executions.map((fill) => `${shares(fill.quantity)} at ${usd(fill.price)}`).join(', ') +
        '.'
  return `Order ${order.id} accepted for ${accountName}: ${status}, ${shares(order.remaining)} remaining.${fills}`
}

const formFields = ['symbol', 'side', 'quantity', 'price'] as const
type FormField = (typeof formFields)[number]
const isFormField = (field: string | null): field is FormField =>
  field !== null && (formFields as readonly string[]).includes(field)

// ---------------------------------------------------------------------------
// Component

type Submission =
  | { status: 'idle' }
  | { status: 'pending' }
  | { status: 'accepted'; message: string }
  | { status: 'rejected'; message: string; fieldErrors: OrderEntryFieldErrors }
  | { status: 'unknown'; message: string }

interface Choices {
  accounts: Pick<AccountDto, 'userId' | 'displayName'>[]
  symbols: SymbolDto[]
}

const idle: Submission = { status: 'idle' }

export default function TradingDesk({ client = tradeGridClient, onBalanceChange }: TradingDeskProps) {
  const titleId = useId()
  const accountTitleId = useId()

  const [symbols, loadSymbols, cancelSymbols] = useSnapshot<SymbolDto[]>()
  const [accounts, loadAccounts, cancelAccounts] = useSnapshot<AccountDto[]>()
  const [orders, loadOrders, cancelOrders] = useSnapshot<OrderDto[]>()
  const [book, loadBook, cancelBook] = useSnapshot<BookSnapshotDto>()
  const [executions, loadExecutions, cancelExecutions] = useSnapshot<ExecutionDto[]>()

  // Who and what can be selected, fixed once the first load succeeds so the
  // selectors survive a failed refresh. Demo identities never change at runtime.
  const [choices, setChoices] = useState<Choices | null>(null)
  const [userId, setUserId] = useState<string | null>(null)
  const [symbol, setSymbol] = useState<string | null>(null)
  const [form, setForm] = useState<OrderEntryValues>({ symbol: '', side: 'BUY', quantity: '', price: '' })
  const [submission, setSubmission] = useState<Submission>(idle)
  const submitting = useRef(false)

  const loadAccountOrders = (id: string, keepData = false) =>
    loadOrders((signal) => client.getOrders(id, { signal }), { keepData })

  const loadMarket = (sym: string, keepData = false) =>
    Promise.all([
      loadBook((signal) => client.getOrderBook(sym, { signal }), { keepData }),
      loadExecutions((signal) => client.getExecutions(sym, { signal }), { keepData }),
    ])

  /** Loads the account and symbol lists, then selects the first of each. */
  function bootstrap() {
    return Promise.all([
      loadSymbols((signal) => client.getSymbols({ signal })),
      loadAccounts((signal) => client.getAccounts({ signal })),
    ]).then(([loadedSymbols, loadedAccounts]) => {
      if (!loadedSymbols || !loadedAccounts) return
      setChoices({
        accounts: loadedAccounts.map(({ userId, displayName }) => ({ userId, displayName })),
        symbols: loadedSymbols,
      })
      const firstUser = loadedAccounts[0]?.userId
      const firstSymbol = loadedSymbols[0]?.symbol
      if (firstUser === undefined || firstSymbol === undefined) return
      setUserId(firstUser)
      setSymbol(firstSymbol)
      setForm((previous) => ({ ...previous, symbol: firstSymbol }))
      void loadAccountOrders(firstUser)
      void loadMarket(firstSymbol)
    })
  }

  // Load once per mount. Unmounting cancels every load so no late response
  // writes state (StrictMode's extra mount in development relies on this).
  const startLoading = useEffectEvent(() => {
    void bootstrap()
  })
  useEffect(() => {
    startLoading()
    return () => {
      for (const cancel of [cancelSymbols, cancelAccounts, cancelOrders, cancelBook, cancelExecutions]) cancel()
    }
  }, [cancelSymbols, cancelAccounts, cancelOrders, cancelBook, cancelExecutions])

  const ready = choices !== null && userId !== null && symbol !== null
  const pending = submission.status === 'pending'
  const selectedAccount = accounts.data?.find((account) => account.userId === userId) ?? null
  const accountName =
    selectedAccount?.displayName ?? choices?.accounts.find((a) => a.userId === userId)?.displayName ?? userId ?? ''

  const balance = selectedAccount ? `${usd(selectedAccount.cash.available)} available` : undefined
  useEffect(() => {
    onBalanceChange?.(balance)
  }, [balance, onBalanceChange])

  /** Success and rejection describe the last submission only; an unknown outcome stays until the next submit. */
  const clearSettledFeedback = () =>
    setSubmission((previous) =>
      previous.status === 'accepted' || previous.status === 'rejected' ? idle : previous,
    )

  function selectUser(next: string) {
    if (next === userId) return
    setUserId(next)
    clearSettledFeedback()
    void loadAccountOrders(next)
  }

  function selectSymbol(next: string) {
    if (next === symbol) return
    setSymbol(next)
    setForm((previous) => ({ ...previous, symbol: next }))
    clearSettledFeedback()
    void loadMarket(next)
  }

  function changeForm(next: OrderEntryValues) {
    setForm(next)
    clearSettledFeedback()
    // One symbol drives both the form and the market panels.
    if (next.symbol !== symbol && choices?.symbols.some((s) => s.symbol === next.symbol)) {
      selectSymbol(next.symbol)
    }
  }

  function refresh(target: { userId: string | null; symbol: string | null } = { userId, symbol }) {
    if (!ready) {
      void bootstrap()
      return
    }
    void loadAccounts((signal) => client.getAccounts({ signal }), { keepData: true })
    if (target.userId !== null) void loadAccountOrders(target.userId, true)
    if (target.symbol !== null) void loadMarket(target.symbol, true)
  }

  async function submit(request: OrderEntryRequest) {
    if (submitting.current) return
    submitting.current = true
    setSubmission({ status: 'pending' })
    const who = accountName || request.userId
    try {
      // Sent once. The client never retries a POST, and neither does the desk.
      const result = await client.submitOrder(request)
      setSubmission({ status: 'accepted', message: describeAccepted(result, who) })
      // The response omits updated balances and resting orders; read them.
      refresh({ userId: request.userId, symbol })
    } catch (error) {
      if (error instanceof ApiError && error.kind === 'http') {
        const field = isFormField(error.field) ? error.field : null
        setSubmission({
          status: 'rejected',
          message: field
            ? `Order rejected by the server (${error.code}). No order was created. Correct the highlighted field and submit again.`
            : `Order rejected by the server: ${error.message} (${error.code}). No order was created.`,
          fieldErrors: field ? { [field]: error.message } : {},
        })
      } else {
        // Anything else may have reached the exchange: never report success or rejection.
        setSubmission({
          status: 'unknown',
          message:
            `Order outcome unknown — refresh to check. Your ${request.side} order for ` +
            `${shares(request.quantity)} of ${request.symbol} at ${usd(request.price)} (${who}) ` +
            `may or may not have been accepted: ${describeUnknownReason(error)}. ` +
            'Press Refresh and check the order list before submitting again.',
        })
      }
    } finally {
      submitting.current = false
    }
  }

  const snapshots = [symbols, accounts, orders, book, executions]
  const offline = snapshots.some((snapshot) => isServerUnavailable(snapshot.error))
  const refreshing = ready && [accounts, orders, book, executions].some((snapshot) => snapshot.loading)
  const startupError = symbols.error ?? accounts.error

  let body
  if (!choices) {
    body =
      startupError !== null ? (
        <div className="trading-desk-message" role="alert">
          <p>Could not load the trading desk: {describeLoadError(startupError)}.</p>
          {isServerUnavailable(startupError) && <ServerHint />}
        </div>
      ) : (
        <p className="trading-desk-message" role="status">
          Loading demo accounts and symbols…
        </p>
      )
  } else if (choices.accounts.length === 0 || choices.symbols.length === 0) {
    body = (
      <p className="trading-desk-message">
        The server returned no {choices.accounts.length === 0 ? 'demo accounts' : 'tradable symbols'}, so there is
        nothing to trade.
      </p>
    )
  } else if (ready) {
    body = (
      <>
        {offline && (
          <div className="trading-desk-offline" role="alert">
            <p>
              <strong>The TradeGrid server is not responding.</strong> Panels that could not load show an error
              instead of old values.
            </p>
            <ServerHint />
          </div>
        )}

        <section className="trading-desk-account" aria-labelledby={accountTitleId}>
          <h2 id={accountTitleId}>{accountName} balances</h2>
          <AccountBalances account={selectedAccount} loading={accounts.loading} error={accounts.error} />
        </section>

        <div className="trading-desk-grid">
          <div className="trading-desk-entry">
            <OrderEntryForm
              userId={userId}
              value={form}
              onChange={changeForm}
              onSubmit={(request) => void submit(request)}
              pending={pending}
              success={submission.status === 'accepted' ? submission.message : undefined}
              fieldErrors={submission.status === 'rejected' ? submission.fieldErrors : undefined}
              submissionError={
                submission.status === 'rejected' || submission.status === 'unknown' ? submission.message : undefined
              }
            />
          </div>
          <div className="trading-desk-market">
            <OrderBook
              symbol={symbol}
              book={book.data}
              isLoading={book.loading && book.data === null}
              error={book.error === null ? null : describeLoadError(book.error)}
            />
            <RecentTrades
              symbol={symbol}
              executions={executions.data ?? []}
              isLoading={executions.loading && executions.data === null}
              error={executions.error === null ? null : describeLoadError(executions.error)}
            />
          </div>
          <div className="trading-desk-orders">
            <OrderList
              accountName={accountName}
              orders={orders.data ?? []}
              isLoading={orders.loading && orders.data === null}
              error={orders.error === null ? null : describeLoadError(orders.error)}
            />
          </div>
        </div>
      </>
    )
  }

  return (
    <section className="trading-desk" aria-labelledby={titleId}>
      <header className="trading-desk-header">
        <div className="trading-desk-intro">
          <h2 id={titleId}>Trading desk</h2>
          <p className="trading-desk-simulation">
            <strong>Simulated demo accounts.</strong> No real money or securities are involved. Choosing an account
            is not a sign-in, and all accounts, orders, and trades reset when the server restarts.
          </p>
        </div>
        <div className="trading-desk-controls">
          {choices && userId !== null && (
            <label className="trading-desk-control">
              <span>Demo account</span>
              <select value={userId} disabled={pending} onChange={(event) => selectUser(event.target.value)}>
                {choices.accounts.map((account) => (
                  <option key={account.userId} value={account.userId}>
                    {account.displayName}
                  </option>
                ))}
              </select>
            </label>
          )}
          {choices && symbol !== null && (
            <label className="trading-desk-control">
              <span>Market</span>
              <select value={symbol} disabled={pending} onChange={(event) => selectSymbol(event.target.value)}>
                {choices.symbols.map((s) => (
                  <option key={s.symbol} value={s.symbol}>
                    {s.symbol} — {s.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          <button
            type="button"
            className="trading-desk-refresh"
            disabled={pending || (!choices && (symbols.loading || accounts.loading))}
            onClick={() => refresh()}
          >
            Refresh
          </button>
        </div>
        <p className="trading-desk-refreshing" role="status">
          {refreshing ? 'Refreshing from the server…' : ''}
        </p>
      </header>
      {body}
    </section>
  )
}

function ServerHint() {
  return (
    <p>
      Check that the backend is running (by default on 127.0.0.1:8080), then press Refresh. If it restarted, every
      demo account is back to its starting balance.
    </p>
  )
}

function AccountBalances({
  account,
  loading,
  error,
}: {
  account: AccountDto | null
  loading: boolean
  error: unknown
}) {
  if (error !== null) return <p role="alert">Could not load account balances: {describeLoadError(error)}</p>
  if (!account) {
    return loading ? <p role="status">Loading account balances…</p> : <p>This account is not on the server.</p>
  }
  return (
    <>
      <dl className="trading-desk-cash">
        <div>
          <dt>Cash available</dt>
          <dd>{usd(account.cash.available)}</dd>
        </div>
        <div>
          <dt>Cash reserved</dt>
          <dd>{usd(account.cash.reserved)}</dd>
        </div>
        <div>
          <dt>Cash total</dt>
          <dd>{usd(account.cash.total)}</dd>
        </div>
      </dl>
      <div className="trading-desk-scroll" role="group" aria-label="Share positions" tabIndex={0}>
        <table className="trading-desk-positions">
          <caption>Share positions</caption>
          <thead>
            <tr>
              <th scope="col">Symbol</th>
              <th scope="col">Available</th>
              <th scope="col">Reserved</th>
              <th scope="col">Total</th>
            </tr>
          </thead>
          <tbody>
            {account.positions.map((position) => (
              <tr key={position.symbol}>
                <th scope="row">{position.symbol}</th>
                <td>{shares(position.available)}</td>
                <td>{shares(position.reserved)}</td>
                <td>{shares(position.total)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="trading-desk-note">
        Server values from the last load. Reserved cash and shares back this account&apos;s open orders.
      </p>
    </>
  )
}
