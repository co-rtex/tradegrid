import { useEffect, useEffectEvent, useId, useRef, useState } from 'react'
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
import { describeLoadError } from '../api/describe'
import type { DemoAccount } from '../hooks/useDemoAccounts'
import { useRealtime, type RefreshRequest } from '../hooks/useRealtime'
import { useSnapshot } from '../hooks/useSnapshot'
import { formatUsdCents, formatWholeShares } from '../lib/money'
import type { WebSocketLike } from '../lib/realtime'
import ConnectionStatus from './ConnectionStatus'
import DemoAccountSelect from './DemoAccountSelect'
import OrderBook from './OrderBook'
import OrderEntryForm, {
  type OrderEntryFieldErrors,
  type OrderEntryRequest,
  type OrderEntryValues,
} from './OrderEntryForm'
import OrderList from './OrderList'
import RecentTrades from './RecentTrades'
import ServerHint from './ServerHint'
import './TradingDesk.css'

export interface TradingDeskProps {
  /** REST client; tests pass a stub. */
  client?: TradeGridClient
  /** The demo accounts that can be selected (the app-level directory). */
  accounts: DemoAccount[]
  /** The app-level selected account. The desk never picks one itself. */
  userId: string
  onUserChange: (userId: string) => void
  /** Stream overrides for tests; the app uses the page's own host. */
  realtime?: { url?: string; createSocket?: (url: string) => WebSocketLike }
  /**
   * Called with the selected account's available cash, formatted for the
   * navigation bar, or `undefined` while no server value is known.
   */
  onBalanceChange?: (balance: string | undefined) => void
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

const idle: Submission = { status: 'idle' }

export default function TradingDesk({
  client = tradeGridClient,
  accounts: choices,
  userId,
  onUserChange,
  onBalanceChange,
  realtime,
}: TradingDeskProps) {
  const titleId = useId()
  const accountTitleId = useId()

  const [symbols, loadSymbols, cancelSymbols] = useSnapshot<SymbolDto[]>()
  const [accounts, loadAccounts, cancelAccounts] = useSnapshot<AccountDto[]>()
  const [orders, loadOrders, cancelOrders] = useSnapshot<OrderDto[]>()
  const [book, loadBook, cancelBook] = useSnapshot<BookSnapshotDto>()
  const [executions, loadExecutions, cancelExecutions] = useSnapshot<ExecutionDto[]>()

  // The symbols that can be selected, fixed once the first load succeeds so
  // the selector survives a failed refresh. The catalog never changes at
  // runtime.
  const [symbolChoices, setSymbolChoices] = useState<SymbolDto[] | null>(null)
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

  /** Loads the symbol list and selects the first symbol, then its market. */
  function bootstrap() {
    return loadSymbols((signal) => client.getSymbols({ signal })).then((loadedSymbols) => {
      if (!loadedSymbols) return
      setSymbolChoices(loadedSymbols)
      const firstSymbol = loadedSymbols[0]?.symbol
      if (firstSymbol === undefined) return
      setSymbol(firstSymbol)
      setForm((previous) => ({ ...previous, symbol: firstSymbol }))
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

  // The selected account's balances and orders: on mount and whenever the
  // app-level selection changes. Starting a load aborts the previous one, so
  // the previous account's late responses are dropped.
  const loadAccount = useEffectEvent((id: string) => {
    void loadAccounts((signal) => client.getAccounts({ signal }), { keepData: true })
    void loadAccountOrders(id)
  })
  useEffect(() => {
    loadAccount(userId)
  }, [userId])

  const ready = symbolChoices !== null && symbol !== null
  const pending = submission.status === 'pending'
  const selectedAccount = accounts.data?.find((account) => account.userId === userId) ?? null
  const accountName =
    selectedAccount?.displayName ?? choices.find((a) => a.userId === userId)?.displayName ?? userId

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
    clearSettledFeedback()
    onUserChange(next)
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
    if (next.symbol !== symbol && symbolChoices?.some((s) => s.symbol === next.symbol)) {
      selectSymbol(next.symbol)
    }
  }

  function refresh() {
    void loadAccounts((signal) => client.getAccounts({ signal }), { keepData: true })
    void loadAccountOrders(userId, true)
    if (!ready) {
      void bootstrap()
      return
    }
    void loadMarket(symbol, true)
  }

  /**
   * Live refresh (#77, docs/realtime.md). A notification is only a signal to
   * re-read the authoritative snapshots it affects for the selection current
   * when it is handled; it never carries data. Selection changes load
   * through the desk's own selectors, so they are not repeated here. After a
   * server restart every held value belongs to the old server, so it is
   * cleared rather than kept while reloading.
   */
  function refreshFromStream({ views, reason, selection }: RefreshRequest) {
    if (reason === 'selection-changed') return
    const keepData = reason !== 'server-restarted'
    if (!ready) {
      // The stream came up while the desk could not load: try again now.
      refresh()
      return
    }
    if (views.includes('account')) void loadAccounts((signal) => client.getAccounts({ signal }), { keepData })
    if (views.includes('orders')) void loadAccountOrders(selection.userId, keepData)
    if (views.includes('orderbook')) {
      void loadBook((signal) => client.getOrderBook(selection.symbol, { signal }), { keepData })
    }
    if (views.includes('executions')) {
      void loadExecutions((signal) => client.getExecutions(selection.symbol, { signal }), { keepData })
    }
  }

  const stream = useRealtime({
    selection: { userId, symbol: symbol ?? '' },
    onRefresh: refreshFromStream,
    url: realtime?.url,
    createSocket: realtime?.createSocket,
  })

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
      // The account selector is disabled while pending, so userId is still
      // request.userId.
      refresh()
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
  const startupError = symbols.error

  let body
  if (!symbolChoices) {
    body =
      startupError !== null ? (
        <div className="trading-desk-message" role="alert">
          <p>Could not load the trading desk: {describeLoadError(startupError)}.</p>
          {isServerUnavailable(startupError) && <ServerHint />}
        </div>
      ) : (
        <p className="trading-desk-message" role="status">
          Loading tradable symbols…
        </p>
      )
  } else if (symbolChoices.length === 0) {
    body = <p className="trading-desk-message">The server returned no tradable symbols, so there is nothing to trade.</p>
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
              data={book.data}
              isLoading={book.loading && book.data === null}
              error={book.error === null ? null : `Could not load the order book: ${describeLoadError(book.error)}`}
            />
            <RecentTrades
              executions={executions.data ?? []}
              isLoading={executions.loading && executions.data === null}
              error={
                executions.error === null ? null : `Could not load recent trades: ${describeLoadError(executions.error)}`
              }
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
          <DemoAccountSelect accounts={choices} value={userId} disabled={pending} onChange={selectUser} />
          {symbolChoices && symbol !== null && (
            <label className="trading-desk-control">
              <span>Market</span>
              <select value={symbol} disabled={pending} onChange={(event) => selectSymbol(event.target.value)}>
                {symbolChoices.map((s) => (
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
            disabled={pending || (!symbolChoices && symbols.loading)}
            onClick={() => refresh()}
          >
            Refresh
          </button>
        </div>
        <p className="trading-desk-refreshing" role="status">
          {refreshing ? 'Refreshing from the server…' : ''}
        </p>
        <div className="trading-desk-live">
          <ConnectionStatus status={stream.state} />
          {(stream.state === 'disconnected' || stream.state === 'error') && (
            <div className="trading-desk-live-actions">
              <p>Press Refresh to read the latest values, or reconnect to resume live updates.</p>
              <button type="button" className="trading-desk-refresh" onClick={stream.retry}>
                Reconnect live updates
              </button>
            </div>
          )}
        </div>
      </header>
      {body}
    </section>
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
