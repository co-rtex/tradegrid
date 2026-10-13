import './PortfolioSummary.css'

const portfolio = {
  cashBalance: 100000,
  totalValue: 100000,
  profitLoss: 0,
  positions: [],
}

const currency = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
})

function PortfolioSummary() {
  return (
    <section className="portfolio-summary" aria-label="Portfolio summary">
      <h2 className="portfolio-heading">Portfolio</h2>

      <div className="portfolio-total">
        <div>
          <p className="portfolio-label">Total Portfolio Value</p>
          <p className="portfolio-value">{currency.format(portfolio.totalValue)}</p>
        </div>
        <svg className="portfolio-chart" viewBox="0 0 240 100" aria-hidden="true" focusable="false">
          <path className="portfolio-chart-fill" d="M4 88 C34 88 38 68 66 72 S106 46 132 51 S174 25 196 30 S222 12 236 8 V100 H4 Z" />
          <path className="portfolio-chart-line" d="M4 88 C34 88 38 68 66 72 S106 46 132 51 S174 25 196 30 S222 12 236 8" />
        </svg>
      </div>

      <dl className="portfolio-metrics">
        <div>
          <dt className="portfolio-label">Cash Balance</dt>
          <dd>{currency.format(portfolio.cashBalance)}</dd>
        </div>
        <div>
          <dt className="portfolio-label">P&amp;L</dt>
          <dd>{currency.format(portfolio.profitLoss)}</dd>
        </div>
      </dl>

      {portfolio.positions.length === 0 && (
        <div className="portfolio-empty">
          <svg className="portfolio-icon" viewBox="0 0 48 48" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
            <rect x="7" y="15" width="34" height="26" rx="4" />
            <path d="M17 15 V11 A3 3 0 0 1 20 8 H28 A3 3 0 0 1 31 11 V15 M7 24 Q24 34 41 24 M21 27 H27 V32 H21 Z" />
          </svg>
          <h3>No positions yet</h3>
          <p>Place your first trade to start building your portfolio.</p>
        </div>
      )}
    </section>
  )
}

export default PortfolioSummary
