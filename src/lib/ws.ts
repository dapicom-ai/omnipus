// WebSocket connection manager for /api/v1/chat/ws
// Handles: connect, authenticate, streaming frames, reconnect with exponential backoff

const BASE_URL = import.meta.env.VITE_API_URL ?? ''

function getWsUrl(): string {
  const httpBase = BASE_URL || window.location.origin
  const wsBase = httpBase.replace(/^http/, 'ws')
  return `${wsBase}/api/v1/chat/ws`
}

// ── Frame types ───────────────────────────────────────────────────────────────

export interface WsAuthFrame {
  type: 'auth'
  token: string
}

export interface WsMessageFrame {
  type: 'message'
  content: string
  session_id?: string
  agent_id?: string
}

export interface WsCancelFrame {
  type: 'cancel'
  session_id: string
}

export interface WsExecApprovalResponseFrame {
  type: 'exec_approval_response'
  id: string
  decision: 'allow' | 'deny' | 'always'
}

export interface WsPingFrame {
  type: 'ping'
}

export type WsSendFrame = WsAuthFrame | WsMessageFrame | WsCancelFrame | WsExecApprovalResponseFrame | WsPingFrame

export interface WsTokenFrame {
  type: 'token'
  content: string
}

export interface WsDoneFrame {
  type: 'done'
  stats?: {
    tokens?: number
    cost?: number
    duration_ms?: number
  }
}

export interface WsErrorFrame {
  type: 'error'
  message: string
}

export interface WsToolCallStartFrame {
  type: 'tool_call_start'
  tool: string
  call_id: string
  params: Record<string, unknown>
}

export interface WsToolCallResultFrame {
  type: 'tool_call_result'
  tool: string
  call_id: string
  result: unknown
  status: 'success' | 'error'
  duration_ms?: number
  error?: string
}

export interface WsExecApprovalRequestFrame {
  type: 'exec_approval_request'
  id: string
  command: string
  working_dir?: string
  matched_policy?: string
}

export type WsReceiveFrame =
  | WsTokenFrame
  | WsDoneFrame
  | WsErrorFrame
  | WsToolCallStartFrame
  | WsToolCallResultFrame
  | WsExecApprovalRequestFrame

// ── Connection ────────────────────────────────────────────────────────────────

export interface WsConnectionCallbacks {
  onFrame: (frame: WsReceiveFrame) => void
  onConnected: () => void
  onDisconnected: () => void
  onError: (error: string) => void
}

export class WsConnection {
  private ws: WebSocket | null = null
  private reconnectAttempts = 0
  private maxReconnectAttempts = 3
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null
  private intentionalClose = false
  private callbacks: WsConnectionCallbacks
  /** Consecutive malformed/invalid frame counter — reset on every valid frame */
  private droppedFrameCount = 0
  private readonly droppedFrameThreshold = 5

  constructor(callbacks: WsConnectionCallbacks) {
    this.callbacks = callbacks
  }

  connect(): void {
    this.intentionalClose = false
    this.reconnectAttempts = 0
    this._createSocket()
  }

  disconnect(): void {
    this.intentionalClose = true
    this._clearReconnectTimer()
    this._stopHeartbeat()
    this.ws?.close(1000, 'User disconnected')
    this.ws = null
  }

  send(frame: WsSendFrame): boolean {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(frame))
      return true
    }
    this.callbacks.onError('Not connected — message not sent')
    return false
  }

  get isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  private _createSocket(): void {
    try {
      this.ws = new WebSocket(getWsUrl())
    } catch (err) {
      this.callbacks.onError(`Failed to create WebSocket: ${err instanceof Error ? err.message : String(err)}`)
      return
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
      // Auth token is re-read from localStorage on every (re-)connect
      // so changes after initial load take effect without a page refresh.
      const token = localStorage.getItem('omnipus_auth_token')
      if (token) {
        this.send({ type: 'auth', token })
      } else {
        console.warn('[ws] No auth token found — connecting unauthenticated')
      }
      this._startHeartbeat()
      this.callbacks.onConnected()
    }

    this.ws.onmessage = (event: MessageEvent) => {
      let frame: WsReceiveFrame
      try {
        frame = JSON.parse(event.data as string) as WsReceiveFrame
      } catch {
        // Malformed JSON — log and discard, don't swallow downstream errors
        console.warn('[ws] Malformed frame:', event.data)
        this.droppedFrameCount++
        if (this.droppedFrameCount >= this.droppedFrameThreshold) {
          this.callbacks.onError(
            `Received ${this.droppedFrameCount} malformed frames in a row — the connection may be unstable.`,
          )
        }
        return
      }
      if (typeof frame.type !== 'string') {
        console.warn('[ws] Invalid frame — missing type field:', frame)
        this.droppedFrameCount++
        if (this.droppedFrameCount >= this.droppedFrameThreshold) {
          this.callbacks.onError(
            `Received ${this.droppedFrameCount} invalid frames in a row — the connection may be unstable.`,
          )
        }
        return
      }
      // Valid frame received — reset the consecutive drop counter
      this.droppedFrameCount = 0
      this.callbacks.onFrame(frame)
    }

    this.ws.onerror = () => {
      this.callbacks.onError('WebSocket connection error')
    }

    this.ws.onclose = (event: CloseEvent) => {
      this.ws = null
      this._stopHeartbeat()
      this.callbacks.onDisconnected()
      if (!this.intentionalClose && event.code !== 1000) {
        this._scheduleReconnect()
      }
    }
  }

  private _scheduleReconnect(): void {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      this.callbacks.onError('Connection failed after 3 attempts. Click retry to reconnect.')
      return
    }

    const delay = Math.pow(2, this.reconnectAttempts) * 1000 // 1s, 2s, 4s
    this.reconnectAttempts++

    this.reconnectTimer = setTimeout(() => {
      this._createSocket()
    }, delay)
  }

  private _clearReconnectTimer(): void {
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  private _startHeartbeat(): void {
    this._stopHeartbeat()
    // Send a ping every 30s to keep the connection alive through proxies and firewalls
    this.heartbeatTimer = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ type: 'ping' }))
      }
    }, 30_000)
  }

  private _stopHeartbeat(): void {
    if (this.heartbeatTimer !== null) {
      clearInterval(this.heartbeatTimer)
      this.heartbeatTimer = null
    }
  }
}
