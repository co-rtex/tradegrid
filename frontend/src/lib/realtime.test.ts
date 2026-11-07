import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  advanceSequence,
  createRealtimeClient,
  createSnapshotGate,
  parseStreamMessage,
  reconnectDelayMs,
  streamUrl,
  viewsForChange,
  type ConnectionState,
  type RealtimeSignal,
  type StreamMessage,
} from './realtime'
import { changed, heartbeat, hello, mockSocketFactory } from '../test/mockWebSocket'

// The sample messages in docs/realtime.md, verbatim.
const contractSamples = [
  '{"type":"hello","version":1,"asOfSequence":2,"sentAt":"2026-10-20T15:00:05Z","data":{"heartbeatSeconds":15}}',
  '{"type":"exchange.changed","version":1,"asOfSequence":1,"sentAt":"2026-10-20T15:00:01Z","data":{"symbol":"AAPL","orderId":"ord-1","userId":"alice","executionCount":0,"affectedUserIds":["alice"]}}',
  '{"type":"exchange.changed","version":1,"asOfSequence":2,"sentAt":"2026-10-20T15:00:02Z","data":{"symbol":"AAPL","orderId":"ord-2","userId":"bob","executionCount":1,"affectedUserIds":["alice","bob"]}}',
  '{"type":"heartbeat","version":1,"asOfSequence":2,"sentAt":"2026-10-20T15:00:20Z","data":{}}',
]

function msg(frame: unknown): StreamMessage {
  const parsed = parseStreamMessage(JSON.stringify(frame))
  if (parsed.kind !== 'message') throw new Error(`not a message: ${JSON.stringify(parsed)}`)
  return parsed.message
}

describe('parseStreamMessage', () => {
  it('accepts every sample message in the contract', () => {
    expect(contractSamples.map((frame) => parseStreamMessage(frame).kind)).toEqual([
      'message',
      'message',
      'message',
      'message',
    ])
    expect(parseStreamMessage(contractSamples[2])).toEqual({
      kind: 'message',
      message: {
        type: 'exchange.changed',
        asOfSequence: 2,
        sentAt: '2026-10-20T15:00:02Z',
        change: { symbol: 'AAPL', orderId: 'ord-2', userId: 'bob', executionCount: 1, affectedUserIds: ['alice', 'bob'] },
      },
    })
    expect(parseStreamMessage(contractSamples[0])).toMatchObject({ message: { type: 'hello', heartbeatSeconds: 15 } })
  })

  it('ignores unknown message types and unknown fields', () => {
    expect(parseStreamMessage(JSON.stringify({ ...heartbeat(3), type: 'market.halted' }))).toEqual({ kind: 'ignored' })
    expect(parseStreamMessage(JSON.stringify({ ...hello(1), extra: true, data: { heartbeatSeconds: 15, more: 1 } }))).toMatchObject({
      kind: 'message',
    })
  })

  it.each([
    ['a binary frame', new ArrayBuffer(4)],
    ['invalid JSON', '{"type":'],
    ['a JSON array', '[]'],
    ['a JSON string', '"hello"'],
    ['a missing type', JSON.stringify({ ...heartbeat(1), type: undefined })],
    ['another protocol version', JSON.stringify({ ...heartbeat(1), version: 2 })],
    ['a missing version', JSON.stringify({ ...heartbeat(1), version: undefined })],
    ['a negative sequence', JSON.stringify(heartbeat(-1))],
    ['a fractional sequence', JSON.stringify(heartbeat(1.5))],
    ['a string sequence', JSON.stringify({ ...heartbeat(1), asOfSequence: '1' })],
    ['a missing sentAt', JSON.stringify({ ...heartbeat(1), sentAt: undefined })],
    ['non-object data', JSON.stringify({ ...heartbeat(1), data: [] })],
    ['hello without heartbeatSeconds', JSON.stringify({ ...hello(0), data: {} })],
    ['hello with zero heartbeatSeconds', JSON.stringify(hello(0, 0))],
    ['a change at sequence 0', JSON.stringify(changed(0))],
    ['a change without a symbol', JSON.stringify(changed(1, { symbol: undefined }))],
    ['a change with a negative executionCount', JSON.stringify(changed(1, { executionCount: -1 }))],
    ['a change with non-string user IDs', JSON.stringify(changed(1, { affectedUserIds: ['alice', 7] }))],
    ['a change with a missing user list', JSON.stringify(changed(1, { affectedUserIds: undefined }))],
  ])('rejects %s as a protocol error', (_name, frame) => {
    expect(parseStreamMessage(frame).kind).toBe('invalid')
  })

  it('copies affectedUserIds so consumers cannot share parser state', () => {
    const frame = JSON.stringify(changed(1, { affectedUserIds: ['alice'] }))
    const first = msg(JSON.parse(frame))
    if (first.type !== 'exchange.changed') throw new Error('wrong type')
    first.change.affectedUserIds.push('bob')
    expect(parseStreamMessage(frame)).toMatchObject({ message: { change: { affectedUserIds: ['alice'] } } })
  })
})

describe('advanceSequence', () => {
  it.each([
    ['first hello', null, hello(4), 4, { action: 'refresh-all', reason: 'connected' }],
    ['hello after reconnect', 4, hello(6), 6, { action: 'refresh-all', reason: 'reconnected' }],
    ['hello at the same sequence', 4, hello(4), 4, { action: 'refresh-all', reason: 'reconnected' }],
    ['hello below lastSeen (server restart)', 9, hello(2), 2, { action: 'refresh-all', reason: 'server-restarted' }],
    ['duplicate change', 5, changed(5), 5, { action: 'none' }],
    ['stale change', 5, changed(3), 5, { action: 'none' }],
    ['gap', 5, changed(8), 8, { action: 'refresh-all', reason: 'gap' }],
    ['heartbeat ahead of lastSeen', 5, heartbeat(7), 7, { action: 'refresh-all', reason: 'gap' }],
    ['heartbeat at lastSeen', 5, heartbeat(5), 5, { action: 'none' }],
    ['heartbeat behind lastSeen', 5, heartbeat(4), 5, { action: 'none' }],
  ])('%s', (_name, lastSeen, frame, wantSeen, outcome) => {
    expect(advanceSequence(lastSeen, msg(frame))).toEqual({ lastSeen: wantSeen, outcome })
  })

  it('applies the next consecutive change', () => {
    const result = advanceSequence(5, msg(changed(6, { symbol: 'MSFT' })))
    expect(result.lastSeen).toBe(6)
    expect(result.outcome).toMatchObject({ action: 'apply', change: { symbol: 'MSFT', orderId: 'ord-6' } })
  })
})

describe('viewsForChange', () => {
  const selection = { userId: 'bob', symbol: 'AAPL' }
  const change = (symbol: string, affectedUserIds: string[]) => ({
    symbol,
    orderId: 'ord-1',
    userId: affectedUserIds[0],
    executionCount: 0,
    affectedUserIds,
  })

  it.each([
    ['the selected symbol only', change('AAPL', ['alice']), ['orderbook', 'executions']],
    ['the selected user only', change('MSFT', ['alice', 'bob']), ['account', 'orders', 'portfolio']],
    ['both', change('AAPL', ['bob']), ['orderbook', 'executions', 'account', 'orders', 'portfolio']],
    ['neither', change('MSFT', ['alice']), []],
  ])('refreshes the views for %s', (_name, input, views) => {
    expect(viewsForChange(input, selection)).toEqual(views)
  })
})

describe('reconnectDelayMs', () => {
  it('doubles from 0.5 s to a 30 s cap without jitter at the midpoint', () => {
    const delays = Array.from({ length: 9 }, (_, i) => reconnectDelayMs(i, () => 0.5))
    expect(delays).toEqual([500, 1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000])
  })

  it('keeps jitter within ±20%', () => {
    expect(reconnectDelayMs(1, () => 0)).toBe(800)
    expect(reconnectDelayMs(1, () => 0.999999)).toBe(1200)
    expect(reconnectDelayMs(20, () => 0.999999)).toBe(36000)
  })
})

describe('streamUrl', () => {
  it('uses the page host and the matching WebSocket scheme', () => {
    expect(streamUrl({ protocol: 'http:', host: 'localhost:5173' })).toBe('ws://localhost:5173/api/v1/stream')
    expect(streamUrl({ protocol: 'https:', host: 'demo.example' })).toBe('wss://demo.example/api/v1/stream')
  })
})

describe('createSnapshotGate', () => {
  it('discards responses from an earlier generation', () => {
    const gate = createSnapshotGate()
    const old = gate.begin('orders')
    gate.advance()
    const current = gate.begin('orders')
    expect(gate.accept(old)).toBe(false)
    expect(gate.accept(current)).toBe(true)
  })

  it('discards a response older than one already applied for the same view', () => {
    const gate = createSnapshotGate()
    const first = gate.begin('orderbook')
    const second = gate.begin('orderbook')
    const other = gate.begin('orders')
    expect(gate.accept(second)).toBe(true)
    expect(gate.accept(first)).toBe(false)
    expect(gate.accept(other)).toBe(true)
    expect(gate.accept(second)).toBe(false)
  })
})

describe('createRealtimeClient', () => {
  let factory: ReturnType<typeof mockSocketFactory>
  let signals: RealtimeSignal[]
  let states: ConnectionState[]
  let client: ReturnType<typeof createRealtimeClient>

  beforeEach(() => {
    vi.useFakeTimers()
    factory = mockSocketFactory()
    signals = []
    states = []
    client = createRealtimeClient({
      url: 'ws://test/api/v1/stream',
      createSocket: factory.create,
      onSignal: (signal) => signals.push(signal),
      onStateChange: (state) => states.push(state),
      random: () => 0.5,
    })
  })

  afterEach(() => {
    client.disconnect()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('connects, waits for hello, then asks for a full refresh', () => {
    client.connect()
    expect(factory.sockets).toHaveLength(1)
    expect(factory.latest().url).toBe('ws://test/api/v1/stream')
    expect(client.getState()).toBe('connecting')

    factory.latest().send(hello(3))

    expect(client.getState()).toBe('connected')
    expect(signals).toEqual([{ type: 'refresh-all', reason: 'connected', asOfSequence: 3 }])
  })

  it('delivers consecutive changes, ignores duplicates, and turns gaps into a full refresh', () => {
    client.connect()
    const socket = factory.latest()
    socket.send(hello(3))
    socket.send(changed(4))
    socket.send(changed(4))
    socket.send(changed(3))
    socket.send(changed(7))
    socket.send(heartbeat(7))
    socket.send(heartbeat(9))

    expect(signals.map((s) => (s.type === 'change' ? `change ${s.asOfSequence}` : `${s.reason} ${s.asOfSequence}`))).toEqual([
      'connected 3',
      'change 4',
      'gap 7',
      'gap 9',
    ])
  })

  it.each([
    ['a change before hello', changed(1)],
    ['invalid JSON', '{not json'],
    ['an unsupported version', { ...hello(0), version: 2 }],
  ])('stops in error without reconnecting on %s', (_name, frame) => {
    client.connect()
    const socket = factory.latest()
    socket.send(frame)

    expect(client.getState()).toBe('error')
    expect(socket.closedByClient).toBe(true)
    expect(socket.listenerCount()).toBe(0)
    vi.advanceTimersByTime(10 * 60_000)
    expect(factory.sockets).toHaveLength(1)
    expect(signals).toEqual([])
  })

  it('treats close code 1003 as a client bug and does not reconnect', () => {
    client.connect()
    factory.latest().send(hello(0))
    factory.latest().serverClose(1003)

    expect(client.getState()).toBe('error')
    vi.advanceTimersByTime(10 * 60_000)
    expect(factory.sockets).toHaveLength(1)
  })

  it.each([1001, 1006, 1011, 1013])('reconnects after close code %i and re-fetches on the new hello', (code) => {
    client.connect()
    factory.latest().send(hello(5))
    factory.latest().serverClose(code)

    expect(client.getState()).toBe('reconnecting')
    vi.advanceTimersByTime(499)
    expect(factory.sockets).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(factory.sockets).toHaveLength(2)

    factory.latest().send(hello(8))
    expect(client.getState()).toBe('connected')
    expect(signals.at(-1)).toEqual({ type: 'refresh-all', reason: 'reconnected', asOfSequence: 8 })
  })

  it('detects a server restart from a lower hello sequence', () => {
    client.connect()
    factory.latest().send(hello(9))
    factory.latest().serverClose(1001)
    vi.advanceTimersByTime(500)
    factory.latest().send(hello(2))

    expect(signals.at(-1)).toEqual({ type: 'refresh-all', reason: 'server-restarted', asOfSequence: 2 })
  })

  it('gives up in disconnected after 10 failed attempts in a row', () => {
    client.connect()
    for (let attempt = 1; attempt <= 10; attempt++) {
      expect(factory.sockets).toHaveLength(attempt)
      factory.latest().serverClose(1006)
      // Run only the scheduled reconnect, not the next socket's hello timeout.
      if (attempt < 10) {
        expect(client.getState()).toBe('reconnecting')
        vi.advanceTimersToNextTimer()
      }
    }

    expect(client.getState()).toBe('disconnected')
    vi.advanceTimersByTime(10 * 60_000)
    expect(factory.sockets).toHaveLength(10)
  })

  it('waits 0.5, 1, 2, 4 ... seconds between failed attempts', () => {
    client.connect()
    const gaps: number[] = []
    for (let i = 0; i < 8; i++) {
      factory.latest().serverClose(1006)
      const before = factory.sockets.length
      let waited = 0
      while (factory.sockets.length === before) {
        vi.advanceTimersByTime(100)
        waited += 100
      }
      gaps.push(waited)
    }
    expect(gaps).toEqual([500, 1000, 2000, 4000, 8000, 16000, 30000, 30000])
  })

  it('resets the backoff after a successful hello', () => {
    client.connect()
    factory.latest().serverClose(1006)
    vi.advanceTimersByTime(500)
    factory.latest().serverClose(1006)
    vi.advanceTimersByTime(1000)
    factory.latest().send(hello(1))
    factory.latest().serverClose(1006)

    vi.advanceTimersByTime(499)
    expect(factory.sockets).toHaveLength(3)
    vi.advanceTimersByTime(1)
    expect(factory.sockets).toHaveLength(4)
  })

  it('counts a connection that never sends hello as a failed attempt', () => {
    client.connect()
    const first = factory.latest()
    vi.advanceTimersByTime(29_999)
    expect(client.getState()).toBe('connecting')
    vi.advanceTimersByTime(1)

    expect(first.closedByClient).toBe(true)
    expect(client.getState()).toBe('reconnecting')
    vi.advanceTimersByTime(500)
    expect(factory.sockets).toHaveLength(2)
  })

  it('reconnects when a connected stream is idle for twice the heartbeat interval', () => {
    client.connect()
    const socket = factory.latest()
    socket.send(hello(1, 10))
    vi.advanceTimersByTime(19_000)
    socket.send(heartbeat(1))
    vi.advanceTimersByTime(19_999)
    expect(client.getState()).toBe('connected')
    vi.advanceTimersByTime(1)

    expect(socket.closedByClient).toBe(true)
    expect(client.getState()).toBe('reconnecting')
  })

  it('ignores events from a socket it has already replaced', () => {
    client.connect()
    const old = factory.latest()
    old.send(hello(1))
    old.serverClose(1006)
    vi.advanceTimersByTime(500)
    const current = factory.latest()
    current.send(hello(1))
    const count = signals.length

    old.send(changed(2))
    old.serverClose(1006)

    expect(signals).toHaveLength(count)
    expect(client.getState()).toBe('connected')
    expect(factory.sockets).toHaveLength(2)
  })

  it('stops completely on disconnect, including a pending reconnect', () => {
    client.connect()
    const socket = factory.latest()
    socket.send(hello(1))
    socket.serverClose(1006)
    expect(client.getState()).toBe('reconnecting')

    client.disconnect()

    expect(client.getState()).toBe('disconnected')
    vi.advanceTimersByTime(10 * 60_000)
    expect(factory.sockets).toHaveLength(1)
    expect(socket.listenerCount()).toBe(0)
  })

  it('closes and detaches the open socket on disconnect', () => {
    client.connect()
    const socket = factory.latest()
    socket.send(hello(1))
    client.disconnect()

    expect(socket.closedByClient).toBe(true)
    expect(socket.listenerCount()).toBe(0)
    socket.send(changed(2))
    expect(signals).toHaveLength(1)
  })

  it('can be retried manually after giving up or after a protocol error', () => {
    client.connect()
    factory.latest().send('not json')
    expect(client.getState()).toBe('error')

    client.connect()
    expect(client.getState()).toBe('connecting')
    factory.latest().send(hello(0))
    expect(client.getState()).toBe('connected')
  })

  it('does not open a second socket when connect is called twice', () => {
    client.connect()
    client.connect()
    expect(factory.sockets).toHaveLength(1)
  })

  it('treats a socket that cannot be created as a failed attempt', () => {
    const failing = createRealtimeClient({
      url: 'ws://test/api/v1/stream',
      createSocket: () => {
        throw new Error('blocked')
      },
      onSignal: () => {},
      random: () => 0.5,
    })
    failing.connect()
    expect(failing.getState()).toBe('reconnecting')
    failing.disconnect()
  })

  it('never makes HTTP requests, so it cannot retry an order', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.reject(new Error('no')))
    client.connect()
    factory.latest().send(hello(1))
    factory.latest().serverClose(1006)
    vi.advanceTimersByTime(60_000)
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('reports each state change once', () => {
    client.connect()
    factory.latest().send(hello(1))
    factory.latest().serverClose(1006)
    vi.advanceTimersByTime(500)
    factory.latest().send(hello(1))
    client.disconnect()

    expect(states).toEqual(['connecting', 'connected', 'reconnecting', 'connected', 'disconnected'])
  })
})
