import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  ALL_VIEWS,
  createRealtimeClient,
  createSnapshotGate,
  streamUrl,
  viewsForChange,
  type ConnectionState,
  type RefreshAllReason,
  type Selection,
  type SnapshotTicket,
  type SnapshotView,
  type WebSocketLike,
} from '../lib/realtime'

export type RefreshReason = RefreshAllReason | 'changed' | 'selection-changed'

/** Snapshots to re-fetch now, for the selection current at this moment. */
export interface RefreshRequest {
  views: SnapshotView[]
  reason: RefreshReason
  selection: Selection
}

export interface UseRealtimeOptions {
  selection: Selection
  /** Re-fetch the listed REST snapshots. Events never replace them. */
  onRefresh: (request: RefreshRequest) => void
  /** Defaults to the stream on the page's own host. */
  url?: string
  createSocket?: (url: string) => WebSocketLike
  /** Window for coalescing bursts of changes into one refresh. */
  coalesceMs?: number
}

export interface UseRealtimeResult {
  state: ConnectionState
  /** Manual retry after `disconnected` or `error`. */
  retry: () => void
  /** Call before requesting a snapshot. */
  beginSnapshot: (view: SnapshotView) => SnapshotTicket
  /** Call when the response arrives; show it only if this returns true. */
  acceptSnapshot: (ticket: SnapshotTicket) => boolean
}

function ordered(views: Iterable<SnapshotView>): SnapshotView[] {
  const set = new Set(views)
  return ALL_VIEWS.filter((view) => set.has(view))
}

/**
 * Subscribes to the real-time stream and turns its notifications into
 * refresh requests for the current selection (docs/realtime.md). It owns no
 * snapshot data: the caller fetches, and uses beginSnapshot/acceptSnapshot so a
 * response for an earlier selection, or an older response, is never shown.
 */
export function useRealtime({
  selection,
  onRefresh,
  url,
  createSocket,
  coalesceMs = 150,
}: UseRealtimeOptions): UseRealtimeResult {
  const [state, setState] = useState<ConnectionState>('connecting')
  const [gate] = useState(createSnapshotGate)
  const clientRef = useRef<ReturnType<typeof createRealtimeClient> | null>(null)
  const selectionRef = useRef(selection)
  const onRefreshRef = useRef(onRefresh)
  const createSocketRef = useRef(createSocket)
  const coalesceRef = useRef(coalesceMs)
  const pendingViews = useRef(new Set<SnapshotView>())
  const pendingTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const selectionInitialized = useRef(false)

  useLayoutEffect(() => {
    onRefreshRef.current = onRefresh
    createSocketRef.current = createSocket
    coalesceRef.current = coalesceMs
  })

  const cancelPending = useCallback(() => {
    clearTimeout(pendingTimer.current)
    pendingTimer.current = undefined
    pendingViews.current.clear()
  }, [])

  const emit = useCallback((views: SnapshotView[], reason: RefreshReason) => {
    onRefreshRef.current({ views, reason, selection: selectionRef.current })
  }, [])

  const { userId, symbol } = selection
  // A layout effect, so a response for the old selection cannot be accepted
  // between rendering the new selection and advancing the generation.
  useLayoutEffect(() => {
    selectionRef.current = { userId, symbol }
    if (!selectionInitialized.current) {
      selectionInitialized.current = true
      return
    }
    gate.advance()
    cancelPending()
    emit([...ALL_VIEWS], 'selection-changed')
  }, [userId, symbol, gate, cancelPending, emit])

  useEffect(() => {
    let active = true
    const client = createRealtimeClient({
      url: url ?? streamUrl(window.location),
      createSocket: (target) => (createSocketRef.current ?? ((u: string) => new WebSocket(u)))(target),
      onStateChange: (next) => {
        if (active) setState(next)
      },
      onSignal: (signal) => {
        if (!active) return
        if (signal.type === 'refresh-all') {
          // After a restart, nothing fetched from the old server may be shown.
          if (signal.reason === 'server-restarted') gate.advance()
          cancelPending()
          emit([...ALL_VIEWS], signal.reason)
          return
        }
        const views = viewsForChange(signal.change, selectionRef.current)
        if (views.length === 0) return
        views.forEach((view) => pendingViews.current.add(view))
        if (pendingTimer.current === undefined) {
          pendingTimer.current = setTimeout(() => {
            const queued = ordered(pendingViews.current)
            cancelPending()
            if (active) emit(queued, 'changed')
          }, coalesceRef.current)
        }
      },
    })
    clientRef.current = client
    client.connect()
    return () => {
      active = false
      cancelPending()
      client.disconnect()
      clientRef.current = null
    }
  }, [url, gate, cancelPending, emit])

  const retry = useCallback(() => clientRef.current?.connect(), [])
  const beginSnapshot = useCallback((view: SnapshotView) => gate.begin(view), [gate])
  const acceptSnapshot = useCallback((ticket: SnapshotTicket) => gate.accept(ticket), [gate])

  return { state, retry, beginSnapshot, acceptSnapshot }
}
