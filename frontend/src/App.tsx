import './App.css'
import PortfolioSummary from './components/PortfolioSummary'

function App() {
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
  )
}

export default App
