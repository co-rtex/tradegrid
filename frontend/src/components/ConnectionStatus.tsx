import './ConnectionStatus.css'

export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected' | 'error'

export interface ConnectionStatusProps {
  /** State of the real-time update stream only, not the REST API or any order. */
  status: ConnectionState
  /** When the last update was received. Omitted rather than guessed when unknown. */
  lastUpdatedAt?: Date
}

const statusText: Record<ConnectionState, { label: string; detail: string }> = {
  connecting: {
    label: 'Connecting…',
    detail: 'Connecting to simulated market updates.',
  },
  connected: {
    label: 'Connected',
    detail: 'Receiving simulated market updates.',
  },
  reconnecting: {
    label: 'Reconnecting…',
    detail: 'Connection lost. Retrying; the data shown may be out of date.',
  },
  disconnected: {
    label: 'Disconnected',
    detail: 'Not receiving updates. The data shown may be out of date.',
  },
  error: {
    label: 'Connection error',
    detail: 'Could not reach the update stream. This does not mean an order failed.',
  },
}

const timeFormat = new Intl.DateTimeFormat('en-US', { timeStyle: 'medium' })

export default function ConnectionStatus({ status, lastUpdatedAt }: ConnectionStatusProps) {
  const { label, detail } = statusText[status]

  return (
    <div className="connection-status" data-status={status}>
      {/* Only the state lives in the live region so timestamp ticks are not announced. */}
      <div role="status" className="connection-status-message">
        <span className="connection-status-indicator" aria-hidden="true" />
        <p className="connection-status-label">
          Live updates: <strong>{label}</strong>
        </p>
        <p className="connection-status-detail">{detail}</p>
      </div>
      {lastUpdatedAt && (
        <p className="connection-status-updated">
          Last update received{' '}
          <time dateTime={lastUpdatedAt.toISOString()}>{timeFormat.format(lastUpdatedAt)}</time>
        </p>
      )}
    </div>
  )
}
