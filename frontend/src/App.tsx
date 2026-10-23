import { useState } from 'react'
import TopNavigation, { type NavigationItem } from './components/TopNavigation'
import './App.css'
import PortfolioSummary from './components/PortfolioSummary'

function App() {
  const [activeItem, setActiveItem] = useState<NavigationItem>('Dashboard')

  return (
    <main className="app-shell">
      <p className="app-community">UW–Madison FinTech Club</p>
      <h1>TradeGrid</h1>
      <p className="app-description">
        A student-built simulated trading platform.
      </p>
      <p className="app-status">Built together. One contribution at a time.</p>
      <PortfolioSummary />
    </main>
    <>
      <TopNavigation activeItem={activeItem} onNavigate={setActiveItem} />
      <main className="app-shell">
        <p className="app-community">UW–Madison FinTech Club</p>
        <h1>{activeItem}</h1>
        <p className="app-description">
          A student-built simulated trading platform.
        </p>
        <p className="app-status">Built together. One contribution at a time.</p>
      </main>
    </>
  )
}

export default App
