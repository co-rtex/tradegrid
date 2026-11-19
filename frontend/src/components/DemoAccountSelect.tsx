import './DemoAccountSelect.css'
import type { DemoAccount } from '../hooks/useDemoAccounts'

export interface DemoAccountSelectProps {
  accounts: DemoAccount[]
  value: string
  onChange: (userId: string) => void
  disabled?: boolean
}

/**
 * The one demo-account selector. Every view that shows account data renders
 * this control bound to the app-level selection, so the trading desk and the
 * portfolio always show the same account. Choosing an account is not a
 * sign-in.
 */
export default function DemoAccountSelect({ accounts, value, onChange, disabled = false }: DemoAccountSelectProps) {
  return (
    <label className="demo-account-select">
      <span>Demo account</span>
      <select value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
        {accounts.map((account) => (
          <option key={account.userId} value={account.userId}>
            {account.displayName}
          </option>
        ))}
      </select>
    </label>
  )
}
