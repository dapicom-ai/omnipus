/**
 * ws.ts — WsConnection tests for B1.3(c) reconnect-on-visibilitychange/online
 *
 * Traces to: B1.3(c) security hardening
 * When the connection is in disconnected or reconnecting state, the
 * visibilitychange and online events must trigger an immediate reconnect
 * attempt, resetting the exponential backoff counter so recovery is fast.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { WsConnection } from './ws'

// ── Mock WebSocket ─────────────────────────────────────────────────────────────

let lastWsInstance: {
  onopen: (() => void) | null
  onmessage: ((ev: { data: string }) => void) | null
  onclose: ((ev: { code: number; reason: string }) => void) | null
  onerror: (() => void) | null
  send: ReturnType<typeof vi.fn>
  close: ReturnType<typeof vi.fn>
  readyState: number
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const MockWebSocket = vi.fn(function (this: any) {
  this.onopen = null
  this.onmessage = null
  this.onclose = null
  this.onerror = null
  this.send = vi.fn()
  this.close = vi.fn()
  this.readyState = 1 // OPEN
  lastWsInstance = this
}) as unknown as typeof WebSocket & { OPEN: number; CLOSED: number; mockClear: () => void }

MockWebSocket.OPEN = 1
MockWebSocket.CLOSED = 3

// ── Event listener capture ─────────────────────────────────────────────────────

// Capture addEventListener / removeEventListener calls on window so we can
// trigger the registered handlers in tests.
const windowListeners: Record<string, EventListenerOrEventListenerObject[]> = {}

function setupWindowEventCapture() {
  vi.spyOn(window, 'addEventListener').mockImplementation(
    (type: string, listener: EventListenerOrEventListenerObject) => {
      if (!windowListeners[type]) windowListeners[type] = []
      windowListeners[type].push(listener)
    }
  )
  vi.spyOn(window, 'removeEventListener').mockImplementation(
    (type: string, listener: EventListenerOrEventListenerObject) => {
      if (windowListeners[type]) {
        windowListeners[type] = windowListeners[type].filter((l) => l !== listener)
      }
    }
  )
}

function triggerWindowEvent(type: string) {
  for (const listener of windowListeners[type] ?? []) {
    if (typeof listener === 'function') {
      listener(new Event(type))
    } else {
      listener.handleEvent(new Event(type))
    }
  }
}

beforeEach(() => {
  MockWebSocket.mockClear()
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('localStorage', { getItem: vi.fn(() => null) })
  vi.stubGlobal('sessionStorage', { getItem: vi.fn(() => null) })
  // Clear captured listeners
  for (const key of Object.keys(windowListeners)) {
    delete windowListeners[key]
  }
  setupWindowEventCapture()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

// ── Helper ─────────────────────────────────────────────────────────────────────

function makeCallbacks() {
  return {
    onFrame: vi.fn(),
    onConnected: vi.fn(),
    onDisconnected: vi.fn(),
    onError: vi.fn(),
  }
}

// ── B1.3(c) — visibilitychange reconnect ──────────────────────────────────────

describe('WsConnection — visibilitychange triggers reconnect (B1.3c)', () => {
  // Traces to: B1.3(c) — when tab returns to visible state and WS is disconnected,
  // reconnect must fire immediately with backoff reset.

  it('registers a visibilitychange listener when connect() is called', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    expect(windowListeners['visibilitychange']).toBeDefined()
    expect(windowListeners['visibilitychange'].length).toBeGreaterThan(0)

    conn.disconnect()
  })

  it('reconnects immediately when visibilitychange fires while ws is null', () => {
    vi.useFakeTimers()
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    // Simulate a disconnect (code 1006 — abnormal close)
    lastWsInstance.onopen?.()
    lastWsInstance.onclose?.({ code: 1006, reason: 'abnormal' })

    // ws is now null — reconnect timer is pending
    const wsCallCountAfterDisconnect = MockWebSocket.mock.calls.length

    // Trigger visibilitychange (user returns to the tab)
    // Stub document.visibilityState to 'visible'
    Object.defineProperty(document, 'visibilityState', {
      get: () => 'visible',
      configurable: true,
    })
    triggerWindowEvent('visibilitychange')

    // A new WebSocket should have been created immediately (backoff reset)
    expect(MockWebSocket.mock.calls.length).toBeGreaterThan(wsCallCountAfterDisconnect)

    conn.disconnect()
    vi.useRealTimers()
  })

  it('does NOT reconnect via visibilitychange when disconnect() was intentional', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()
    lastWsInstance.onopen?.()

    // Intentional disconnect
    conn.disconnect()

    const wsCallCountAfterDisconnect = MockWebSocket.mock.calls.length

    Object.defineProperty(document, 'visibilityState', {
      get: () => 'visible',
      configurable: true,
    })
    triggerWindowEvent('visibilitychange')

    // No new WebSocket created — intentional close must not auto-reconnect
    expect(MockWebSocket.mock.calls.length).toBe(wsCallCountAfterDisconnect)
  })

  it('removes visibilitychange listener when disconnect() is called', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    expect(windowListeners['visibilitychange']?.length).toBeGreaterThan(0)

    conn.disconnect()

    expect(windowListeners['visibilitychange']?.length ?? 0).toBe(0)
  })
})

// ── B1.3(c) — online reconnect ────────────────────────────────────────────────

describe('WsConnection — online event triggers reconnect (B1.3c)', () => {
  // Traces to: B1.3(c) — when the network recovers (online event) and WS is
  // disconnected, reconnect must fire immediately with backoff reset.

  it('registers an online listener when connect() is called', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    expect(windowListeners['online']).toBeDefined()
    expect(windowListeners['online'].length).toBeGreaterThan(0)

    conn.disconnect()
  })

  it('reconnects immediately when online event fires while ws is null', () => {
    vi.useFakeTimers()
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    // Simulate disconnect
    lastWsInstance.onopen?.()
    lastWsInstance.onclose?.({ code: 1006, reason: '' })

    const wsCallCountAfterDisconnect = MockWebSocket.mock.calls.length

    // Trigger online — network recovered
    triggerWindowEvent('online')

    expect(MockWebSocket.mock.calls.length).toBeGreaterThan(wsCallCountAfterDisconnect)

    conn.disconnect()
    vi.useRealTimers()
  })

  it('does NOT reconnect via online when disconnect() was intentional', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()
    lastWsInstance.onopen?.()

    conn.disconnect()

    const wsCallCountAfterDisconnect = MockWebSocket.mock.calls.length

    triggerWindowEvent('online')

    expect(MockWebSocket.mock.calls.length).toBe(wsCallCountAfterDisconnect)
  })

  it('removes online listener when disconnect() is called', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()

    expect(windowListeners['online']?.length).toBeGreaterThan(0)

    conn.disconnect()

    expect(windowListeners['online']?.length ?? 0).toBe(0)
  })
})

// ── B1.3(c) — persistent banner for non-1000/1001 close codes ─────────────────

describe('WsConnection — persistent banner for non-1000/1001 close (B1.3c)', () => {
  // Traces to: B1.3(c) — non-1000/1001 close codes must call onError with a
  // message containing the code, which AppShell renders as a persistent banner.

  it('calls onError with code in message for 1006 close', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()
    lastWsInstance.onopen?.()

    lastWsInstance.onclose?.({ code: 1006, reason: '' })

    expect(cbs.onError).toHaveBeenCalledWith(
      expect.stringContaining('1006')
    )

    conn.disconnect()
  })

  it('calls onError with code in message for 1011 close', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()
    lastWsInstance.onopen?.()

    lastWsInstance.onclose?.({ code: 1011, reason: 'server error' })

    expect(cbs.onError).toHaveBeenCalledWith(
      expect.stringContaining('1011')
    )

    conn.disconnect()
  })

  it('does NOT call onError for intentional 1000 close', () => {
    const cbs = makeCallbacks()
    const conn = new WsConnection(cbs)
    conn.connect()
    lastWsInstance.onopen?.()

    // Intentional disconnect triggers close(1000)
    conn.disconnect()

    // onError must not be called for a 1000 close initiated by the client
    expect(cbs.onError).not.toHaveBeenCalled()
  })
})
