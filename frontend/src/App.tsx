import { useState } from 'react'
import TopNavigation, { type NavigationItem } from './components/TopNavigation'
import PortfolioSummary from './components/PortfolioSummary'
import TradingDesk from './components/TradingDesk'
import './App.css'

function App() {
  const [activeItem, setActiveItem] = useState<NavigationItem>('Dashboard')
  // Available cash of the account selected on the desk, from the server.
  const [balance, setBalance] = useState<string | undefined>()

  return (
    <>
      <TopNavigation activeItem={activeItem} onNavigate={setActiveItem} balance={balance} />
      <main className="app-shell">
        <p className="app-community">UW–Madison FinTech Club</p>
        <h1>{activeItem}</h1>
        <p className="app-description">A student-built simulated trading platform.</p>

        {/* Stays mounted while other views are shown, so selections and
            snapshots survive navigation without reloading. */}
        <div hidden={activeItem !== 'Dashboard'}>
          <TradingDesk onBalanceChange={setBalance} />
        </div>

        {activeItem === 'Portfolio' && (
          <div className="app-preview">
            {/* The card's values are built in (#23), not read from the
                server, so it is labeled and kept off the live desk. */}
            <p className="app-preview-label">
              <strong>Design preview with sample values.</strong> This card is not connected to your demo account.
              Live cash and share positions are on the Dashboard.
            </p>
            <PortfolioSummary />
          </div>
        )}

        {(activeItem === 'Orders' || activeItem === 'Markets') && (
          <p className="app-placeholder">
            This view is not built yet. Orders and market data for the selected demo account are on the Dashboard.
          </p>
        )}

        <p className="app-status">Built together. One contribution at a time.</p>
      </main>
    </>
  )
}

export default App
