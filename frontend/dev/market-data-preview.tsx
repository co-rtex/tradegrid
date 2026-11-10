import { createRoot } from 'react-dom/client'
import OrderBook from '../src/components/OrderBook'
import RecentTrades from '../src/components/RecentTrades'
import seed from '../../docs/fixtures/00-seed.json'
import noFill from '../../docs/fixtures/01-no-fill.json'
import fullFill from '../../docs/fixtures/02-full-fill.json'
import partialFill from '../../docs/fixtures/03-partial-fill.json'
import rejection from '../../docs/fixtures/04-risk-rejection.json'
import './market-data.css'

const fixtures = { seed, 'no-fill': noFill, 'full-fill': fullFill, 'partial-fill': partialFill, rejection }

export function mountPreview() {
  const scenario = new URLSearchParams(location.search).get('scenario') ?? 'partial-fill'
  const fixture = fixtures[scenario as keyof typeof fixtures] ?? partialFill
  createRoot(document.getElementById('root')!).render(
    <main>
      <p className="preview-eyebrow">TRADEGRID · DEVELOPMENT FIXTURES</p>
      <h1>Market data</h1>
      <nav aria-label="Fixture scenarios">
        {Object.keys(fixtures).map(name => <a key={name} href={`?scenario=${name}`} aria-current={name === scenario ? 'page' : undefined}>{name}</a>)}
      </nav>
      <p>{fixture.description}</p>
      <OrderBook data={fixture.after['GET /api/v1/orderbook?symbol=AAPL']} />
      <RecentTrades executions={fixture.after['GET /api/v1/executions?symbol=AAPL'].executions} />
    </main>,
  )
}
