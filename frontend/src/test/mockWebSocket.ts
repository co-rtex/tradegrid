import type { WebSocketLike } from '../lib/realtime'

type Listener = (event: Event) => void

/** A WebSocket stand-in the test drives as the server. */
export class MockWebSocket implements WebSocketLike {
  readonly url: string
  closedByClient = false
  private readonly listeners = new Map<string, Set<Listener>>()

  constructor(url: string) {
    this.url = url
  }

  addEventListener(type: string, listener: Listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(listener)
  }

  removeEventListener(type: string, listener: Listener) {
    this.listeners.get(type)?.delete(listener)
  }

  close() {
    this.closedByClient = true
  }

  listenerCount() {
    return [...this.listeners.values()].reduce((sum, set) => sum + set.size, 0)
  }

  /** Delivers one frame; objects are sent as JSON text. */
  send(frame: unknown) {
    const data = typeof frame === 'string' ? frame : frame instanceof ArrayBuffer ? frame : JSON.stringify(frame)
    this.dispatch('message', Object.assign(new Event('message'), { data }))
  }

  /** The server or network closes the connection. */
  serverClose(code = 1006) {
    this.dispatch('close', Object.assign(new Event('close'), { code }))
  }

  private dispatch(type: string, event: Event) {
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener(event)
  }
}

/** A socket factory that records every socket it creates. */
export function mockSocketFactory() {
  const sockets: MockWebSocket[] = []
  return {
    sockets,
    create: (url: string) => {
      const socket = new MockWebSocket(url)
      sockets.push(socket)
      return socket
    },
    latest: () => sockets[sockets.length - 1],
  }
}

const sentAt = '2026-10-20T15:00:05Z'

export function hello(asOfSequence: number, heartbeatSeconds = 15) {
  return { type: 'hello', version: 1, asOfSequence, sentAt, data: { heartbeatSeconds } }
}

export function changed(asOfSequence: number, data: Partial<Record<string, unknown>> = {}) {
  return {
    type: 'exchange.changed',
    version: 1,
    asOfSequence,
    sentAt,
    data: {
      symbol: 'AAPL',
      orderId: `ord-${asOfSequence}`,
      userId: 'alice',
      executionCount: 0,
      affectedUserIds: ['alice'],
      ...data,
    },
  }
}

export function heartbeat(asOfSequence: number) {
  return { type: 'heartbeat', version: 1, asOfSequence, sentAt, data: {} }
}
