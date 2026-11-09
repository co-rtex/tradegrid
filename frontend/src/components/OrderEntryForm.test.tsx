import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import noFill from '../../../docs/fixtures/01-no-fill.json'
import partialFill from '../../../docs/fixtures/03-partial-fill.json'
import OrderEntryForm, { type OrderEntryFormProps, type OrderEntryValues } from './OrderEntryForm'

const initial: OrderEntryValues = { symbol: 'AAPL', side: 'BUY', quantity: '25', price: '190.50' }

function setup(overrides: Partial<OrderEntryFormProps> = {}) {
  const onSubmit = vi.fn()
  function Harness(props: Partial<OrderEntryFormProps>) {
    const [value, onChange] = useState(initial)
    return <OrderEntryForm userId="bob" value={value} onChange={onChange} onSubmit={onSubmit} {...props} />
  }
  const view = render(<Harness {...overrides} />)
  return { ...view, onSubmit, user: userEvent.setup(), update: (props: Partial<OrderEntryFormProps>) => view.rerender(<Harness {...props} />) }
}

describe('OrderEntryForm', () => {
  it('submits the exact BUY fixture payload once', async () => {
    const { user, onSubmit } = setup()
    await user.click(screen.getByRole('button', { name: 'Submit limit order' }))
    expect(onSubmit.mock.calls).toEqual([[partialFill.request.body]])
  })

  it('submits the exact SELL fixture with the parent-selected user', async () => {
    const { user, onSubmit } = setup({ userId: 'alice' })
    await user.click(screen.getByRole('radio', { name: 'SELL' }))
    await user.clear(screen.getByLabelText('Quantity (shares)'))
    await user.type(screen.getByLabelText('Quantity (shares)'), '10')
    await user.clear(screen.getByLabelText('Limit price (USD)'))
    await user.type(screen.getByLabelText('Limit price (USD)'), '190.00')
    await user.click(screen.getByRole('button', { name: 'Submit limit order' }))
    expect(onSubmit.mock.calls).toEqual([[noFill.request.body]])
  })

  it.each([
    ['Symbol', ''], ['Symbol', 'aapl'], ['Symbol', 'GOOG'], ['Symbol', ' AAPL '],
    ['Quantity (shares)', ''], ['Quantity (shares)', '0'], ['Quantity (shares)', '-1'],
    ['Quantity (shares)', '1.5'], ['Quantity (shares)', '1e2'], ['Quantity (shares)', '1000001'],
    ['Quantity (shares)', '9007199254740992'], ['Quantity (shares)', 'NaN'],
    ['Limit price (USD)', ''], ['Limit price (USD)', '0'], ['Limit price (USD)', '-1'],
    ['Limit price (USD)', '1.005'], ['Limit price (USD)', '1e2'],
    ['Limit price (USD)', '1000000.01'], ['Limit price (USD)', 'Infinity'],
  ])('blocks invalid %s: %s with linked feedback and focus', async (label, text) => {
    const { user, onSubmit } = setup()
    const input = screen.getByLabelText(label)
    await user.clear(input)
    if (text) await user.type(input, text)
    await user.click(screen.getByRole('button', { name: 'Submit limit order' }))
    expect(onSubmit).not.toHaveBeenCalled()
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveFocus()
    expect(input).toHaveAccessibleDescription(expect.stringContaining(screen.getByRole('alert').textContent!))
  })

  it.each([['0.01', '1', 1, 1], ['1000000.00', '1000000', 100000000, 1000000], ['10.25', '10', 1025, 10]])(
    'accepts price %s and quantity %s without rounding', async (price, quantity, cents, shares) => {
      const { user, onSubmit } = setup({ value: { symbol: 'MSFT', side: 'BUY', price, quantity } })
      await user.click(screen.getByRole('button', { name: 'Submit limit order' }))
      expect(onSubmit.mock.calls).toEqual([[{ userId: 'bob', symbol: 'MSFT', side: 'BUY', type: 'LIMIT', price: cents, quantity: shares }]])
    },
  )

  it('keeps values controlled by its parent', async () => {
    const onChange = vi.fn()
    const { user } = setup({ value: initial, onChange })
    await user.type(screen.getByLabelText('Symbol'), 'X')
    expect(onChange).toHaveBeenCalledWith({ ...initial, symbol: 'AAPLX' })
    expect(screen.getByLabelText('Symbol')).toHaveValue('AAPL')
  })

  it('blocks duplicate clicks and form submissions while pending, then allows retry', async () => {
    const { user, onSubmit, update } = setup({ pending: true })
    const form = screen.getByRole('form', { name: 'Place a limit order' })
    expect(form).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByLabelText('Symbol')).toBeDisabled()
    expect(screen.getByRole('radio', { name: 'SELL' })).toBeDisabled()
    expect(screen.getByRole('button')).toBeDisabled()
    await user.dblClick(screen.getByRole('button'))
    fireEvent.submit(form)
    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByRole('status')).toHaveTextContent('Submitting order…')
    update({ pending: false })
    await user.click(screen.getByRole('button'))
    expect(onSubmit).toHaveBeenCalledTimes(1)
  })

  it('restores focus to the first server-invalid field when pending ends', () => {
    const { update } = setup({ pending: true })
    // Simulate Chromium's focus loss; jsdom alone does not reproduce it.
    update({ pending: false, fieldErrors: { price: 'Price rejected.', quantity: 'Quantity rejected.' } })
    expect(screen.getByLabelText('Quantity (shares)')).toHaveFocus()
  })

  it.each([{ submissionError: 'Order rejected.' }, { success: 'Order accepted.' }])(
    'restores submit focus when pending ends without a field error: %j', (result) => {
      const { update } = setup({ pending: true })
      update({ pending: false, ...result })
      expect(screen.getByRole('button', { name: 'Submit limit order' })).toHaveFocus()
    },
  )

  it('does not steal focus if the user moved outside the form while pending', async () => {
    const { update, user } = setup({ pending: true })
    const outside = document.createElement('button')
    outside.textContent = 'Other action'
    document.body.append(outside)
    await user.click(outside)
    update({ pending: false, fieldErrors: { price: 'Price rejected.' } })
    expect(outside).toHaveFocus()
    outside.remove()
  })

  it('shows parent field/submission errors, preserves inputs, and allows correction', async () => {
    const { user, onSubmit, update } = setup()
    await user.click(screen.getByRole('button'))
    update({ fieldErrors: { price: 'Price rejected by server.' }, submissionError: 'Order was not accepted.' })
    expect(screen.getAllByRole('alert')).toHaveLength(2)
    expect(screen.getByLabelText('Limit price (USD)')).toHaveAccessibleDescription(expect.stringContaining('Price rejected by server.'))
    expect(screen.getByLabelText('Symbol')).toHaveValue('AAPL')
    expect(screen.getByLabelText('Quantity (shares)')).toHaveValue('25')
    expect(screen.getByLabelText('Limit price (USD)')).toHaveValue('190.50')
    await user.clear(screen.getByLabelText('Limit price (USD)'))
    await user.type(screen.getByLabelText('Limit price (USD)'), '180')
    update({})
    await user.click(screen.getByRole('button'))
    expect(onSubmit).toHaveBeenLastCalledWith({ ...partialFill.request.body, price: 18000 })
  })

  it('announces success supplied by the parent without clearing inputs', () => {
    setup({ success: 'Limit order accepted.' })
    expect(screen.getByRole('status')).toHaveTextContent('Limit order accepted.')
    expect(screen.getByLabelText('Quantity (shares)')).toHaveValue('25')
  })

  it('supports Tab, radio arrow keys, and Enter submission', async () => {
    const { user, onSubmit } = setup()
    await user.tab()
    expect(screen.getByLabelText('Symbol')).toHaveFocus()
    await user.tab()
    expect(screen.getByRole('radio', { name: 'BUY' })).toHaveFocus()
    await user.keyboard('{ArrowRight}')
    expect(screen.getByRole('radio', { name: 'SELL' })).toBeChecked()
    await user.tab()
    expect(screen.getByLabelText('Quantity (shares)')).toHaveFocus()
    await user.tab()
    expect(screen.getByLabelText('Limit price (USD)')).toHaveFocus()
    await user.tab()
    expect(screen.getByRole('button')).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(onSubmit.mock.calls).toEqual([[{ ...partialFill.request.body, side: 'SELL' }]])
  })
})
