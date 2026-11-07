/**
 * Client for the real-time stream contract in docs/realtime.md (#71).
 *
 * Stream messages are invalidation notices, not data: they say which REST
 * snapshots to re-fetch. Nothing here stores balances, books, or orders, and
 * nothing here sends data to the server or retries an order POST. A dropped
 * or failed stream only delays a refresh; it never means an order failed.
 */

export const STREAM_PATH = '/api/v1/stream'
export const PROTOCOL_VERSION = 1
/** Used until `hello` supplies the server's value. */
export const DEFAULT_HEARTBEAT_SECONDS = 15
/** Consecutive failed attempts before the client stops in `disconnected`. */
export const MAX_FAILED_ATTEMPTS = 10

const BASE_RECONNECT_DELAY_MS = 500
const MAX_RECONNECT_DELAY_MS = 30_000
const RECONNECT_JITTER = 0.2
/** Close code the server uses when the client sent a data message. */
const CLOSE_UNSUPPORTED_DATA = 1003

/** The stream's connection state. It says nothing about the REST API or any order. */
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'disconnected' | 'error'

/** `data` of an `exchange.changed` message. */
export interface ExchangeChange {
  symbol: string
  orderId: string
  userId: string
  executionCount: number
  affectedUserIds: string[]
}

export type StreamMessage =
  | { type: 'hello'; asOfSequence: number; sentAt: string; heartbeatSeconds: number }
  | { type: 'exchange.changed'; asOfSequence: number; sentAt: string; change: ExchangeChange }
  | { type: 'heartbeat'; asOfSequence: number; sentAt: string }

export type ParseResult =
  | { kind: 'message'; message: StreamMessage }
  /** A valid envelope with a type this client does not know. */
  | { kind: 'ignored' }
  | { kind: 'invalid'; reason: string }

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isCount(value: unknown, minimum: number): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= minimum
}

function invalid(reason: string): ParseResult {
  return { kind: 'invalid', reason }
}

/**
 * Validates one text frame against the message envelope. Unknown types and
 * unknown fields are ignored, as the contract requires; anything else that
 * does not match is a protocol error.
 */
export function parseStreamMessage(frame: unknown): ParseResult {
  if (typeof frame !== 'string') return invalid('message is not a text frame')
  let value: unknown
  try {
    value = JSON.parse(frame)
  } catch {
    return invalid('message is not valid JSON')
  }
  if (!isRecord(value)) return invalid('message is not a JSON object')

  const { type, version, asOfSequence, sentAt, data } = value
  if (typeof type !== 'string') return invalid('type is missing')
  if (version !== PROTOCOL_VERSION) return invalid(`unsupported protocol version ${String(version)}`)
  if (!isCount(asOfSequence, 0)) return invalid('asOfSequence is not a non-negative integer')
  if (typeof sentAt !== 'string') return invalid('sentAt is missing')
  if (!isRecord(data)) return invalid('data is not an object')

  switch (type) {
    case 'hello':
      if (!isCount(data.heartbeatSeconds, 1)) return invalid('hello has no valid heartbeatSeconds')
      return { kind: 'message', message: { type, asOfSequence, sentAt, heartbeatSeconds: data.heartbeatSeconds } }
    case 'exchange.changed': {
      const { symbol, orderId, userId, executionCount, affectedUserIds } = data
      if (
        asOfSequence < 1 ||
        typeof symbol !== 'string' ||
        typeof orderId !== 'string' ||
        typeof userId !== 'string' ||
        !isCount(executionCount, 0) ||
        !Array.isArray(affectedUserIds) ||
        !affectedUserIds.every((id) => typeof id === 'string')
      ) {
        return invalid('exchange.changed has invalid data')
      }
      return {
        kind: 'message',
        message: {
          type,
          asOfSequence,
          sentAt,
          change: { symbol, orderId, userId, executionCount, affectedUserIds: [...affectedUserIds] },
        },
      }
    }
    case 'heartbeat':
      return { kind: 'message', message: { type, asOfSequence, sentAt } }
    default:
      return { kind: 'ignored' }
  }
}

/** Why every displayed snapshot must be re-fetched. */
export type RefreshAllReason = 'connected' | 'reconnected' | 'server-restarted' | 'gap'

export type SequenceOutcome =
  | { action: 'refresh-all'; reason: RefreshAllReason }
  | { action: 'apply'; change: ExchangeChange }
  | { action: 'none' }

/**
 * Applies the "Ordering, Gaps, and Duplicates" table. `lastSeen` is the
 * highest sequence accounted for, or null before the first `hello`.
 */
export function advanceSequence(
  lastSeen: number | null,
  message: StreamMessage,
): { lastSeen: number | null; outcome: SequenceOutcome } {
  const seq = message.asOfSequence
  switch (message.type) {
    case 'hello': {
      const reason: RefreshAllReason =
        lastSeen === null ? 'connected' : seq < lastSeen ? 'server-restarted' : 'reconnected'
      return { lastSeen: seq, outcome: { action: 'refresh-all', reason } }
    }
    case 'exchange.changed':
      if (lastSeen !== null && seq <= lastSeen) return { lastSeen, outcome: { action: 'none' } }
      if (lastSeen !== null && seq === lastSeen + 1) {
        return { lastSeen: seq, outcome: { action: 'apply', change: message.change } }
      }
      return { lastSeen: seq, outcome: { action: 'refresh-all', reason: 'gap' } }
    case 'heartbeat':
      if (lastSeen === null || seq > lastSeen) {
        return { lastSeen: seq, outcome: { action: 'refresh-all', reason: 'gap' } }
      }
      return { lastSeen, outcome: { action: 'none' } }
  }
}

/** A REST snapshot a client may display. */
export type SnapshotView = 'orderbook' | 'executions' | 'account' | 'orders' | 'portfolio'

export const ALL_VIEWS: readonly SnapshotView[] = ['orderbook', 'executions', 'account', 'orders', 'portfolio']

/** The demo account and symbol a view shows. `userId` is a selector, not authentication. */
export interface Selection {
  userId: string
  symbol: string
}

/** The snapshots one change affects for the current selection ("Client-side Filtering"). */
export function viewsForChange(change: ExchangeChange, selection: Selection): SnapshotView[] {
  const views: SnapshotView[] = []
  if (change.symbol === selection.symbol) views.push('orderbook', 'executions')
  if (change.affectedUserIds.includes(selection.userId)) views.push('account', 'orders', 'portfolio')
  return views
}

/**
 * Delay before reconnect attempt `retryIndex` (0-based): 0.5 s doubling to a
 * 30 s cap, with ±20% jitter. `random` returns a number in [0, 1).
 */
export function reconnectDelayMs(retryIndex: number, random: () => number = Math.random): number {
  const base = Math.min(BASE_RECONNECT_DELAY_MS * 2 ** retryIndex, MAX_RECONNECT_DELAY_MS)
  return Math.round(base * (1 + (random() * 2 - 1) * RECONNECT_JITTER))
}

/** The browser stream URL on the page's own host. */
export function streamUrl(location: Pick<Location, 'protocol' | 'host'>): string {
  return `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${STREAM_PATH}`
}

export interface SnapshotTicket {
  view: SnapshotView
  generation: number
  request: number
}

/**
 * Tracks REST snapshot requests so stale responses are discarded: one from an
 * earlier selection generation, or one older than a response already applied
 * for the same view.
 */
export function createSnapshotGate() {
  let generation = 0
  let lastRequest = 0
  const applied = new Map<SnapshotView, number>()
  return {
    /** Starts a new generation, as on a selection change. */
    advance() {
      generation += 1
      applied.clear()
    },
    /** Records a request about to be sent. */
    begin(view: SnapshotView): SnapshotTicket {
      lastRequest += 1
      return { view, generation, request: lastRequest }
    },
    /** Whether the ticket's response may be shown. Accepting it marks it applied. */
    accept(ticket: SnapshotTicket): boolean {
      if (ticket.generation !== generation) return false
      if (ticket.request <= (applied.get(ticket.view) ?? 0)) return false
      applied.set(ticket.view, ticket.request)
      return true
    },
  }
}

export type SnapshotGate = ReturnType<typeof createSnapshotGate>

/** The parts of the browser WebSocket the client uses. It never sends. */
export interface WebSocketLike {
  addEventListener(type: 'message' | 'close', listener: (event: Event) => void): void
  removeEventListener(type: 'message' | 'close', listener: (event: Event) => void): void
  close(): void
}

/** What the client asks its consumer to do. */
export type RealtimeSignal =
  | { type: 'refresh-all'; reason: RefreshAllReason; asOfSequence: number }
  | { type: 'change'; change: ExchangeChange; asOfSequence: number }

export interface RealtimeClientOptions {
  url: string
  onSignal: (signal: RealtimeSignal) => void
  onStateChange?: (state: ConnectionState) => void
  /** Every valid message, after sequence handling. */
  onMessage?: (message: StreamMessage) => void
  createSocket?: (url: string) => WebSocketLike
  random?: () => number
}

export interface RealtimeClient {
  /** Starts connecting; also the manual retry after `disconnected` or `error`. */
  connect(): void
  /** Closes the stream and cancels any pending reconnect. */
  disconnect(): void
  getState(): ConnectionState
}

/**
 * Creates a stream client that follows the contract's connection lifecycle:
 * it waits for `hello`, detects gaps and duplicates, reconnects with bounded
 * backoff, closes idle connections, and stops on protocol errors.
 *
 * A connection that sends no `hello` within twice the default heartbeat
 * interval counts as a failed attempt.
 */
export function createRealtimeClient(options: RealtimeClientOptions): RealtimeClient {
  const createSocket = options.createSocket ?? ((url: string) => new WebSocket(url))
  const random = options.random ?? Math.random

  let state: ConnectionState = 'disconnected'
  let socket: WebSocketLike | null = null
  let detach: (() => void) | null = null
  let lastSeen: number | null = null
  let helloReceived = false
  let heartbeatMs = DEFAULT_HEARTBEAT_SECONDS * 1000
  let failedAttempts = 0
  let retryIndex = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined
  let helloTimer: ReturnType<typeof setTimeout> | undefined
  let idleTimer: ReturnType<typeof setTimeout> | undefined

  function setState(next: ConnectionState) {
    if (state === next) return
    state = next
    options.onStateChange?.(next)
  }

  function clearTimers() {
    clearTimeout(reconnectTimer)
    clearTimeout(helloTimer)
    clearTimeout(idleTimer)
    reconnectTimer = helloTimer = idleTimer = undefined
  }

  function closeSocket() {
    detach?.()
    detach = null
    const current = socket
    socket = null
    try {
      current?.close()
    } catch {
      // Closing an already closed socket is harmless.
    }
  }

  function openSocket() {
    reconnectTimer = undefined
    helloReceived = false
    let current: WebSocketLike
    try {
      current = createSocket(options.url)
    } catch {
      connectionLost()
      return
    }
    socket = current
    const onMessage = (event: Event) => {
      if (socket === current) handleFrame((event as MessageEvent).data)
    }
    const onClose = (event: Event) => {
      if (socket !== current) return
      if ((event as CloseEvent).code === CLOSE_UNSUPPORTED_DATA) protocolError()
      else connectionLost()
    }
    current.addEventListener('message', onMessage)
    current.addEventListener('close', onClose)
    detach = () => {
      current.removeEventListener('message', onMessage)
      current.removeEventListener('close', onClose)
    }
    helloTimer = setTimeout(connectionLost, 2 * DEFAULT_HEARTBEAT_SECONDS * 1000)
  }

  function resetIdleTimer() {
    clearTimeout(idleTimer)
    idleTimer = setTimeout(connectionLost, 2 * heartbeatMs)
  }

  function handleFrame(frame: unknown) {
    const parsed = parseStreamMessage(frame)
    if (parsed.kind === 'invalid') {
      protocolError()
      return
    }
    if (parsed.kind === 'ignored') {
      if (helloReceived) resetIdleTimer()
      return
    }
    const message = parsed.message
    if (!helloReceived) {
      // The contract sends hello first on every connection.
      if (message.type !== 'hello') {
        protocolError()
        return
      }
      helloReceived = true
      heartbeatMs = message.heartbeatSeconds * 1000
      failedAttempts = 0
      retryIndex = 0
      clearTimeout(helloTimer)
      helloTimer = undefined
      setState('connected')
    }
    resetIdleTimer()

    const next = advanceSequence(lastSeen, message)
    lastSeen = next.lastSeen
    options.onMessage?.(message)
    const outcome = next.outcome
    if (outcome.action === 'refresh-all') {
      options.onSignal({ type: 'refresh-all', reason: outcome.reason, asOfSequence: message.asOfSequence })
    } else if (outcome.action === 'apply') {
      options.onSignal({ type: 'change', change: outcome.change, asOfSequence: message.asOfSequence })
    }
  }

  function connectionLost() {
    const hadHello = helloReceived
    closeSocket()
    clearTimers()
    helloReceived = false
    if (!hadHello) failedAttempts += 1
    if (failedAttempts >= MAX_FAILED_ATTEMPTS) {
      setState('disconnected')
      return
    }
    setState('reconnecting')
    reconnectTimer = setTimeout(openSocket, reconnectDelayMs(retryIndex, random))
    retryIndex += 1
  }

  function protocolError() {
    closeSocket()
    clearTimers()
    helloReceived = false
    setState('error')
  }

  return {
    connect() {
      if (socket || reconnectTimer !== undefined) return
      failedAttempts = 0
      retryIndex = 0
      setState('connecting')
      openSocket()
    },
    disconnect() {
      closeSocket()
      clearTimers()
      helloReceived = false
      setState('disconnected')
    },
    getState: () => state,
  }
}
