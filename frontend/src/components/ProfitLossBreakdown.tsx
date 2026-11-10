import { useId } from 'react'
import './ProfitLossBreakdown.css'
import { formatUsdCents } from '../lib/money'

/**
 * Already calculated amounts in integer cents, from `totals` in
 * GET /api/v1/portfolio (docs/portfolio.md). Null or undefined means the amount
 * is not known, which is shown as unavailable and never as $0.00.
 */
export interface ProfitLossBreakdownProps {
  realizedPnl?: number | null
  unrealizedPnl?: number | null
  combinedPnl?: number | null
  isLoading?: boolean
  error?: string | null
}

type Amount = { kind: 'loading' } | { kind: 'unavailable' } | { kind: 'known'; cents: number; text: string }

function resolveAmount(cents: number | null | undefined, isLoading: boolean, error: string | null): Amount {
  if (isLoading) return { kind: 'loading' }
  // After a failed load, any amounts still supplied may be out of date.
  if (error || cents === null || cents === undefined) return { kind: 'unavailable' }
  try {
    return { kind: 'known', cents, text: formatUsdCents(cents, { showPlus: true }) }
  } catch {
    console.warn('ProfitLossBreakdown: could not format value', cents)
    return { kind: 'unavailable' }
  }
}

function direction(cents: number) {
  if (cents > 0) return { name: 'gain', label: 'Gain' }
  if (cents < 0) return { name: 'loss', label: 'Loss' }
  return { name: 'flat', label: 'No change' }
}

function AmountValue({ amount }: { amount: Amount }) {
  if (amount.kind === 'loading') {
    return <dd className="pnl-value pnl-value-pending">Loading…</dd>
  }
  if (amount.kind === 'unavailable') {
    return <dd className="pnl-value pnl-value-pending">Unavailable</dd>
  }
  const { name, label } = direction(amount.cents)
  return (
    <dd className="pnl-value" data-direction={name}>
      <span className="pnl-amount">{amount.text}</span> <span className="pnl-direction">{label}</span>
    </dd>
  )
}

export default function ProfitLossBreakdown({
  realizedPnl,
  unrealizedPnl,
  combinedPnl,
  isLoading = false,
  error = null,
}: ProfitLossBreakdownProps) {
  const titleId = useId()
  const rows = [
    {
      label: 'Realized P&L',
      cents: realizedPnl,
      helper: 'From shares already sold: sale proceeds minus their average cost.',
    },
    {
      label: 'Unrealized P&L',
      cents: unrealizedPnl,
      helper: 'On shares still held, valued at fixed simulated reference prices, not live prices.',
    },
    {
      label: 'Combined P&L',
      cents: combinedPnl,
      helper: 'Realized plus unrealized P&L.',
    },
  ]

  return (
    <section className="pnl" aria-labelledby={titleId}>
      <h2 id={titleId} className="pnl-title">
        Profit and loss
      </h2>

      <p role="status" className="pnl-status">
        {isLoading ? 'Loading profit and loss…' : ''}
      </p>
      {!isLoading && error && <p role="alert">Could not load profit and loss: {error}</p>}

      <dl className="pnl-rows" aria-busy={isLoading}>
        {rows.map(({ label, cents, helper }) => (
          <div key={label} className="pnl-row">
            <dt className="pnl-label">{label}</dt>
            <AmountValue amount={resolveAmount(cents, isLoading, error)} />
            <dd className="pnl-helper">{helper}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
