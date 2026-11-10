import { useState } from 'react'
import { isServerUnavailable, tradeGridClient, type TradeGridClient } from './api/client'
import { describeLoadError } from './api/describe'
import ServerHint from './components/ServerHint'
import TopNavigation, { type NavigationItem } from './components/TopNavigation'
import TradingDesk from './components/TradingDesk'
import { useDemoAccounts, useSelectedAccount } from './hooks/useDemoAccounts'
import PortfolioPage from './pages/PortfolioPage'
import type { WebSocketLike } from './lib/realtime'
import './App.css'

export interface AppProps {
  /** REST client; tests pass a stub. */
  client?: TradeGridClient
  /** Real-time stream overrides for tests; the app uses the page's own host. */
  realtime?: { url?: string; createSocket?: (url: string) => WebSocketLike }
}

function App({ client = tradeGridClient, realtime }: AppProps) {
  const [activeItem, setActiveItem] = useState<NavigationItem>('Dashboard')
  // Available cash of the selected account, reported by the view showing it.
  const [balance, setBalance] = useState<string | undefined>()
  // One account selection for the whole app: every view that shows account
  // data receives it and renders the same selector bound to it.
  const [directory, reloadDirectory] = useDemoAccounts(client)
  const accounts = directory.status === 'ready' ? directory.accounts : null
  const [userId, setUserId] = useSelectedAccount(accounts)

  function navigate(item: NavigationItem) {
    // The next view reports its own balance; never show the previous one's.
    if (item !== activeItem) setBalance(undefined)
    setActiveItem(item)
  }

  let view
  if (activeItem === 'Orders' || activeItem === 'Markets') {
    view = (
      <p className="app-placeholder">
        This view is not built yet. Orders and market data for the selected demo account are on the Dashboard.
      </p>
    )
  } else if (directory.status === 'loading') {
    view = (
      <p className="app-message" role="status">
        Loading demo accounts…
      </p>
    )
  } else if (directory.status === 'error') {
    view = (
      <div className="app-message" role="alert">
        <p>Could not load the demo accounts: {describeLoadError(directory.error)}.</p>
        {isServerUnavailable(directory.error) && <ServerHint />}
        <button type="button" className="app-retry" onClick={reloadDirectory}>
          Retry
        </button>
      </div>
    )
  } else if (accounts === null || userId === null) {
    view = <p className="app-message">The server returned no demo accounts, so there is nothing to show.</p>
  } else if (activeItem === 'Dashboard') {
    // Only the active view is mounted, so at most one view holds live
    // connections or in-flight reads at a time.
    view = (
      <TradingDesk
        client={client}
        accounts={accounts}
        userId={userId}
        onUserChange={setUserId}
        onBalanceChange={setBalance}
        realtime={realtime}
      />
    )
  } else {
    view = (
      <PortfolioPage
        client={client}
        accounts={accounts}
        userId={userId}
        onUserChange={setUserId}
        onBalanceChange={setBalance}
        realtime={realtime}
      />
    )
  }

  return (
    <>
      <TopNavigation activeItem={activeItem} onNavigate={navigate} balance={balance} />
      <main className="app-shell">
        <p className="app-community">UW–Madison FinTech Club</p>
        <h1>{activeItem}</h1>
        <p className="app-description">A student-built simulated trading platform.</p>
        {view}
        <p className="app-status">Built together. One contribution at a time.</p>
      </main>
    </>
  )
}

export default App
