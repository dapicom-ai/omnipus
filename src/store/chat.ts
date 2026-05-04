import { create } from 'zustand'
import { generateId } from '@/lib/constants'
import { useUiStore } from '@/store/ui'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore, registerChatSetReplaying, registerChatResetForReplay } from '@/store/session'
import { queryClient } from '@/lib/queryClient'
import type { Message, ToolCall } from '@/lib/api'
import type { WsReceiveFrame, WsExecApprovalRequestFrame, WsReplayMessageFrame, WsRateLimitFrame, WsSubagentStartFrame, WsSubagentEndFrame } from '@/lib/ws'
import { useToolApprovalStore } from '@/store/toolApproval'
import { registerSyncChatForeground } from '@/store/session'

export interface MediaAttachment {
  type: 'image' | 'audio' | 'video' | 'file'
  url: string
  filename: string
  contentType: string
  caption?: string
}

// SpanStep is one step in a subagent span.
// The discriminant `kind` allows renderers to switch between tool calls
// and interleaved text fragments without a runtime type-check on all fields.
// Text steps are reserved for future subagent-text streaming; no emit site
// writes them yet, but the type admits them so a future sprint can add
// subagent-text streaming without a type change.
export type SpanStep =
  | { kind: 'tool'; tool: ToolCall & { call_id: string } }
  | { kind: 'text'; text: string; ts: number }

// FR-H-008/FR-H-009: a subagent span brackets one sub-turn.
// Discriminated union: 'running' vs terminal so TypeScript enforces that
// durationMs / finalResult / reason are only accessible on terminal spans.
interface SubagentSpanBase {
  spanId: string
  parentCallId: string
  taskLabel: string
  steps: SpanStep[]
}

export interface SubagentSpanRunning extends SubagentSpanBase {
  status: 'running'
}

export interface SubagentSpanTerminal extends SubagentSpanBase {
  status: 'success' | 'error' | 'cancelled' | 'interrupted' | 'timeout'
  durationMs: number
  finalResult?: string
  /** W1-9 coordination: reason populated when status is 'interrupted'. */
  reason?: 'parent_timeout' | 'parent_cancelled' | 'parent_done_early' | 'unknown'
}

export type SubagentSpan = SubagentSpanRunning | SubagentSpanTerminal

// A buffered frame waiting for its subagent_start to arrive (FR-H-009)
interface BufferedFrame {
  frame: WsReceiveFrame & { type: 'tool_call_start' | 'tool_call_result' }
  arrivedAt: number
}

export interface ChatMessage extends Message {
  isStreaming?: boolean
  media?: MediaAttachment[]
  spans?: SubagentSpan[]
}

export interface RateLimitEventData {
  scope: 'agent' | 'channel' | 'global'
  resource: string
  policyRule: string
  retryAfterSeconds: number
  agentId?: string
  tool?: string
}

export interface ExecApprovalRequest {
  id: string
  command: string
  working_dir?: string
  matched_policy?: string
  status: 'pending' | 'allowed' | 'denied' | 'always_allowed'
}

/** All per-session chat state for one concurrent session. */
export interface SessionChatState {
  messages: ChatMessage[]
  toolCalls: Record<string, ToolCall & { call_id: string }>
  toolCallOrder: string[]
  textAtToolCallStart: Record<string, string>
  pendingApprovals: ExecApprovalRequest[]
  isStreaming: boolean
  /** True from attach_session until first done frame — disables send input during replay. */
  isReplaying: boolean
  /** W3-9: set when a done frame arrives while isReplaying was true. */
  replayCompletedForSession: string | null
  sessionTokens: number
  sessionCost: number
  rateLimitEvent: RateLimitEventData | null
  /**
   * H1-FE: Unix timestamp (ms) when the most recent user message was sent for
   * this session. Used to guard against force-clearing isStreaming on the active
   * bucket when an unknown-sid done arrives — if the active session just sent a
   * user message recently it is very likely mid-stream, not a stale spinner.
   */
  lastUserMessageAt: number | null
}

function emptySessionState(): SessionChatState {
  return {
    messages: [],
    toolCalls: {},
    toolCallOrder: [],
    textAtToolCallStart: {},
    pendingApprovals: [],
    isStreaming: false,
    isReplaying: false,
    replayCompletedForSession: null,
    sessionTokens: 0,
    sessionCost: 0,
    rateLimitEvent: null,
    lastUserMessageAt: null,
  }
}

interface ChatStore {
  /** Per-session state buckets keyed by session_id. */
  sessionsById: Record<string, SessionChatState>

  // ── Foreground selectors (derived from sessionsById[activeSessionId]) ────────
  // These are convenience getters for the UI to read the active session's state.
  // They return stable empty values when no session is active.
  messages: ChatMessage[]
  isStreaming: boolean
  isReplaying: boolean
  replayCompletedForSession: string | null
  toolCalls: Record<string, ToolCall & { call_id: string }>
  toolCallOrder: string[]
  textAtToolCallStart: Record<string, string>
  pendingApprovals: ExecApprovalRequest[]
  sessionTokens: number
  sessionCost: number
  rateLimitEvent: RateLimitEventData | null

  // ── Actions that operate on the foreground session ───────────────────────────
  setReplaying: (value: boolean) => void
  setMessages: (messages: Message[]) => void
  appendMessage: (message: ChatMessage) => void
  updateLastAssistantMessage: (content: string, done?: boolean) => void
  markLastMessageInterrupted: () => void

  startToolCall: (callId: string, tool: string, params: Record<string, unknown>) => void
  resolveToolCall: (callId: string, result: unknown, status: 'success' | 'error', durationMs?: number, error?: string) => void
  cancelToolCall: (callId: string) => void

  addApprovalRequest: (req: WsExecApprovalRequestFrame) => void
  resolveApproval: (id: string, status: 'allowed' | 'denied' | 'always_allowed') => void

  updateSessionStats: (tokens: number, cost: number) => void
  setRateLimitEvent: (event: RateLimitEventData) => void
  clearRateLimitEvent: () => void

  startSpan: (frame: WsSubagentStartFrame) => void
  endSpan: (frame: WsSubagentEndFrame) => void
  attachStepToSpan: (parentCallId: string, step: ToolCall & { call_id: string }) => void

  // Resets only the foreground session bucket. Does NOT affect other sessions.
  resetSession: () => void

  // Wipes a specific session bucket and marks it as replaying so the next
  // replay frames rebuild from scratch. Used on WS reconnect to prevent
  // duplicate bubbles when the gateway re-replays the transcript.
  resetSessionForReplay: (sessionId: string) => void

  // ── Actions ───────────────────────────────────────────────────────────────────
  sendMessage: (content: string) => void
  cancelStream: () => void
  respondToApproval: (id: string, decision: 'allow' | 'deny' | 'always') => void
  respondToPairing: (deviceId: string, decision: 'approve' | 'reject') => void

  handleFrame: (frame: WsReceiveFrame) => void
}

// Module-scoped handle for the 60s auto-clear timer on rate-limit events, keyed per session.
const rateLimitClearTimers: Record<string, ReturnType<typeof setTimeout>> = {}

// Tracks when isReplaying was most recently set to true per session, keyed by session_id.
const replayingStartedAt: Record<string, number> = {}

// W1-7: diagnostic flag per session — true when at least one replay_message was processed.
const sawReplayMessageThisTurn: Record<string, boolean> = {}

// FR-H-009: out-of-order frame buffer — tool_call_start/result frames that
// arrived before their subagent_start. Keyed by `${sessionId}:${parentCallId}`.
// Dropped to flat rendering after ORPHAN_BUFFER_TTL_MS if no subagent_start arrives.
const ORPHAN_BUFFER_TTL_MS = 10_000
const pendingByParentCallId: Record<string, BufferedFrame[]> = {}
const orphanTimers: Record<string, ReturnType<typeof setTimeout>> = {}

// ── Frame-routing helpers ─────────────────────────────────────────────────────

function hasOpenSpan(messages: ChatMessage[], parentCallId: string): boolean {
  return messages.some(
    (m) => m.role === 'assistant' && m.spans?.some((s) => s.parentCallId === parentCallId)
  )
}

function bufferForSpan(
  bufferKey: string,
  frame: BufferedFrame['frame'],
  onTimeout: (buffered: BufferedFrame[]) => void,
): void {
  if (!pendingByParentCallId[bufferKey]) {
    pendingByParentCallId[bufferKey] = []
    if (!orphanTimers[bufferKey]) {
      orphanTimers[bufferKey] = setTimeout(() => {
        const buffered = pendingByParentCallId[bufferKey] ?? []
        delete pendingByParentCallId[bufferKey]
        delete orphanTimers[bufferKey]
        if (buffered.length > 0) {
          onTimeout(buffered)
        }
      }, ORPHAN_BUFFER_TTL_MS)
    }
  }
  pendingByParentCallId[bufferKey].push({ frame, arrivedAt: Date.now() })
}

const EMPTY_BUCKET = emptySessionState()

// F-S1: all server→client frames that must carry session_id.
// Frames in this set without a session_id are routing errors.
// Global frames (error, auth_*, ping, pong, device_pairing_*) are intentionally absent.
const SESSION_SCOPED_FRAME_TYPES = new Set([
  'token', 'done', 'tool_call_start', 'tool_call_result',
  'subagent_start', 'subagent_end', 'replay_message', 'replay_done',
  'agent_switched', 'task_status_changed', 'exec_approval_request',
  'tool_approval_required', 'rate_limit', 'media', 'session_started',
  'system_overload', 'session_close_ack',
])

// F-S2: FALLBACK_SID exists only in test mode so tests that don't establish a session
// still route frames to a consistent bucket. In production getActiveSid() returns null
// when no session is active; frame writers must early-return on null.
const FALLBACK_SID = import.meta.env.MODE === 'test' ? '__default' : null

export const useChatStore = create<ChatStore>((set, get) => {
  // ── Internal helpers that mutate a named session bucket ─────────────────────
  // These read/write sessionsById[sid] and then re-sync foreground fields.

  // F-S2: returns null in production when no session is active.
  // In test mode returns FALLBACK_SID ('__default') for test compatibility.
  function getActiveSid(): string | null {
    return useSessionStore.getState().activeSessionId ?? FALLBACK_SID
  }

  /** Find or lazily create a bucket for sid. No-op if sid is null. */
  function withBucket(sid: string | null, updater: (bucket: SessionChatState) => Partial<SessionChatState>): void {
    if (!sid) return
    set((state) => {
      const existing = state.sessionsById[sid] ?? emptySessionState()
      const patch = updater(existing)
      const updated: SessionChatState = { ...existing, ...patch }
      const sessionsById = { ...state.sessionsById, [sid]: updated }
      const activeSid = getActiveSid()
      const activeBucket = (activeSid ? sessionsById[activeSid] : null) ?? EMPTY_BUCKET
      return { sessionsById, ...activeBucket }
    })
  }

  /** Re-sync foreground fields from sessionsById after an external session switch. */
  function syncForeground(): void {
    set((state) => {
      const activeSid = getActiveSid()
      const fg = (activeSid ? state.sessionsById[activeSid] : null) ?? EMPTY_BUCKET
      return { ...fg }
    })
  }

  return {
    sessionsById: {},

    // Foreground selectors — derived from sessionsById[activeSessionId].
    // Initial values are the empty-session defaults.
    ...emptySessionState(),

    setReplaying: (value) => {
      const sid = getActiveSid()
      if (!sid) return
      if (value) {
        // Only reset the window start on a false→true transition.
        const current = get().sessionsById[sid]
        if (!current?.isReplaying) {
          replayingStartedAt[sid] = Date.now()
        }
        withBucket(sid, () => ({ isReplaying: true }))
        return
      }
      // No-op if already false.
      const current = get().sessionsById[sid]
      if (!current?.isReplaying) {
        if (sawReplayMessageThisTurn[sid]) {
          console.warn('[chat] setReplaying(false) ignored — isReplaying was already false despite replay_message having been processed. Likely attachToSession race.')
        }
        return
      }
      sawReplayMessageThisTurn[sid] = false
      const elapsed = Date.now() - (replayingStartedAt[sid] ?? 0)
      const MIN_REPLAY_DISPLAY_MS = 250
      if (elapsed >= MIN_REPLAY_DISPLAY_MS) {
        withBucket(sid, () => ({ isReplaying: false }))
      } else {
        setTimeout(() => withBucket(sid, () => ({ isReplaying: false })), MIN_REPLAY_DISPLAY_MS - elapsed)
      }
    },

    setMessages: (messages) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, () => ({ ...emptySessionState(), messages: messages as ChatMessage[] }))
    },

    appendMessage: (message) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => ({ messages: [...b.messages, message] }))
    },

    updateLastAssistantMessage: (content, done = false) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        const msgs = [...b.messages]
        let lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
        if (lastIdx === -1) {
          const placeholder: ChatMessage = {
            id: generateId(),
            role: 'assistant',
            content: '',
            timestamp: new Date().toISOString(),
            status: 'streaming',
            isStreaming: true,
          }
          msgs.push(placeholder)
          lastIdx = msgs.length - 1
        }
        msgs[lastIdx] = {
          ...msgs[lastIdx],
          content: msgs[lastIdx].content + content,
          isStreaming: !done,
          status: done ? 'done' : 'streaming',
        }
        return { messages: msgs, isStreaming: !done }
      })
    },

    markLastMessageInterrupted: () => {
      const sid = getActiveSid()
      withBucket(sid, (b) => {
        const msgs = [...b.messages]
        const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
        if (lastIdx === -1) return {}
        msgs[lastIdx] = {
          ...msgs[lastIdx],
          isStreaming: false,
          status: 'interrupted',
        }
        return { messages: msgs, isStreaming: false }
      })
      // If no streaming assistant message was found in the active bucket, search
      // all buckets. This handles scenarios where a message was appended to a
      // different bucket before the active session was set (e.g. in test scaffolding).
      const state = get()
      const activeBucket = sid ? state.sessionsById[sid] : undefined
      const hasStreamingInActive = activeBucket?.messages.some((m: ChatMessage) => m.role === 'assistant' && m.isStreaming)
      if (!hasStreamingInActive) {
        for (const [bucketSid, bucket] of Object.entries(state.sessionsById)) {
          if (bucketSid === sid) continue
          const lastIdx = bucket.messages.map((m) => m.role).lastIndexOf('assistant')
          if (lastIdx !== -1 && bucket.messages[lastIdx].isStreaming) {
            // Update the background bucket AND sync the updated messages to the foreground
            // flat field so callers reading get().messages can see the change.
            useChatStore.setState((s) => {
              const b = s.sessionsById[bucketSid]
              if (!b) return {}
              const msgs = [...b.messages]
              const idx = msgs.map((m) => m.role).lastIndexOf('assistant')
              if (idx === -1) return {}
              msgs[idx] = { ...msgs[idx], isStreaming: false, status: 'interrupted' }
              const updated = { ...b, messages: msgs, isStreaming: false }
              return {
                sessionsById: { ...s.sessionsById, [bucketSid]: updated },
                // Propagate to flat foreground so observers see the interrupted message.
                messages: msgs,
                isStreaming: false,
              }
            })
            break
          }
        }
      }
    },

    startToolCall: (callId, tool, params) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        const lastMsg = b.messages[b.messages.length - 1]
        const textSnapshot = (lastMsg?.role === 'assistant' ? lastMsg.content : '') ?? ''
        return {
          toolCalls: {
            ...b.toolCalls,
            [callId]: { id: callId, call_id: callId, tool, params, status: 'running' },
          },
          toolCallOrder: [...b.toolCallOrder, callId],
          textAtToolCallStart: { ...b.textAtToolCallStart, [callId]: textSnapshot },
        }
      })
    },

    resolveToolCall: (callId, result, status, durationMs, error) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        if (!b.toolCalls[callId]) {
          console.debug('[chat] resolveToolCall for unknown call_id', callId)
          return {}
        }
        return {
          toolCalls: {
            ...b.toolCalls,
            [callId]: { ...b.toolCalls[callId], result, status, duration_ms: durationMs, error },
          },
        }
      })
    },

    cancelToolCall: (callId) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        if (!b.toolCalls[callId]) return {}
        return {
          toolCalls: {
            ...b.toolCalls,
            [callId]: { ...b.toolCalls[callId], status: 'cancelled' },
          },
        }
      })
    },

    startSpan: (frame) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        const msgs = [...b.messages]
        const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
        if (lastIdx === -1) return {}
        const span: SubagentSpanRunning = {
          spanId: frame.span_id,
          parentCallId: frame.parent_call_id,
          taskLabel: frame.task_label,
          status: 'running',
          steps: [],
        }
        const bufferKey = `${sid}:${frame.parent_call_id}`
        const buffered = pendingByParentCallId[bufferKey] ?? []
        delete pendingByParentCallId[bufferKey]
        if (orphanTimers[bufferKey]) {
          clearTimeout(orphanTimers[bufferKey])
          delete orphanTimers[bufferKey]
        }
        for (const { frame: bf } of buffered) {
          if (bf.type === 'tool_call_start') {
            span.steps.push({
              kind: 'tool',
              tool: {
                id: bf.call_id,
                call_id: bf.call_id,
                tool: bf.tool,
                params: bf.params,
                status: 'running',
              },
            })
          } else if (bf.type === 'tool_call_result') {
            const existingIdx = span.steps.findIndex(
              (s) => s.kind === 'tool' && s.tool.call_id === bf.call_id
            )
            if (existingIdx !== -1) {
              const existing = span.steps[existingIdx]
              if (existing.kind === 'tool') {
                span.steps[existingIdx] = {
                  kind: 'tool',
                  tool: {
                    ...existing.tool,
                    result: bf.result,
                    status: bf.status,
                    duration_ms: bf.duration_ms,
                    error: bf.error,
                  },
                }
              }
            }
          }
        }
        msgs[lastIdx] = {
          ...msgs[lastIdx],
          spans: [...(msgs[lastIdx].spans ?? []), span],
        }
        return { messages: msgs }
      })
    },

    endSpan: (frame) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        const msgs = [...b.messages]
        for (let i = msgs.length - 1; i >= 0; i--) {
          const msg = msgs[i]
          if (msg.role !== 'assistant' || !msg.spans) continue
          const spanIdx = msg.spans.findIndex((s) => s.spanId === frame.span_id)
          if (spanIdx === -1) continue
          const existingSpan = msg.spans[spanIdx]
          const updatedSpans = [...msg.spans]
          const terminalSpan: SubagentSpanTerminal = {
            spanId: existingSpan.spanId,
            parentCallId: existingSpan.parentCallId,
            taskLabel: existingSpan.taskLabel,
            steps: existingSpan.steps,
            status: frame.status,
            durationMs: frame.duration_ms ?? 0,
            finalResult: frame.final_result,
            reason: frame.reason,
          }
          updatedSpans[spanIdx] = terminalSpan
          msgs[i] = { ...msgs[i], spans: updatedSpans }
          return { messages: msgs }
        }
        console.warn('[chat] subagent_end received for unknown span_id', { spanId: frame.span_id })
        return {}
      })
    },

    attachStepToSpan: (parentCallId, step) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => {
        const msgs = [...b.messages]
        for (let i = msgs.length - 1; i >= 0; i--) {
          const msg = msgs[i]
          if (msg.role !== 'assistant' || !msg.spans) continue
          const spanIdx = msg.spans.findIndex((s) => s.parentCallId === parentCallId)
          if (spanIdx === -1) continue
          const updatedSpans = [...msg.spans]
          const existingIdx = updatedSpans[spanIdx].steps.findIndex(
            (s) => s.kind === 'tool' && s.tool.call_id === step.call_id
          )
          if (existingIdx !== -1) {
            const existingStep = updatedSpans[spanIdx].steps[existingIdx]
            if (existingStep.kind === 'tool') {
              const updatedSteps = [...updatedSpans[spanIdx].steps]
              updatedSteps[existingIdx] = { kind: 'tool', tool: { ...existingStep.tool, ...step } }
              updatedSpans[spanIdx] = { ...updatedSpans[spanIdx], steps: updatedSteps }
            }
          } else {
            updatedSpans[spanIdx] = {
              ...updatedSpans[spanIdx],
              steps: [...updatedSpans[spanIdx].steps, { kind: 'tool', tool: step }],
            }
          }
          msgs[i] = { ...msgs[i], spans: updatedSpans }
          return { messages: msgs }
        }
        return {}
      })
    },

    addApprovalRequest: (req) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => ({
        pendingApprovals: [...b.pendingApprovals, { ...req, status: 'pending' }],
      }))
    },

    resolveApproval: (id, status) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => ({
        pendingApprovals: b.pendingApprovals.map((a) => a.id === id ? { ...a, status } : a),
      }))
    },

    updateSessionStats: (tokens, cost) => {
      const sid = getActiveSid()
      if (!sid) return
      withBucket(sid, (b) => ({
        sessionTokens: b.sessionTokens + tokens,
        sessionCost: b.sessionCost + cost,
      }))
    },

    setRateLimitEvent: (event) => {
      const sid = getActiveSid()
      if (!sid) return
      if (rateLimitClearTimers[sid] != null) {
        clearTimeout(rateLimitClearTimers[sid])
        delete rateLimitClearTimers[sid]
      }
      withBucket(sid, () => ({ rateLimitEvent: event }))
      rateLimitClearTimers[sid] = setTimeout(() => {
        delete rateLimitClearTimers[sid]
        set((state) => {
          const bucket = state.sessionsById[sid]
          if (!bucket || bucket.rateLimitEvent !== event) return {}
          const updated: SessionChatState = { ...bucket, rateLimitEvent: null }
          const sessionsById = { ...state.sessionsById, [sid]: updated }
          const activeSid = getActiveSid()
          const fg = (activeSid ? sessionsById[activeSid] : undefined) ?? EMPTY_BUCKET
          return { sessionsById, ...fg }
        })
      }, 60_000)
    },

    clearRateLimitEvent: () => {
      const sid = getActiveSid()
      if (!sid) return
      if (rateLimitClearTimers[sid] != null) {
        clearTimeout(rateLimitClearTimers[sid])
        delete rateLimitClearTimers[sid]
      }
      withBucket(sid, () => ({ rateLimitEvent: null }))
    },

    resetSession: () => {
      const sid = getActiveSid()
      if (!sid) return
      // Clear orphan buffers for this session only.
      const prefix = `${sid}:`
      for (const key of Object.keys(orphanTimers)) {
        if (key.startsWith(prefix)) {
          clearTimeout(orphanTimers[key])
          delete orphanTimers[key]
        }
      }
      for (const key of Object.keys(pendingByParentCallId)) {
        if (key.startsWith(prefix)) {
          delete pendingByParentCallId[key]
        }
      }
      if (rateLimitClearTimers[sid] != null) {
        clearTimeout(rateLimitClearTimers[sid])
        delete rateLimitClearTimers[sid]
      }
      sawReplayMessageThisTurn[sid] = false
      withBucket(sid, () => emptySessionState())
    },

    resetSessionForReplay: (sessionId) => {
      // Clear all transient state so the upcoming replay rebuilds from
      // scratch. This is the targeted reset for WS reconnect: without it,
      // replay frames append duplicate bubbles to the existing bucket.
      const prefix = `${sessionId}:`
      for (const key of Object.keys(orphanTimers)) {
        if (key.startsWith(prefix)) {
          clearTimeout(orphanTimers[key])
          delete orphanTimers[key]
        }
      }
      for (const key of Object.keys(pendingByParentCallId)) {
        if (key.startsWith(prefix)) {
          delete pendingByParentCallId[key]
        }
      }
      sawReplayMessageThisTurn[sessionId] = false
      replayingStartedAt[sessionId] = Date.now()
      withBucket(sessionId, () => ({
        ...emptySessionState(),
        isReplaying: true,
      }))
    },

    sendMessage: (content) => {
      const { connection, isConnected } = useConnectionStore.getState()
      const { activeSessionId, activeAgentId } = useSessionStore.getState()
      const { isStreaming } = get()

      if (isStreaming) {
        useConnectionStore.getState().setConnectionError('Please wait — a response is still generating.')
        return
      }
      if (!connection || !isConnected) {
        useConnectionStore.getState().setConnectionError('Cannot send message — not connected to the server. Check your connection and try again.')
        return
      }

      // When activeSessionId is null we do NOT render optimistically until
      // session_started arrives and gives us a real bucket key. This avoids
      // a temporary bucket that we'd have to migrate on the ack, at the cost
      // of ~1 round-trip of perceived latency on the very first message.
      if (activeSessionId !== null) {
        const userMsg: ChatMessage = {
          id: generateId(),
          session_id: activeSessionId,
          role: 'user',
          content,
          timestamp: new Date().toISOString(),
          status: 'done',
        }
        const assistantMsg: ChatMessage = {
          id: generateId(),
          session_id: activeSessionId,
          role: 'assistant',
          content: '',
          timestamp: new Date().toISOString(),
          status: 'streaming',
          isStreaming: true,
        }

        withBucket(activeSessionId, (b) => {
          const msgs = [...b.messages]
          const prevAssistantIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
          let toolCallsAfterReset: typeof b.toolCalls = b.toolCalls
          let toolCallOrderAfterReset: string[] = b.toolCallOrder

          if (prevAssistantIdx !== -1) {
            const prev = msgs[prevAssistantIdx]
            const alreadySeen = new Set((prev.tool_calls ?? []).map((tc) => tc.id))
            const liveIds = b.toolCallOrder.filter(
              (id) => !alreadySeen.has(id) && b.toolCalls[id],
            )
            if (liveIds.length > 0) {
              const baked = liveIds.map((id) => {
                const tc = b.toolCalls[id]
                return {
                  id,
                  tool: tc.tool,
                  params: tc.params,
                  result: tc.result,
                  status: tc.status,
                  duration_ms: tc.duration_ms,
                  error: tc.error,
                }
              })
              // Dedupe the merged tool_calls list by id so a re-bake (after
              // an attach + replay, or any other path that revisits live
              // ids) cannot leave duplicate ids on the message.
              const mergedById = new Map<string, NonNullable<typeof prev.tool_calls>[number]>()
              for (const tc of (prev.tool_calls ?? [])) mergedById.set(tc.id, tc)
              for (const tc of baked) mergedById.set(tc.id, tc)
              msgs[prevAssistantIdx] = {
                ...prev,
                tool_calls: Array.from(mergedById.values()),
              }
              const liveSet = new Set(liveIds)
              const remainingCalls: typeof b.toolCalls = {}
              for (const [k, v] of Object.entries(b.toolCalls)) {
                if (!liveSet.has(k)) remainingCalls[k] = v
              }
              toolCallsAfterReset = remainingCalls
              toolCallOrderAfterReset = b.toolCallOrder.filter((id) => !liveSet.has(id))
            }
          }

          return {
            messages: [...msgs, userMsg, assistantMsg],
            toolCalls: toolCallsAfterReset,
            toolCallOrder: toolCallOrderAfterReset,
            isStreaming: true,
            // H1-FE: record when user last sent a message so the unknown-sid
            // done handler can tell whether the active bucket is mid-stream.
            lastUserMessageAt: Date.now(),
          }
        })

        const sent = connection.send({
          type: 'message',
          content,
          session_id: activeSessionId,
          agent_id: activeAgentId ?? undefined,
        })

        if (!sent) {
          withBucket(activeSessionId, (b) => ({
            messages: b.messages.filter((m) => m.id !== userMsg.id && m.id !== assistantMsg.id),
            isStreaming: false,
          }))
          useConnectionStore.getState().setConnectionError('Message could not be sent — connection dropped. Please try again.')
        }
      } else {
        // No active session — send without session_id; server will mint one
        // and ack with session_started. UI renders nothing until that ack.
        const sent = connection.send({
          type: 'message',
          content,
          agent_id: activeAgentId ?? undefined,
        })
        if (!sent) {
          useConnectionStore.getState().setConnectionError('Message could not be sent — connection dropped. Please try again.')
        }
      }
    },

    cancelStream: () => {
      const { connection } = useConnectionStore.getState()
      const { activeSessionId } = useSessionStore.getState()
      const { isStreaming } = get()

      if (!connection || !isStreaming) return
      if (!activeSessionId) {
        // No server-side session established yet — just clear local streaming state.
        withBucket(getActiveSid(), () => ({ isStreaming: false }))
        get().markLastMessageInterrupted()
        return
      }

      const sent = connection.send({ type: 'cancel', session_id: activeSessionId })
      if (!sent) {
        console.warn('[chat] cancelStream: send failed — connection may be closed')
        useUiStore.getState().addToast({
          message: 'Could not send cancel — connection dropped. The response may continue briefly.',
          variant: 'error',
        })
      }

      get().markLastMessageInterrupted()

      withBucket(activeSessionId, (b) => {
        const updated = { ...b.toolCalls }
        for (const key of Object.keys(updated)) {
          if (updated[key].status === 'running') {
            updated[key] = { ...updated[key], status: 'cancelled' }
          }
        }
        return { toolCalls: updated, isStreaming: false }
      })
    },

    respondToApproval: (id, decision) => {
      const { connection } = useConnectionStore.getState()
      if (!connection) {
        useConnectionStore.getState().setConnectionError('Cannot respond to approval — not connected. Reconnect and try again.')
        return
      }

      const sent = connection.send({ type: 'exec_approval_response', id, decision })
      if (!sent) {
        useConnectionStore.getState().setConnectionError('Failed to send approval response — connection dropped. Reconnect and try again.')
        return
      }
      const statusMap = { allow: 'allowed', deny: 'denied', always: 'always_allowed' } as const
      get().resolveApproval(id, statusMap[decision])
    },

    respondToPairing: (deviceId, decision) => {
      const { connection } = useConnectionStore.getState()
      if (!connection) {
        useConnectionStore.getState().setConnectionError('Cannot respond to pairing — not connected. Reconnect and try again.')
        return
      }

      const sent = connection.send({ type: 'device_pairing_response', device_id: deviceId, decision })
      if (!sent) {
        useConnectionStore.getState().setConnectionError('Failed to send pairing response — connection dropped. Reconnect and try again.')
      }
    },

    handleFrame: (frame) => {
      // Resolve which session this frame belongs to.
      // session_started is special: it carries the new id for the pending message.
      const frameSessionId = (frame as { session_id?: string }).session_id
      const activeSid = getActiveSid()

      // F-S1: Route to the correct bucket.
      // Session-scoped frames missing session_id are treated differently per environment.
      const targetSid: string | null = (() => {
        if (frame.type === 'session_started') return activeSid // handled below, value unused
        if (frameSessionId) return frameSessionId
        if (SESSION_SCOPED_FRAME_TYPES.has(frame.type)) {
          if (import.meta.env.MODE === 'test') {
            // In test mode: fall back to active session so test scaffolding stays simple.
            console.warn('[chat] frame missing session_id — routing to active session', { type: frame.type, activeSid })
            return activeSid
          }
          // In production: drop the frame and surface a one-shot connection error.
          console.error('[chat] server frame missing session_id — dropping', { type: frame.type })
          useConnectionStore.getState().setConnectionError(
            'internal: server frame missing session_id — please reload'
          )
          return null
        }
        // Global frame (error, ping, pong, device_pairing_*, session_state) — use active.
        return activeSid
      })()

      const originalActiveSid = activeSid

      const store = get()

      switch (frame.type) {
        case 'session_started': {
          // Server minted a new session_id in response to a message sent without one.
          const newSid = frame.session_id
          // Register in session store and create the bucket.
          useSessionStore.getState().setActiveSession(newSid, frame.agent_id ?? useSessionStore.getState().activeAgentId)
          // Bucket is lazily created by first withBucket call; ensure it exists now
          // so the foreground syncs immediately.
          set((state) => {
            if (state.sessionsById[newSid]) return {}
            const sessionsById = { ...state.sessionsById, [newSid]: emptySessionState() }
            return { sessionsById, ...emptySessionState() }
          })
          // Invalidate sessions list so SessionPanel shows the new session.
          queryClient.invalidateQueries({ queryKey: ['sessions'] })
          break
        }

        case 'token':
          if (targetSid) {
            withBucket(targetSid, (b) => {
              const msgs = [...b.messages]
              let lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
              // Only reuse the last assistant bubble if it is still
              // streaming. A closed bubble (status=done) means the prior
              // LLM call has finalized and any new tokens are part of a
              // *new* turn-segment — typically a follow-up call after a
              // tool returned. Stuffing them back into the closed bubble
              // is what produced the "text-then-image-at-bottom" ordering.
              if (lastIdx !== -1 && !msgs[lastIdx].isStreaming) {
                lastIdx = -1
              }
              if (lastIdx === -1) {
                const placeholder: ChatMessage = {
                  id: generateId(),
                  role: 'assistant',
                  content: '',
                  timestamp: new Date().toISOString(),
                  status: 'streaming',
                  isStreaming: true,
                }
                msgs.push(placeholder)
                lastIdx = msgs.length - 1
              }
              msgs[lastIdx] = {
                ...msgs[lastIdx],
                content: msgs[lastIdx].content + frame.content,
                isStreaming: true,
                status: 'streaming',
              }
              return { messages: msgs, isStreaming: true }
            })
          }
          break

        case 'done':
          if (targetSid) {
            // B1.3d: when done arrives for a targetSid that isn't in sessionsById yet,
            // the session was probably switched away mid-stream. The active bucket's
            // isStreaming flag would otherwise stay true forever (infinite spinner).
            // Log a diagnostic warning and conditionally force-clear isStreaming on
            // the active bucket.
            //
            // H1-FE: Guard against corrupting an active mid-stream session.
            // Two cases where we must NOT force-clear the active bucket:
            //   1. targetSid === activeSid — the active session itself produced an
            //      unknown-sid done, which should never happen; the normal path below
            //      will handle it correctly, so do not fall through to the break.
            //   2. The active bucket sent a user message recently (< 10 s ago) and is
            //      still streaming — the done belongs to a different (wiped/replayed)
            //      session and the active spinner is legitimate.
            const knownSid = !!get().sessionsById[targetSid]
            if (!knownSid) {
              console.warn('chat.done_unknown_sid', { targetSid, activeSid: activeSid })
              const STREAM_GRACE_MS = 10_000
              if (activeSid && activeSid !== targetSid && get().sessionsById[activeSid]) {
                const activeBucket = get().sessionsById[activeSid]!
                const isActiveMidStream =
                  activeBucket.isStreaming &&
                  activeBucket.lastUserMessageAt !== null &&
                  Date.now() - activeBucket.lastUserMessageAt < STREAM_GRACE_MS
                if (!isActiveMidStream) {
                  // Active bucket spinner is likely a stale remnant from the wiped
                  // session — safe to clear.
                  withBucket(activeSid, () => ({ isStreaming: false }))
                } else {
                  console.warn('chat.done_unknown_sid_skipped_active_mid_stream', {
                    targetSid,
                    activeSid,
                    lastUserMessageAt: activeBucket.lastUserMessageAt,
                  })
                }
              }
              break
            }

            // Decide whether isReplaying must clear now vs. defer to a setTimeout.
            // The clear happens INSIDE the same withBucket return below — never via
            // a nested withBucket call, because the outer set() commits the bucket
            // last and clobbers any nested writes that ran during the updater.
            const sid = targetSid
            const wasReplaying = (get().sessionsById[sid] ?? EMPTY_BUCKET).isReplaying
            const elapsed = wasReplaying ? Date.now() - (replayingStartedAt[sid] ?? 0) : 0
            const MIN_REPLAY_DISPLAY_MS = 250
            const clearReplayingNow = wasReplaying && elapsed >= MIN_REPLAY_DISPLAY_MS
            if (wasReplaying) {
              sawReplayMessageThisTurn[sid] = false
              if (!clearReplayingNow) {
                setTimeout(() => withBucket(sid, () => ({ isReplaying: false })), MIN_REPLAY_DISPLAY_MS - elapsed)
              }
            }
            withBucket(sid, (b) => {
              const msgs = [...b.messages]
              const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
              if (lastIdx !== -1) {
                msgs[lastIdx] = {
                  ...msgs[lastIdx],
                  isStreaming: false,
                  status: 'done',
                }
              }
              const tokenDelta = frame.stats?.tokens ?? 0
              const costDelta = frame.stats?.cost ?? 0
              const replayCompleted = b.isReplaying ? sid : b.replayCompletedForSession
              const patch: Partial<SessionChatState> = {
                messages: msgs,
                isStreaming: false,
                sessionTokens: b.sessionTokens + tokenDelta,
                sessionCost: b.sessionCost + costDelta,
                replayCompletedForSession: replayCompleted,
              }
              if (clearReplayingNow) {
                patch.isReplaying = false
              }
              return patch
            })
          }
          break

        case 'error':
          {
            withBucket(targetSid, (b) => {
              const msgs = [...b.messages]
              const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
              if (lastIdx !== -1) {
                msgs[lastIdx] = {
                  ...msgs[lastIdx],
                  content: msgs[lastIdx].content || frame.message,
                  isStreaming: false,
                  status: 'error',
                }
                return { messages: msgs, isStreaming: false }
              }
              msgs.push({
                id: generateId(),
                role: 'assistant',
                content: frame.message,
                timestamp: new Date().toISOString(),
                status: 'error',
                isStreaming: false,
              })
              useConnectionStore.getState().setConnectionError(frame.message)
              return { messages: msgs, isStreaming: false }
            })
          }
          break

        case 'tool_call_start': {
          if (!targetSid) break
          const parentCallId = frame.parent_call_id
          if (parentCallId) {
            const b = get().sessionsById[targetSid] ?? emptySessionState()
            if (hasOpenSpan(b.messages, parentCallId)) {
              // Temporarily patch active session for attachStepToSpan.
              if (targetSid === originalActiveSid) {
                store.attachStepToSpan(parentCallId, {
                  id: frame.call_id,
                  call_id: frame.call_id,
                  tool: frame.tool,
                  params: frame.params,
                  status: 'running',
                })
              } else {
                // Direct bucket mutation for non-foreground session.
                withBucket(targetSid, (bucket) => {
                  const msgs = [...bucket.messages]
                  for (let i = msgs.length - 1; i >= 0; i--) {
                    const msg = msgs[i]
                    if (msg.role !== 'assistant' || !msg.spans) continue
                    const spanIdx = msg.spans.findIndex((s) => s.parentCallId === parentCallId)
                    if (spanIdx === -1) continue
                    const updatedSpans = [...msg.spans]
                    updatedSpans[spanIdx] = {
                      ...updatedSpans[spanIdx],
                      steps: [...updatedSpans[spanIdx].steps, {
                        kind: 'tool' as const,
                        tool: { id: frame.call_id, call_id: frame.call_id, tool: frame.tool, params: frame.params, status: 'running' as const },
                      }],
                    }
                    msgs[i] = { ...msgs[i], spans: updatedSpans }
                    return { messages: msgs }
                  }
                  return {}
                })
              }
            } else {
              const bufferKey = `${targetSid}:${parentCallId}`
              bufferForSpan(bufferKey, frame, (buffered) => {
                console.warn(`[chat] orphan frame: parent_call_id="${parentCallId}" session="${targetSid}" — subagent_start never arrived within ${ORPHAN_BUFFER_TTL_MS}ms. Releasing as flat tool calls.`)
                useUiStore.getState().addToast({
                  variant: 'default',
                  message: 'Some subagent steps arrived without their span — displayed as flat tool calls',
                })
                withBucket(targetSid, (bucket) => {
                  let patchToolCalls = { ...bucket.toolCalls }
                  let patchOrder = [...bucket.toolCallOrder]
                  let patchText = { ...bucket.textAtToolCallStart }
                  let patchMsgs = [...bucket.messages]
                  for (const { frame: bf } of buffered) {
                    if (bf.type === 'tool_call_start') {
                      const lastMsg = patchMsgs[patchMsgs.length - 1]
                      const textSnapshot = (lastMsg?.role === 'assistant' ? lastMsg.content : '') ?? ''
                      if (!lastMsg || lastMsg.role !== 'assistant') {
                        const ph: ChatMessage = { id: generateId(), role: 'assistant', content: '', timestamp: new Date().toISOString(), status: 'streaming', isStreaming: true }
                        patchMsgs = [...patchMsgs, ph]
                      }
                      patchToolCalls[bf.call_id] = { id: bf.call_id, call_id: bf.call_id, tool: bf.tool, params: bf.params, status: 'running' }
                      patchOrder = [...patchOrder, bf.call_id]
                      patchText[bf.call_id] = textSnapshot
                    } else if (bf.type === 'tool_call_result') {
                      if (patchToolCalls[bf.call_id]) {
                        patchToolCalls[bf.call_id] = { ...patchToolCalls[bf.call_id], result: bf.result, status: bf.status, duration_ms: bf.duration_ms, error: bf.error }
                      }
                    }
                  }
                  return { toolCalls: patchToolCalls, toolCallOrder: patchOrder, textAtToolCallStart: patchText, messages: patchMsgs }
                })
              })
            }
          } else {
            withBucket(targetSid, (b) => {
              const msgs = [...b.messages]
              const lastMsg = msgs[msgs.length - 1]
              const textSnapshot = (lastMsg?.role === 'assistant' ? lastMsg.content : '') ?? ''
              if (!lastMsg || lastMsg.role !== 'assistant') {
                const ph: ChatMessage = { id: generateId(), role: 'assistant', content: '', timestamp: new Date().toISOString(), status: 'streaming', isStreaming: true }
                msgs.push(ph)
              }
              // Reconnect/replay safety: if this call_id is already recorded
              // (we have a textAtToolCallStart snapshot for it), keep the
              // ORIGINAL snapshot. A reattach replays from the start of the
              // transcript while the bucket already holds the completed
              // assistant text — without this guard every snapshot gets
              // overwritten with "end of full text", which makes the
              // runtime adapter render every tool call AFTER the text
              // (the "tool calls grouped at the bottom" reconnect bug).
              // Likewise, don't downgrade a tool call's status from
              // success/error back to running.
              const orderHasCall = b.toolCallOrder.includes(frame.call_id)
              const existingSnapshot = b.textAtToolCallStart[frame.call_id]
              const existingTC = b.toolCalls[frame.call_id]
              return {
                messages: msgs,
                toolCalls: existingTC && existingTC.status !== 'running'
                  ? b.toolCalls
                  : {
                      ...b.toolCalls,
                      [frame.call_id]: { id: frame.call_id, call_id: frame.call_id, tool: frame.tool, params: frame.params, status: 'running' },
                    },
                toolCallOrder: orderHasCall ? b.toolCallOrder : [...b.toolCallOrder, frame.call_id],
                textAtToolCallStart: existingSnapshot !== undefined
                  ? b.textAtToolCallStart
                  : { ...b.textAtToolCallStart, [frame.call_id]: textSnapshot },
              }
            })
          }
          break
        }

        case 'tool_call_result': {
          if (!targetSid) break
          const parentCallId = frame.parent_call_id
          if (parentCallId) {
            const b = get().sessionsById[targetSid] ?? emptySessionState()
            if (hasOpenSpan(b.messages, parentCallId)) {
              withBucket(targetSid, (bucket) => {
                const msgs = [...bucket.messages]
                for (let i = msgs.length - 1; i >= 0; i--) {
                  const msg = msgs[i]
                  if (msg.role !== 'assistant' || !msg.spans) continue
                  const spanIdx = msg.spans.findIndex((s) => s.parentCallId === parentCallId)
                  if (spanIdx === -1) continue
                  const updatedSpans = [...msg.spans]
                  const existingIdx = updatedSpans[spanIdx].steps.findIndex((s) => s.kind === 'tool' && s.tool.call_id === frame.call_id)
                  const step = { id: frame.call_id, call_id: frame.call_id, tool: frame.tool, params: {}, result: frame.result, status: frame.status ?? 'success' as const, duration_ms: frame.duration_ms, error: frame.error }
                  if (existingIdx !== -1) {
                    const existingStep = updatedSpans[spanIdx].steps[existingIdx]
                    if (existingStep.kind === 'tool') {
                      const updatedSteps = [...updatedSpans[spanIdx].steps]
                      updatedSteps[existingIdx] = { kind: 'tool', tool: { ...existingStep.tool, ...step } }
                      updatedSpans[spanIdx] = { ...updatedSpans[spanIdx], steps: updatedSteps }
                    }
                  } else {
                    updatedSpans[spanIdx] = { ...updatedSpans[spanIdx], steps: [...updatedSpans[spanIdx].steps, { kind: 'tool' as const, tool: step }] }
                  }
                  msgs[i] = { ...msgs[i], spans: updatedSpans }
                  return { messages: msgs }
                }
                return {}
              })
            } else {
              const bufferKey = `${targetSid}:${parentCallId}`
              bufferForSpan(bufferKey, frame, (buffered) => {
                console.warn(`[chat] orphan frame: parent_call_id="${parentCallId}" session="${targetSid}" — subagent_start never arrived within ${ORPHAN_BUFFER_TTL_MS}ms. Releasing as flat tool calls.`)
                useUiStore.getState().addToast({
                  variant: 'default',
                  message: 'Some subagent steps arrived without their span — displayed as flat tool calls',
                })
                withBucket(targetSid, (bucket) => {
                  let patchToolCalls = { ...bucket.toolCalls }
                  let patchOrder = [...bucket.toolCallOrder]
                  let patchText = { ...bucket.textAtToolCallStart }
                  let patchMsgs = [...bucket.messages]
                  for (const { frame: bf } of buffered) {
                    if (bf.type === 'tool_call_start') {
                      const lastMsg = patchMsgs[patchMsgs.length - 1]
                      const textSnapshot = (lastMsg?.role === 'assistant' ? lastMsg.content : '') ?? ''
                      patchToolCalls[bf.call_id] = { id: bf.call_id, call_id: bf.call_id, tool: bf.tool, params: bf.params, status: 'running' }
                      patchOrder = [...patchOrder, bf.call_id]
                      patchText[bf.call_id] = textSnapshot
                    } else if (bf.type === 'tool_call_result') {
                      if (patchToolCalls[bf.call_id]) {
                        patchToolCalls[bf.call_id] = { ...patchToolCalls[bf.call_id], result: bf.result, status: bf.status, duration_ms: bf.duration_ms, error: bf.error }
                      }
                    }
                  }
                  return { toolCalls: patchToolCalls, toolCallOrder: patchOrder, textAtToolCallStart: patchText, messages: patchMsgs }
                })
              })
            }
          } else {
            withBucket(targetSid, (b) => {
              if (!b.toolCalls[frame.call_id]) {
                console.debug('[chat] resolveToolCall for unknown call_id', frame.call_id)
                return {}
              }
              return {
                toolCalls: {
                  ...b.toolCalls,
                  [frame.call_id]: {
                    ...b.toolCalls[frame.call_id],
                    result: frame.result,
                    status: frame.status ?? 'success',
                    duration_ms: frame.duration_ms,
                    error: frame.error,
                  },
                },
              }
            })
          }
          break
        }

        case 'subagent_start': {
          if (!targetSid) break
          const sf = frame as WsSubagentStartFrame
          // For subagent_start we need to operate on the target bucket directly.
          withBucket(targetSid, (b) => {
            const msgs = [...b.messages]
            const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
            if (lastIdx === -1) return {}
            const span: SubagentSpanRunning = {
              spanId: sf.span_id,
              parentCallId: sf.parent_call_id,
              taskLabel: sf.task_label,
              status: 'running',
              steps: [],
            }
            const bufferKey = `${targetSid}:${sf.parent_call_id}`
            const buffered = pendingByParentCallId[bufferKey] ?? []
            delete pendingByParentCallId[bufferKey]
            if (orphanTimers[bufferKey]) {
              clearTimeout(orphanTimers[bufferKey])
              delete orphanTimers[bufferKey]
            }
            for (const { frame: bf } of buffered) {
              if (bf.type === 'tool_call_start') {
                span.steps.push({ kind: 'tool', tool: { id: bf.call_id, call_id: bf.call_id, tool: bf.tool, params: bf.params, status: 'running' } })
              } else if (bf.type === 'tool_call_result') {
                const existingIdx = span.steps.findIndex((s) => s.kind === 'tool' && s.tool.call_id === bf.call_id)
                if (existingIdx !== -1) {
                  const existing = span.steps[existingIdx]
                  if (existing.kind === 'tool') {
                    span.steps[existingIdx] = { kind: 'tool', tool: { ...existing.tool, result: bf.result, status: bf.status, duration_ms: bf.duration_ms, error: bf.error } }
                  }
                }
              }
            }
            msgs[lastIdx] = { ...msgs[lastIdx], spans: [...(msgs[lastIdx].spans ?? []), span] }
            return { messages: msgs }
          })
          break
        }

        case 'subagent_end': {
          if (!targetSid) break
          const ef = frame as WsSubagentEndFrame
          withBucket(targetSid, (b) => {
            const msgs = [...b.messages]
            for (let i = msgs.length - 1; i >= 0; i--) {
              const msg = msgs[i]
              if (msg.role !== 'assistant' || !msg.spans) continue
              const spanIdx = msg.spans.findIndex((s) => s.spanId === ef.span_id)
              if (spanIdx === -1) continue
              const existingSpan = msg.spans[spanIdx]
              const updatedSpans = [...msg.spans]
              const terminalSpan: SubagentSpanTerminal = {
                spanId: existingSpan.spanId,
                parentCallId: existingSpan.parentCallId,
                taskLabel: existingSpan.taskLabel,
                steps: existingSpan.steps,
                status: ef.status,
                durationMs: ef.duration_ms ?? 0,
                finalResult: ef.final_result,
                reason: ef.reason,
              }
              updatedSpans[spanIdx] = terminalSpan
              msgs[i] = { ...msgs[i], spans: updatedSpans }
              return { messages: msgs }
            }
            console.warn('[chat] subagent_end received for unknown span_id', { spanId: ef.span_id })
            return {}
          })
          break
        }

        case 'exec_approval_request':
          if (targetSid) {
            withBucket(targetSid, (b) => ({
              pendingApprovals: [...b.pendingApprovals, { ...frame, status: 'pending' as const }],
            }))
          }
          break

        case 'task_status_changed':
          queryClient.invalidateQueries({ queryKey: ['tasks'] })
          break

        case 'agent_switched': {
          const newAgentId = frame.agent_id
          if (newAgentId) {
            const sessionStore = useSessionStore.getState()
            // Use the frame's session_id if present; fall back to active.
            const switchSid = frameSessionId ?? sessionStore.activeSessionId
            sessionStore.setActiveSession(switchSid, newAgentId)
          }
          queryClient.invalidateQueries({ queryKey: ['sessions'] })
          break
        }

        case 'replay_message': {
          if (!targetSid) break
          sawReplayMessageThisTurn[targetSid] = true
          const replayFrame = frame as WsReplayMessageFrame
          const role = (replayFrame.role || 'assistant') as 'user' | 'assistant'
          const text = replayFrame.content ?? ''
          withBucket(targetSid, (b) => {
            const msgs = [...b.messages]
            // Reconnection dedup: if a message with the same role and same
            // content is already at the tail of the bucket, the gateway is
            // re-replaying a transcript we already have in memory — skip
            // the push so we don't get a duplicate bubble.
            const tail = msgs[msgs.length - 1]
            if (tail && tail.role === role && (tail.content ?? '') === text) {
              return {}
            }
            // Coalesce assistant text into the trailing empty assistant bubble
            // that tool_call_start frames already created. Without this, replay
            // splits one agent turn into two bubbles: the first with media/tool
            // calls and an empty body (still showing "Thinking…"), the second
            // with the final text — visually out of chronological order.
            if (role === 'assistant') {
              const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
              if (lastIdx !== -1 && (msgs[lastIdx].content ?? '') === '') {
                msgs[lastIdx] = {
                  ...msgs[lastIdx],
                  content: text,
                  status: 'done',
                  isStreaming: false,
                }
                return { messages: msgs }
              }
            }
            msgs.push({
              id: generateId(),
              role,
              content: text,
              timestamp: new Date().toISOString(),
              status: 'done' as const,
            })
            return { messages: msgs }
          })
          break
        }

        case 'media': {
          if (!targetSid) break
          if (!Array.isArray(frame.parts) || frame.parts.length === 0) {
            console.warn('[chat] Received media frame with empty or invalid parts — ignoring')
            break
          }
          const attachments: MediaAttachment[] = frame.parts
            .filter((p) => p.url && p.type)
            .map((p) => ({
              type: p.type,
              url: p.url,
              filename: p.filename,
              contentType: p.content_type,
              caption: p.caption,
            }))
          if (attachments.length === 0) break
          withBucket(targetSid, (b) => {
            const msgs = [...b.messages]
            const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
            const canAttach =
              lastIdx !== -1 &&
              (msgs[lastIdx].isStreaming || (msgs[lastIdx].content ?? '') === '')
            const dedupe = (existing: MediaAttachment[] | undefined, incoming: MediaAttachment[]) => {
              const seen = new Set((existing ?? []).map((a) => a.url))
              const fresh = incoming.filter((a) => !seen.has(a.url))
              return [...(existing ?? []), ...fresh]
            }
            if (canAttach) {
              msgs[lastIdx] = {
                ...msgs[lastIdx],
                media: dedupe(msgs[lastIdx].media, attachments),
              }
              return { messages: msgs }
            }
            msgs.push({
              id: generateId(),
              role: 'assistant',
              content: '',
              timestamp: new Date().toISOString(),
              media: attachments,
            })
            return { messages: msgs }
          })
          break
        }

        case 'rate_limit': {
          const rlFrame = frame as WsRateLimitFrame
          const sid = targetSid ?? getActiveSid()
          if (!sid) break
          if (rateLimitClearTimers[sid] != null) {
            clearTimeout(rateLimitClearTimers[sid])
            delete rateLimitClearTimers[sid]
          }
          const event: RateLimitEventData = {
            scope: rlFrame.scope,
            resource: rlFrame.resource,
            policyRule: rlFrame.policy_rule,
            retryAfterSeconds: rlFrame.retry_after_seconds,
            agentId: rlFrame.agent_id,
            tool: rlFrame.tool,
          }
          withBucket(sid, () => ({ rateLimitEvent: event }))
          rateLimitClearTimers[sid] = setTimeout(() => {
            delete rateLimitClearTimers[sid]
            set((state) => {
              const bucket = state.sessionsById[sid]
              if (!bucket || bucket.rateLimitEvent !== event) return {}
              const updated: SessionChatState = { ...bucket, rateLimitEvent: null }
              const sessionsById = { ...state.sessionsById, [sid]: updated }
              const activeSidNow = getActiveSid()
              const fg = (activeSidNow ? sessionsById[activeSidNow] : null) ?? EMPTY_BUCKET
              return { sessionsById, ...fg }
            })
          }, 60_000)
          break
        }

        case 'tool_approval_required':
          useToolApprovalStore.getState().enqueue(frame)
          break

        case 'session_state':
          useToolApprovalStore.getState().reconcileWithSessionState(frame)
          break

        case 'system_overload':
          useUiStore.getState().addToast({
            message: frame.message ?? 'System at capacity — agent action blocked. Retry shortly.',
            variant: 'warning',
          })
          break

        case 'replay_warning':
          // V1.B: gateway detected duplicate tool_call_ids in the transcript on
          // replay. Server-only slog.Warn was invisible to operators because
          // the count was buried in done.Stats. One-shot toast surfaces it.
          useUiStore.getState().addToast({
            message: frame.message,
            variant: 'warning',
          })
          break

        default:
          console.warn('[chat] Unknown frame type', { type: (frame as { type?: string }).type })
          break
      }

      // After processing a frame for the foreground session, re-sync foreground fields
      // in case withBucket targeted a non-foreground session (background sessions).
      // When the target was foreground, withBucket already synced; this call is idempotent.
      syncForeground()
    },
  }
})

// Expose syncForeground so setActiveSession can call it after switching sessions.
// Avoiding a direct import of the session store here to keep the cycle-break intact.
export function syncChatForeground(): void {
  // Re-read active session from the session store and sync foreground fields.
  const activeSid = useSessionStore.getState().activeSessionId ?? FALLBACK_SID
  useChatStore.setState((state) => {
    const fg = (activeSid ? state.sessionsById[activeSid] : null) ?? EMPTY_BUCKET
    return { ...fg }
  })
}

// Register callbacks with the session store to break circular imports.
registerChatSetReplaying((value) => useChatStore.getState().setReplaying(value))
registerChatResetForReplay((sessionId) => useChatStore.getState().resetSessionForReplay(sessionId))
registerSyncChatForeground(syncChatForeground)


// F-S8: removed flat→bucket bidirectional sync subscriber.
// Tests now seed sessionsById directly (see resetStores() in test files).
// The subscriber was only needed for test scaffolding that set messages on the flat state;
// that pattern is no longer used.

// Detect direct useSessionStore.setState({activeSessionId: ...}) bypasses (used in tests).
// We intentionally do NOT auto-sync foreground here because it would overwrite flat fields
// (like isStreaming) that tests set directly before switching sessions. Foreground sync
// happens only through the store actions (setActiveSession, attachToSession, startNewSession).
// This comment documents the intentional gap for future maintainers.
