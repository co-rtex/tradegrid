import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useRealtime, type RefreshRequest, type UseRealtimeOptions } from './useRealtime'
import { changed, hello, mockSocketFactory } from '../test/mockWebSocket'

const ALL = ['orderbook', 'executions', 'account', 'orders', 'portfolio']
// The first retry waits 0.5 s with up to 20% jitter.
const FIRST_RETRY_MAX_MS = 600

describe('useRealtime', () => {
  let factory: ReturnType<typeof mockSocketFactory>
  let refreshes: RefreshRequest[]

  beforeEach(() => {
    vi.useFakeTimers()
    factory = mockSocketFactory()
    refreshes = []
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  function setup(selection = { userId: 'alice', symbol: 'AAPL' }) {
    return renderHook((props: Partial<UseRealtimeOptions>) =>
      useRealtime({
        selection,
        onRefresh: (request) => refreshes.push(request),
        url: 'ws://test/api/v1/stream',
        createSocket: (url) => factory.create(url),
        ...props,
      }),
    )
  }

  function server(fn: () => void) {
    act(fn)
  }

  it('exposes the connection state and re-fetches everything on hello', () => {
    const { result } = setup()
    expect(result.current.state).toBe('connecting')

    server(() => factory.latest().send(hello(4)))

    expect(result.current.state).toBe('connected')
    expect(refreshes).toEqual([
      { views: ALL, reason: 'connected', selection: { userId: 'alice', symbol: 'AAPL' } },
    ])
  })

  it('coalesces a burst of changes into one refresh of the affected views', () => {
    setup()
    server(() => factory.latest().send(hello(0)))
    refreshes.length = 0

    server(() => {
      factory.latest().send(changed(1, { symbol: 'AAPL', affectedUserIds: ['bob'] }))
      factory.latest().send(changed(2, { symbol: 'MSFT', affectedUserIds: ['alice', 'bob'] }))
      factory.latest().send(changed(3, { symbol: 'MSFT', affectedUserIds: ['bob'] }))
    })
    expect(refreshes).toEqual([])

    act(() => vi.advanceTimersByTime(150))

    expect(refreshes).toEqual([
      { views: ALL, reason: 'changed', selection: { userId: 'alice', symbol: 'AAPL' } },
    ])
  })

  it('ignores changes that do not touch the selected user or symbol', () => {
    setup()
    server(() => factory.latest().send(hello(0)))
    refreshes.length = 0

    server(() => factory.latest().send(changed(1, { symbol: 'MSFT', affectedUserIds: ['bob'] })))
    act(() => vi.advanceTimersByTime(1000))

    expect(refreshes).toEqual([])
  })

  it('refreshes immediately for a new selection and drops work queued for the old one', () => {
    const { rerender } = renderHook((selection: { userId: string; symbol: string }) =>
      useRealtime({
        selection,
        onRefresh: (request) => refreshes.push(request),
        url: 'ws://test/api/v1/stream',
        createSocket: (url) => factory.create(url),
      }),
      { initialProps: { userId: 'alice', symbol: 'AAPL' } },
    )
    server(() => factory.latest().send(hello(0)))
    server(() => factory.latest().send(changed(1, { affectedUserIds: ['alice'] })))
    refreshes.length = 0

    rerender({ userId: 'bob', symbol: 'AAPL' })
    act(() => vi.advanceTimersByTime(1000))

    expect(refreshes).toEqual([
      { views: ALL, reason: 'selection-changed', selection: { userId: 'bob', symbol: 'AAPL' } },
    ])
  })

  it('never accepts a response requested for an earlier selection', () => {
    const { result, rerender } = renderHook((selection: { userId: string; symbol: string }) =>
      useRealtime({
        selection,
        onRefresh: () => {},
        url: 'ws://test/api/v1/stream',
        createSocket: (url) => factory.create(url),
      }),
      { initialProps: { userId: 'alice', symbol: 'AAPL' } },
    )
    const aliceOrders = result.current.beginSnapshot('orders')
    const aliceBook = result.current.beginSnapshot('orderbook')

    rerender({ userId: 'bob', symbol: 'AAPL' })

    // Alice's responses arrive after the switch, before anything for Bob.
    expect(result.current.acceptSnapshot(aliceOrders)).toBe(false)
    expect(result.current.acceptSnapshot(aliceBook)).toBe(false)
    const bobOrders = result.current.beginSnapshot('orders')
    expect(result.current.acceptSnapshot(bobOrders)).toBe(true)
  })

  it('uses the selection current when hello arrives after a reconnect', () => {
    const { rerender } = renderHook((selection: { userId: string; symbol: string }) =>
      useRealtime({
        selection,
        onRefresh: (request) => refreshes.push(request),
        url: 'ws://test/api/v1/stream',
        createSocket: (url) => factory.create(url),
      }),
      { initialProps: { userId: 'alice', symbol: 'AAPL' } },
    )
    server(() => factory.latest().send(hello(2)))
    server(() => factory.latest().serverClose(1006))
    rerender({ userId: 'bob', symbol: 'MSFT' })
    act(() => vi.advanceTimersByTime(FIRST_RETRY_MAX_MS))
    refreshes.length = 0

    server(() => factory.latest().send(hello(3)))

    expect(refreshes).toEqual([
      { views: ALL, reason: 'reconnected', selection: { userId: 'bob', symbol: 'MSFT' } },
    ])
  })

  it('discards snapshots from before a server restart', () => {
    const { result } = setup()
    server(() => factory.latest().send(hello(9)))
    const before = result.current.beginSnapshot('account')
    server(() => factory.latest().serverClose(1001))
    act(() => vi.advanceTimersByTime(FIRST_RETRY_MAX_MS))
    server(() => factory.latest().send(hello(1)))

    expect(refreshes.at(-1)?.reason).toBe('server-restarted')
    expect(result.current.acceptSnapshot(before)).toBe(false)
  })

  it('re-fetches everything after a missed sequence', () => {
    setup()
    server(() => factory.latest().send(hello(1)))
    server(() => factory.latest().send(changed(5, { symbol: 'MSFT', affectedUserIds: ['bob'] })))

    expect(refreshes.at(-1)).toMatchObject({ views: ALL, reason: 'gap' })
  })

  it('closes the socket, cancels reconnects, and stops refreshing on unmount', () => {
    const { unmount } = setup()
    server(() => factory.latest().send(hello(1)))
    server(() => factory.latest().send(changed(2)))
    server(() => factory.latest().serverClose(1006))
    const socket = factory.latest()
    const count = refreshes.length

    unmount()
    act(() => vi.advanceTimersByTime(10 * 60_000))

    expect(factory.sockets).toHaveLength(1)
    expect(socket.listenerCount()).toBe(0)
    expect(refreshes).toHaveLength(count)
  })

  it('does not reconnect when the parent re-renders with a new socket factory function', () => {
    const { rerender } = setup()
    server(() => factory.latest().send(hello(1)))

    rerender({ createSocket: (url) => factory.create(url) })
    rerender({ onRefresh: () => {} })

    expect(factory.sockets).toHaveLength(1)
    expect(factory.latest().closedByClient).toBe(false)
  })

  it('reports a protocol error without retrying, and retries on request', () => {
    const { result } = setup()
    server(() => factory.latest().send('{bad json'))
    expect(result.current.state).toBe('error')
    act(() => vi.advanceTimersByTime(10 * 60_000))
    expect(factory.sockets).toHaveLength(1)

    act(() => result.current.retry())
    expect(result.current.state).toBe('connecting')
    expect(factory.sockets).toHaveLength(2)
  })
})
