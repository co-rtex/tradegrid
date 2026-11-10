import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { MAX_ORDER_QUANTITY, MoneyInputError, parseUsdToCents } from '../lib/money'
import './OrderEntryForm.css'

export interface OrderEntryValues {
  symbol: string
  side: 'BUY' | 'SELL'
  quantity: string
  price: string
}

/** Only client-owned fields from docs/api.md; price is integer cents. */
export interface OrderEntryRequest {
  userId: string
  symbol: 'AAPL' | 'MSFT'
  side: 'BUY' | 'SELL'
  type: 'LIMIT'
  quantity: number
  price: number
}

export type OrderEntryFieldErrors = Partial<Record<keyof OrderEntryValues, string>>

export interface OrderEntryFormProps {
  userId: string
  value: OrderEntryValues
  onChange: (value: OrderEntryValues) => void
  onSubmit: (request: OrderEntryRequest) => void
  /** Set true synchronously in onSubmit, before awaiting work, to block duplicate submissions. */
  pending?: boolean
  success?: string
  fieldErrors?: OrderEntryFieldErrors
  submissionError?: string
}

export default function OrderEntryForm({
  userId, value, onChange, onSubmit, pending = false,
  success, fieldErrors = {}, submissionError,
}: OrderEntryFormProps) {
  const id = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const wasPending = useRef(pending)
  const [localErrors, setLocalErrors] = useState<OrderEntryFieldErrors>({})
  const errors = { ...fieldErrors, ...localErrors }
  const fieldId = (field: string) => `${id}-${field}`

  useEffect(() => {
    const finished = wasPending.current && !pending
    wasPending.current = pending
    const form = formRef.current
    if (!finished || !form) return
    // Chromium drops focus to BODY when the focused control is disabled.
    // Restore the correction point, but respect users who moved elsewhere.
    const active = form.ownerDocument.activeElement
    if (active !== form.ownerDocument.body && active && !form.contains(active)) return
    const invalid = (['symbol', 'side', 'quantity', 'price'] as const).find((field) => fieldErrors[field])
    form.querySelector<HTMLElement>(invalid ? `[name="${invalid}"]` : '[type="submit"]')?.focus()
  }, [pending, fieldErrors])

  function change(field: keyof OrderEntryValues, text: string) {
    setLocalErrors((previous) => {
      const next = { ...previous }
      delete next[field]
      return next
    })
    onChange({ ...value, [field]: text })
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (pending) return

    const next: OrderEntryFieldErrors = {}
    // Keep exact uppercase symbols, matching the shared API contract.
    if (value.symbol !== 'AAPL' && value.symbol !== 'MSFT') {
      next.symbol = 'Enter AAPL or MSFT in uppercase.'
    }
    if (value.side !== 'BUY' && value.side !== 'SELL') next.side = 'Choose BUY or SELL.'
    const quantity = Number(value.quantity)
    if (!/^\d+$/.test(value.quantity) || !Number.isSafeInteger(quantity) ||
        quantity < 1 || quantity > MAX_ORDER_QUANTITY) {
      next.quantity = 'Enter a whole-share quantity from 1 to 1,000,000.'
    }
    let price = 0
    try {
      price = parseUsdToCents(value.price)
    } catch (error) {
      if (!(error instanceof MoneyInputError)) throw error
      next.price = error.message
    }
    setLocalErrors(next)
    const firstInvalid = (['symbol', 'side', 'quantity', 'price'] as const).find((field) => next[field])
    if (firstInvalid) {
      event.currentTarget.querySelector<HTMLElement>(`[name="${firstInvalid}"]`)?.focus()
      return
    }
    onSubmit({ userId, symbol: value.symbol as OrderEntryRequest['symbol'],
      side: value.side, type: 'LIMIT', quantity, price })
  }

  const feedback = (field: keyof OrderEntryValues) => errors[field] ? (
    <p className="order-entry-error" id={fieldId(`${field}-error`)} role="alert">{errors[field]}</p>
  ) : null
  const describedBy = (field: keyof OrderEntryValues) =>
    `${fieldId(`${field}-hint`)}${errors[field] ? ` ${fieldId(`${field}-error`)}` : ''}`

  return (
    <form ref={formRef} className="order-entry" aria-labelledby={fieldId('heading')} aria-busy={pending} onSubmit={submit} noValidate>
      <header className="order-entry-header">
        <div><h2 id={fieldId('heading')}>Place a limit order</h2><p>Demo account: <strong>{userId}</strong></p></div>
        <span className="order-entry-badge">LIMIT</span>
      </header>
      <fieldset disabled={pending} className="order-entry-fields">
        <legend className="order-entry-sr-only">Order details</legend>
        <div className="order-entry-field">
          <label htmlFor={fieldId('symbol')}>Symbol</label>
          <input id={fieldId('symbol')} name="symbol" value={value.symbol} onChange={(event) => change('symbol', event.target.value)}
            autoComplete="off" autoCapitalize="characters" spellCheck={false} required
            aria-invalid={!!errors.symbol} aria-describedby={describedBy('symbol')} />
          <p id={fieldId('symbol-hint')}>AAPL or MSFT, uppercase.</p>{feedback('symbol')}
        </div>
        <fieldset className="order-entry-side" aria-describedby={describedBy('side')} aria-invalid={!!errors.side}>
          <legend>Side</legend>
          <div className="order-entry-side-options">
            {(['BUY', 'SELL'] as const).map((side) => (
              <label key={side} className={`order-entry-${side.toLowerCase()}`}>
                <input type="radio" name="side" value={side} checked={value.side === side} onChange={() => change('side', side)} />{side}
              </label>
            ))}
          </div>
          <p id={fieldId('side-hint')}>Buy or sell whole shares.</p>{feedback('side')}
        </fieldset>
        <div className="order-entry-field">
          <label htmlFor={fieldId('quantity')}>Quantity (shares)</label>
          <input id={fieldId('quantity')} name="quantity" inputMode="numeric" value={value.quantity}
            onChange={(event) => change('quantity', event.target.value)} required
            aria-invalid={!!errors.quantity} aria-describedby={describedBy('quantity')} />
          <p id={fieldId('quantity-hint')}>1 – 1,000,000 whole shares.</p>{feedback('quantity')}
        </div>
        <div className="order-entry-field">
          <label htmlFor={fieldId('price')}>Limit price (USD)</label>
          <input id={fieldId('price')} name="price" inputMode="decimal" value={value.price}
            onChange={(event) => change('price', event.target.value)} required
            aria-invalid={!!errors.price} aria-describedby={describedBy('price')} />
          <p id={fieldId('price-hint')}>$0.01 – $1,000,000.00</p>{feedback('price')}
        </div>
      </fieldset>
      {submissionError && <p className="order-entry-error" role="alert">{submissionError}</p>}
      <p className="order-entry-status" role="status">{pending ? 'Submitting order…' : success}</p>
      <button className="order-entry-submit" type="submit" disabled={pending}>{pending ? 'Submitting…' : 'Submit limit order'}</button>
    </form>
  )
}
