import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import ConnectionStatus, { type ConnectionState } from './ConnectionStatus'

const expectedLabels: Record<ConnectionState, string> = {
  connecting: 'Connecting…',
  connected: 'Connected',
  reconnecting: 'Reconnecting…',
  disconnected: 'Disconnected',
  error: 'Connection error',
}
const connectionStatuses = Object.keys(expectedLabels) as ConnectionState[]

describe('ConnectionStatus', () => {
  it.each(connectionStatuses)('announces a text label for %s in a status region', (status) => {
    render(<ConnectionStatus status={status} />)

    const region = screen.getByRole('status')
    expect(region).toHaveTextContent(`Live updates: ${expectedLabels[status]}`)
  })

  it.each(connectionStatuses)('gives %s a distinct, scoped description without overclaiming', (status) => {
    render(<ConnectionStatus status={status} />)

    const text = screen.getByRole('status').textContent ?? ''
    expect(text).not.toMatch(/market is live|online|real-time market/i)
    for (const other of connectionStatuses) {
      if (other !== status) {
        expect(text).not.toContain(`Live updates: ${expectedLabels[other]}`)
      }
    }
  })

  it('describes connected data as simulated', () => {
    render(<ConnectionStatus status="connected" />)

    expect(screen.getByRole('status')).toHaveTextContent('Receiving simulated market updates.')
  })

  it('separates a stream error from the outcome of an order', () => {
    render(<ConnectionStatus status="error" />)

    expect(screen.getByRole('status')).toHaveTextContent('This does not mean an order failed.')
  })

  it.each(['reconnecting', 'disconnected', 'error'] as const)('warns that data may be stale when %s', (status) => {
    render(<ConnectionStatus status={status} />)

    expect(screen.getByRole('status')).toHaveTextContent(/out of date|does not mean/)
  })

  it('keeps the visual indicator hidden from assistive technology', () => {
    const { container } = render(<ConnectionStatus status="connected" />)

    expect(container.querySelector('.connection-status-indicator')).toHaveAttribute('aria-hidden', 'true')
  })

  it('updates the same status region when the state changes', () => {
    const { rerender } = render(<ConnectionStatus status="connected" />)
    const region = screen.getByRole('status')

    rerender(<ConnectionStatus status="reconnecting" />)

    expect(screen.getByRole('status')).toBe(region)
    expect(region).toHaveTextContent('Live updates: Reconnecting…')
  })

  it('shows no timestamp when lastUpdatedAt is not supplied', () => {
    const { container } = render(<ConnectionStatus status="connected" />)

    expect(screen.queryByText(/Last update received/)).not.toBeInTheDocument()
    expect(container.querySelector('time')).toBeNull()
  })

  it('shows the supplied lastUpdatedAt outside the live region', () => {
    const lastUpdatedAt = new Date('2026-10-27T14:30:05Z')
    const { container } = render(<ConnectionStatus status="disconnected" lastUpdatedAt={lastUpdatedAt} />)

    const time = container.querySelector('time')
    expect(time).toHaveAttribute('dateTime', '2026-10-27T14:30:05.000Z')
    expect(screen.getByText(/Last update received/)).toContainElement(time)
    expect(screen.getByRole('status')).not.toContainElement(time)
  })
})
