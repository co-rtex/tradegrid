import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import App from './App'

describe('App', () => {
  it('starts on Dashboard and follows navigation clicks', async () => {
    const user = userEvent.setup()
    render(<App />)
    const nav = screen.getByRole('navigation', { name: 'Primary navigation' })

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Dashboard')

    await user.click(within(nav).getByRole('button', { name: 'Orders' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Orders')
    expect(within(nav).getByRole('button', { name: 'Orders' })).toHaveAttribute('aria-current', 'page')
    expect(within(nav).getByRole('button', { name: 'Dashboard' })).not.toHaveAttribute('aria-current')

    await user.click(screen.getByRole('button', { name: 'TradeGrid dashboard' }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Dashboard')
  })
})
