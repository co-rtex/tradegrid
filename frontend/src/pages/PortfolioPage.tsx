import { useEffect, useEffectEvent, useId } from 'react'
import { isServerUnavailable, tradeGridClient, type PortfolioDto, type TradeGridClient } from '../api/client'
import { describeLoadError } from '../api/describe'
import ConnectionStatus from '../components/ConnectionStatus'
import DemoAccountSelect from '../components/DemoAccountSelect'
import PortfolioSummary from '../components/PortfolioSummary'
import PositionsTable from '../components/PositionsTable'
import ProfitLossBreakdown from '../components/ProfitLossBreakdown'
import ServerHint from '../components/ServerHint'
import type { DemoAccount } from '../hooks/useDemoAccounts'
import { useRealtime, type RefreshRequest } from '../hooks/useRealtime'
import { useSnapshot } from '../hooks/useSnapshot'
import { formatUsdCents } from '../lib/money'
import { combinedPnl } from '../lib/portfolio'
import type { WebSocketLike } from '../lib/realtime'
import './PortfolioPage.css'

export interface PortfolioPageProps {
  /** REST client; tests pass a stub. */
  client?: TradeGridClient
  /** The demo accounts that can be selected (the app-level directory). */
  accounts: DemoAccount[]
  /** The app-level selected account. The page never picks one itself. */
  userId: string
  onUserChange: (userId: string) => void
  /** Called with the selected account's available cash for the navigation bar. */
  onBalanceChange?: (balance: string | undefined) => void
  /** Stream overrides for tests; the app uses the page's own host. */
  realtime?: { url?: string; createSocket?: (url: string) => WebSocketLike }
}

function usd(cents: number): string {
  try {
    return formatUsdCents(cents)
  } catch {
    return '—'
  }
}

/**
 * One account's portfolio, read from GET /api/v1/portfolio. Every amount is
 * the server's; the page only formats it. Switching account clears the view
 * before loading, and a late response for the previous account is dropped.
 */
export default function PortfolioPage({
  client = tradeGridClient,
  accounts,
  userId,
  onUserChange,
  onBalanceChange,
  realtime,
}: PortfolioPageProps) {
  const titleId = useId()
  const cashTitleId = useId()
  const [portfolio, loadPortfolio, cancelPortfolio] = useSnapshot<PortfolioDto>()

  const load = (id: string, keepData = false) =>
    loadPortfolio((signal) => client.getPortfolio(id, { signal }), { keepData })

  // On mount and whenever the app-level selection changes. Never keep the
  // previous account's values while the new ones load.
  const loadSelected = useEffectEvent((id: string) => {
    void load(id)
  })
  useEffect(() => {
    loadSelected(userId)
  }, [userId])
  useEffect(() => cancelPortfolio, [cancelPortfolio])

  /**
   * Live refresh (#96, docs/realtime.md). The portfolio changes only when a
   * committed submission involves this account, so an exchange.changed
   * re-reads it only when affectedUserIds contains the selected account.
   * hello, a reconnect, or a missed sequence re-read it; after a server
   * restart the old values are cleared first. A notification is never used
   * as data, and a selection change already loads above.
   */
  function refreshFromStream({ views, reason, selection }: RefreshRequest) {
    if (reason === 'selection-changed') return
    if (reason === 'changed' && !views.includes('portfolio')) return
    void load(selection.userId, reason !== 'server-restarted')
  }

  // The portfolio has no symbol selection, so symbol-only changes are ignored.
  const stream = useRealtime({
    selection: { userId, symbol: '' },
    onRefresh: refreshFromStream,
    url: realtime?.url,
    createSocket: realtime?.createSocket,
  })

  // A response is only ever for the account that was requested (the client
  // checks userId), and a stale one never reaches state; this guards the
  // render between a selection change and the new load starting.
  const data = portfolio.data?.userId === userId ? portfolio.data : null
  const accountName = data?.displayName ?? accounts.find((a) => a.userId === userId)?.displayName ?? userId
  const loading = portfolio.loading && data === null
  const error = portfolio.error === null ? null : describeLoadError(portfolio.error)

  const balance = data ? `${usd(data.cash.available)} available` : undefined
  useEffect(() => {
    onBalanceChange?.(balance)
  }, [balance, onBalanceChange])

  return (
    <section className="portfolio-page" aria-labelledby={titleId}>
      <header className="portfolio-page-header">
        <div className="portfolio-page-intro">
          <h2 id={titleId}>{accountName} portfolio</h2>
          <p className="portfolio-page-simulation">
            <strong>Simulated demo account.</strong> Positions are valued at fixed, simulated reference prices, not
            live market prices. Every value is calculated by the server, updates after each trade involving this
            account, and resets when the server restarts.
          </p>
        </div>
        <div className="portfolio-page-controls">
          <DemoAccountSelect accounts={accounts} value={userId} onChange={onUserChange} />
          <button type="button" className="portfolio-page-refresh" onClick={() => void load(userId, true)}>
            Refresh
          </button>
        </div>
        <p className="portfolio-page-refreshing" role="status">
          {portfolio.loading && data !== null ? 'Refreshing from the server…' : ''}
        </p>
        <div className="portfolio-page-live">
          <ConnectionStatus status={stream.state} />
          {(stream.state === 'disconnected' || stream.state === 'error') && (
            <div className="portfolio-page-live-actions">
              <p>Press Refresh to read the latest values, or reconnect to resume live updates.</p>
              <button type="button" className="portfolio-page-refresh" onClick={stream.retry}>
                Reconnect live updates
              </button>
            </div>
          )}
        </div>
      </header>

      {error !== null && (
        <div className="portfolio-page-message" role="alert">
          <p>Could not load the portfolio: {error}.</p>
          {isServerUnavailable(portfolio.error) && <ServerHint />}
        </div>
      )}
      {loading && (
        <p className="portfolio-page-message" role="status">
          Loading the portfolio…
        </p>
      )}

      {data && (
        <>
          <PortfolioSummary
            equity={data.totals.equity}
            cashTotal={data.cash.total}
            combinedPnl={combinedPnl(data.totals)}
            hasPositions={data.positions.some((position) => position.total > 0)}
          />
          <section className="portfolio-page-cash" aria-labelledby={cashTitleId}>
            <h2 id={cashTitleId}>Cash</h2>
            <dl>
              <div>
                <dt>Available</dt>
                <dd>{usd(data.cash.available)}</dd>
              </div>
              <div>
                <dt>Reserved by open BUY orders</dt>
                <dd>{usd(data.cash.reserved)}</dd>
              </div>
              <div>
                <dt>Total</dt>
                <dd>{usd(data.cash.total)}</dd>
              </div>
            </dl>
          </section>
          <PositionsTable positions={data.positions} />
          <ProfitLossBreakdown
            realizedPnl={data.totals.realizedPnl}
            unrealizedPnl={data.totals.unrealizedPnl}
            combinedPnl={combinedPnl(data.totals)}
          />
          <p className="portfolio-page-asof">
            As of exchange sequence {data.asOfSequence}. Equity is total cash plus the market value of all shares,
            reserved or not.
          </p>
        </>
      )}
    </section>
  )
}
