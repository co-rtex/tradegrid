import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import TopNavigation, { type NavigationItem, type TopNavigationProps } from './TopNavigation'

const items: NavigationItem[] = ['Dashboard', 'Portfolio', 'Orders', 'Markets']

function renderNavigation(props: Partial<TopNavigationProps> = {}) {
  const onNavigate = vi.fn<(item: NavigationItem) => void>()
  render(<TopNavigation activeItem="Dashboard" onNavigate={onNavigate} {...props} />)
  return { onNavigate, user: userEvent.setup() }
}

function primaryNavigation() {
  return screen.getByRole('navigation', { name: 'Primary navigation' })
}

describe('TopNavigation', () => {
  it('exposes a labelled primary navigation with every item in order', () => {
    renderNavigation()

    const buttons = within(primaryNavigation()).getAllByRole('button')
    expect(buttons.map((button) => button.textContent)).toEqual(items)
    expect(screen.getByRole('button', { name: 'TradeGrid dashboard' })).toBeInTheDocument()
  })

  it.each(items)('marks only %s as the current page when it is active', (active) => {
    renderNavigation({ activeItem: active })

    for (const item of items) {
      const button = within(primaryNavigation()).getByRole('button', { name: item })
      if (item === active) {
        expect(button).toHaveAttribute('aria-current', 'page')
      } else {
        expect(button).not.toHaveAttribute('aria-current')
      }
    }
  })

  it.each(items)('calls onNavigate once with %s when it is clicked', async (item) => {
    const { onNavigate, user } = renderNavigation({ activeItem: item === 'Orders' ? 'Markets' : 'Orders' })

    await user.click(within(primaryNavigation()).getByRole('button', { name: item }))

    expect(onNavigate).toHaveBeenCalledTimes(1)
    expect(onNavigate).toHaveBeenCalledWith(item)
  })

  it('returns to Dashboard from the wordmark', async () => {
    const { onNavigate, user } = renderNavigation({ activeItem: 'Markets' })

    await user.click(screen.getByRole('button', { name: 'TradeGrid dashboard' }))

    expect(onNavigate).toHaveBeenCalledTimes(1)
    expect(onNavigate).toHaveBeenCalledWith('Dashboard')
  })

  it('does not change the active item by itself; the parent controls it', async () => {
    const { user } = renderNavigation({ activeItem: 'Dashboard' })

    await user.click(within(primaryNavigation()).getByRole('button', { name: 'Orders' }))

    const nav = primaryNavigation()
    expect(within(nav).getByRole('button', { name: 'Dashboard' })).toHaveAttribute('aria-current', 'page')
    expect(within(nav).getByRole('button', { name: 'Orders' })).not.toHaveAttribute('aria-current')
  })

  it('can be operated with the keyboard in reading order', async () => {
    const { onNavigate, user } = renderNavigation()

    await user.tab()
    expect(screen.getByRole('button', { name: 'TradeGrid dashboard' })).toHaveFocus()
    await user.tab()
    await user.tab()
    expect(within(primaryNavigation()).getByRole('button', { name: 'Portfolio' })).toHaveFocus()

    await user.keyboard('{Enter}')
    await user.tab()
    await user.keyboard(' ')

    expect(onNavigate.mock.calls).toEqual([['Portfolio'], ['Orders']])
  })

  it('shows an em dash when no balance is supplied', () => {
    renderNavigation()

    expect(screen.getByText('Balance')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows the supplied balance text unchanged', () => {
    renderNavigation({ balance: '$100,000.00' })

    expect(screen.getByText('$100,000.00')).toBeInTheDocument()
    expect(screen.queryByText('—')).not.toBeInTheDocument()
  })
})
