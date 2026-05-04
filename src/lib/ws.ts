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

// F-S6: discriminated union separates "mint new session" (no session_id) from
// "continue existing session" (session_id present). Callers narrow via
// `'session_id' in frame` to determine which path applies.
export type WsMessageFrame =
  | { type: 'message'; content: string; agent_id?: string }
  | { type: 'message'; content: string; session_id: string; agent_id?: string }

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

export interface WsAttachSessionFrame {
  type: 'attach_session'
  session_id: string
}

export interface WsDevicePairingResponseFrame {
  type: 'device_pairing_response'
  device_id: string
  decision: 'approve' | 'reject'
}

// F-S9: append session_close to the WsClientFrame.SessionID usage list (it mirrors cancel)

export type WsSendFrame = WsAuthFrame | WsMessageFrame | WsCancelFrame | WsExecApprovalResponseFrame | WsPingFrame | WsAttachSessionFrame | WsDevicePairingResponseFrame

// Emitted by the server immediately after it mints a new session_id
// (i.e. when the SPA sent a message frame without a session_id).
// The SPA stores this id as the new activeSessionId.
export interface WsSessionStartedFrame {
  type: 'session_started'
  session_id: string
  agent_id?: string
}

// F-S5: session-scoped frames require session_id (non-optional).
// The compile-time requirement prevents future frames from accidentally omitting it.
// Global frames (error, pong, session_state, device_pairing_*) keep session_id optional.

export interface WsTokenFrame {
  type: 'token'
  session_id: string
  content: string
}

export interface WsDoneFrame {
  type: 'done'
  session_id: string
  stats?: { tokens: number; cost: number; duration_ms: number; tokens_dropped?: number }
}

export interface WsErrorFrame {
  type: 'error'
  // Global error frames need not be session-scoped; session_id optional.
  session_id?: string
  message: string
}

export interface WsToolCallStartFrame {
  type: 'tool_call_start'
  session_id: string
  tool: string
  call_id: string
  params: Record<string, unknown>
  parent_call_id?: string
  agent_id?: string
}

export interface WsToolCallResultFrame {
  type: 'tool_call_result'
  session_id: string
  tool: string
  call_id: string
  result: unknown
  status: 'success' | 'error'
  duration_ms?: number
  error?: string
  parent_call_id?: string
  agent_id?: string
}

// FR-H-004: subagent span bracket frames
export interface WsSubagentStartFrame {
  type: 'subagent_start'
  session_id: string
  span_id: string
  parent_call_id: string
  task_label: string
  agent_id?: string
}

export interface WsSubagentEndFrame {
  type: 'subagent_end'
  session_id: string
  span_id: string
  status: 'success' | 'error' | 'cancelled' | 'interrupted' | 'timeout'
  duration_ms?: number
  final_result?: string
  /**
   * Coordination with wave-1a-go W1-9: optional reason for interrupted status.
   * Backend populates this when the sub-turn was interrupted by the parent.
   */
  reason?: 'parent_timeout' | 'parent_cancelled' | 'parent_done_early' | 'unknown'
}

/**
 * Truncation sentinel emitted by pkg/gateway/replay.go:truncateResult when a
 * tool result exceeds 10 KiB. The result field of WsToolCallResultFrame will
 * be an object matching this shape instead of the raw result.
 *
 * Also see TruncatedResult helper type below for UI narrowing.
 */
export interface TruncatedResult {
  _truncated: true
  original_size_bytes: number
  preview: string
}

/**
 * Marshal-error sentinel emitted when json.Marshal fails on the tool result.
 * The result field of WsToolCallResultFrame will be an object matching this shape.
 */
export interface MarshalErrorResult {
  _marshal_error: string
}

export interface WsExecApprovalRequestFrame {
  type: 'exec_approval_request'
  session_id: string
  id: string
  command: string
  working_dir?: string
  matched_policy?: string
}

export interface WsTaskStatusChangedFrame {
  type: 'task_status_changed'
  session_id: string
  task_id: string
  status: string
  agent_id?: string
}

export interface WsReplayMessageFrame {
  type: 'replay_message'
  session_id: string
  content: string
  role: string
  /** Server-assigned message id, used for dedup on reconnect (optional for back-compat). */
  id?: string
  timestamp?: string
}

export interface WsRateLimitFrame {
  type: 'rate_limit'
  session_id: string
  scope: 'agent' | 'channel' | 'global'
  resource: string
  policy_rule: string
  retry_after_seconds: number
  agent_id?: string
  tool?: string
}

export interface WsMediaPart {
  type: 'image' | 'audio' | 'video' | 'file'
  url: string
  filename: string
  content_type: string
  caption?: string
}

export interface WsMediaFrame {
  type: 'media'
  session_id: string
  parts: WsMediaPart[]
}

export interface WsAgentSwitchedFrame {
  type: 'agent_switched'
  session_id: string
  agent_id: string
  agent_name?: string  // included by backend for display without an extra lookup
}

// FR-011, FR-082: tool-policy approval request
export interface WsToolApprovalRequiredFrame {
  type: 'tool_approval_required'
  approval_id: string
  tool_call_id: string
  tool_name: string
  args: Record<string, unknown>
  agent_id: string
  session_id: string
  turn_id: string
  /** Relative expiry in milliseconds from receipt. Client computes expiresAt = Date.now() + expires_in_ms. */
  expires_in_ms: number
}

// FR-052, FR-073, FR-081: session state reset on WS reconnect
export interface WsSessionStatePendingApproval {
  approval_id: string
  session_id: string
  tool_name: string
  agent_id: string
  expires_in_ms: number
}

export interface WsSessionStateFrame {
  type: 'session_state'
  user_id: string
  pending_approvals: WsSessionStatePendingApproval[]
  emitted_at: string
}

// FR-016, MAJ-009: system overload notification
export interface WsSystemOverloadFrame {
  type: 'system_overload'
  session_id: string
  message?: string
}

// V1.B: emitted by replay.go just before `done` when the transcript contained
// duplicate tool_call_ids (older copies are silently overwritten on disk;
// only the latest one is replayed). Surfacing the count as a one-shot toast
// gives the operator visible feedback that something irregular was detected
// — server-only `slog.Warn` was invisible before this frame existed.
export interface WsReplayWarningFrame {
  type: 'replay_warning'
  session_id: string
  message: string
  stats?: {
    duplicate_tool_call_id_count?: number
  }
}

// F-S5: session_id contract — session-scoped frames have session_id: string (required);
// global frames (error, session_state, device_pairing_*) may omit it.
// WsSessionStartedFrame carries session_id as the minted id, not as routing context.
export type WsReceiveFrame =
  | WsSessionStartedFrame
  | WsTokenFrame
  | WsDoneFrame
  | WsErrorFrame
  | WsToolCallStartFrame
  | WsToolCallResultFrame
  | WsExecApprovalRequestFrame
  | WsTaskStatusChangedFrame
  | WsReplayMessageFrame
  | WsRateLimitFrame
  | WsMediaFrame
  | WsAgentSwitchedFrame
  | WsSubagentStartFrame
  | WsSubagentEndFrame
  | WsToolApprovalRequiredFrame
  | WsSessionStateFrame
  | WsSystemOverloadFrame
  | WsReplayWarningFrame

/** Allowed status values for subagent_end frames. */
const SUBAGENT_END_STATUSES = new Set<string>(['success', 'error', 'cancelled', 'interrupted', 'timeout'])

function isValidFrame(frame: unknown): frame is WsReceiveFrame {
  if (typeof frame !== 'object' || frame === null) return false
  const f = frame as Record<string, unknown>
  if (typeof f.type !== 'string') return false
  // W4-6: validate subagent_end status to prevent unknown-status render crashes.
  if (f.type === 'subagent_end') {
    if (typeof f.status !== 'string' || !SUBAGENT_END_STATUSES.has(f.status)) {
      return false
    }
  }
  return true
}

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

  // B1.3c: bound event handler references so they can be removed on disconnect.
  private _onVisibilityChange: (() => void) | null = null
  private _onOnline: (() => void) | null = null

  constructor(callbacks: WsConnectionCallbacks) {
    this.callbacks = callbacks
  }

  connect(): void {
    this.intentionalClose = false
    this.reconnectAttempts = 0
    this._attachWindowListeners()
    this._createSocket()
  }

  disconnect(): void {
    this.intentionalClose = true
    this._clearReconnectTimer()
    this._stopHeartbeat()
    this._detachWindowListeners()
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
      // Auth token is re-read on every (re-)connect so changes after
      // initial load take effect without a page refresh.
      // Check sessionStorage first (XSS protection), fall back to localStorage.
      const token = sessionStorage.getItem('omnipus_auth_token') ?? localStorage.getItem('omnipus_auth_token')
      if (token) {
        this.send({ type: 'auth', token })
      } else {
        console.warn('[ws] No auth token found — connecting unauthenticated')
      }
      this._startHeartbeat()
      this.callbacks.onConnected()
    }

    this.ws.onmessage = (event: MessageEvent) => {
      let parsed: unknown
      try {
        parsed = JSON.parse(event.data as string)
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
      if (!isValidFrame(parsed)) {
        console.warn('[ws] Invalid frame — missing type field:', parsed)
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
      this.callbacks.onFrame(parsed)
    }

    this.ws.onerror = () => {
      // onerror fires before onclose; onclose produces a richer message with
      // close code and reason. We emit a minimal diagnostic here so the
      // connection-error banner has a message, but avoid duplicating the
      // onclose banner message.
      this.callbacks.onError(`Connection error reaching ${getWsUrl()} — will retry`)
    }

    this.ws.onclose = (event: CloseEvent) => {
      this.ws = null
      this._stopHeartbeat()
      this.callbacks.onDisconnected()
      // B1.3c: surface non-1000/1001 close codes through the persistent connection
      // error banner (via onError → connectionStore.setConnectionError). The banner
      // in AppShell.tsx will remain visible until reconnect succeeds (onConnected
      // clears connectionError via setConnected(true)).
      if (!this.intentionalClose && event.code !== 1000 && event.code !== 1001) {
        const codeLabel = event.code ? ` code ${event.code}` : ''
        const reasonLabel = event.reason ? `: ${event.reason}` : ''
        this.callbacks.onError(
          `Disconnected from gateway —${codeLabel}${reasonLabel || ' connection lost'}. Reconnecting…`
        )
        this._scheduleReconnect()
      } else if (!this.intentionalClose) {
        // Normal close (1000/1001) that wasn't intentional — still reconnect but no banner.
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

  // B1.3c: attach window-level listeners that trigger reconnect on
  // visibilitychange (tab re-focused after backgrounding) and online (network
  // recovered). Both fire a reconnect immediately if in disconnected state,
  // resetting the backoff counter so the next attempt is fast.
  private _attachWindowListeners(): void {
    if (this._onVisibilityChange) return // already attached

    this._onVisibilityChange = () => {
      if (
        document.visibilityState === 'visible' &&
        !this.intentionalClose &&
        this.ws === null
      ) {
        // Reset backoff — user is actively looking at the page.
        this.reconnectAttempts = 0
        this._clearReconnectTimer()
        this._createSocket()
      }
    }

    this._onOnline = () => {
      if (!this.intentionalClose && this.ws === null) {
        // Network recovered — reset backoff and reconnect immediately.
        this.reconnectAttempts = 0
        this._clearReconnectTimer()
        this._createSocket()
      }
    }

    window.addEventListener('visibilitychange', this._onVisibilityChange)
    window.addEventListener('online', this._onOnline)
  }

  private _detachWindowListeners(): void {
    if (this._onVisibilityChange) {
      window.removeEventListener('visibilitychange', this._onVisibilityChange)
      this._onVisibilityChange = null
    }
    if (this._onOnline) {
      window.removeEventListener('online', this._onOnline)
      this._onOnline = null
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
