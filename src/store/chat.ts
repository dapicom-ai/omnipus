import { create } from 'zustand'
import { generateId } from '@/lib/constants'
import { useUiStore } from '@/store/ui'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore, registerChatResetSession, registerChatSetReplaying } from '@/store/session'
import { queryClient } from '@/lib/queryClient'
import type { Message, ToolCall } from '@/lib/api'
import type { WsReceiveFrame, WsExecApprovalRequestFrame, WsReplayMessageFrame, WsRateLimitFrame, WsSubagentStartFrame, WsSubagentEndFrame, WsToolApprovalRequiredFrame, WsSessionStateFrame } from '@/lib/ws'
import { useToolApprovalStore } from '@/store/toolApproval'

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
  streamCursor?: boolean
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

interface ChatStore {
  // Messages
  messages: ChatMessage[]
  isStreaming: boolean
  // FR-I-014: true from attach_session until first done frame — disables send input during replay
  isReplaying: boolean
  setReplaying: (value: boolean) => void
  // W3-9: tracks the session ID for which WS replay completed successfully.
  // Set when a done frame arrives while isReplaying was true.
  // Cleared on resetSession. Used to gate the REST fallback in ChatScreen.
  replayCompletedForSession: string | null
  setMessages: (messages: Message[]) => void
  appendMessage: (message: ChatMessage) => void
  updateLastAssistantMessage: (content: string, done?: boolean) => void
  markLastMessageInterrupted: () => void

  // Tool calls (keyed by call_id) + insertion order for interleaved rendering
  toolCalls: Record<string, ToolCall & { call_id: string }>
  toolCallOrder: string[]
  textAtToolCallStart: Record<string, string> // snapshot of assistant content when each tool call started
  startToolCall: (callId: string, tool: string, params: Record<string, unknown>) => void
  resolveToolCall: (callId: string, result: unknown, status: 'success' | 'error', durationMs?: number, error?: string) => void
  cancelToolCall: (callId: string) => void

  // Exec approval
  pendingApprovals: ExecApprovalRequest[]
  addApprovalRequest: (req: WsExecApprovalRequestFrame) => void
  resolveApproval: (id: string, status: 'allowed' | 'denied' | 'always_allowed') => void

  // Session cost/token tracking
  sessionTokens: number
  sessionCost: number
  updateSessionStats: (tokens: number, cost: number) => void

  // Rate limit event (set by WS rate_limit frame, auto-cleared after 60s)
  rateLimitEvent: RateLimitEventData | null
  setRateLimitEvent: (event: RateLimitEventData) => void
  clearRateLimitEvent: () => void

  // Subagent span management (FR-H-008, FR-H-009)
  startSpan: (frame: WsSubagentStartFrame) => void
  endSpan: (frame: WsSubagentEndFrame) => void
  attachStepToSpan: (parentCallId: string, step: ToolCall & { call_id: string }) => void

  // Reset all session-scoped state (called by sessionStore on session switch)
  resetSession: () => void

  // Actions
  sendMessage: (content: string) => void
  cancelStream: () => void
  respondToApproval: (id: string, decision: 'allow' | 'deny' | 'always') => void
  respondToPairing: (deviceId: string, decision: 'approve' | 'reject') => void

  // Inbound frame handler
  handleFrame: (frame: WsReceiveFrame) => void
}

// Module-scoped handle for the 60s auto-clear timer on rate-limit events.
// Kept outside the zustand store because it is ephemeral (not state) and must
// be cancellable across calls without retrieving it through the store.
let rateLimitClearTimer: ReturnType<typeof setTimeout> | null = null

// Tracks when isReplaying was most recently set to true, so setReplaying(false)
// can enforce a minimum display window (prevents sub-frame flicker of the
// "Loading session history..." placeholder and gives E2E tests an observable
// disabled-input window). See setReplaying action.
let replayingStartedAt = 0

// W1-7: diagnostic flag — true when at least one replay_message was processed
// in the current session/turn. Used to emit a warning if setReplaying(false)
// hits the no-op guard unexpectedly after a replay sequence ran.
let sawReplayMessageThisTurn = false

// FR-H-009: out-of-order frame buffer — tool_call_start/result frames that
// arrived before their subagent_start. Keyed by parent_call_id. Dropped to
// flat rendering after ORPHAN_BUFFER_TTL_MS if no subagent_start arrives.
const ORPHAN_BUFFER_TTL_MS = 10_000
const pendingByParentCallId: Record<string, BufferedFrame[]> = {}
const orphanTimers: Record<string, ReturnType<typeof setTimeout>> = {}

/** State reset applied whenever switching or attaching to a session. */
const CLEAN_SESSION_STATE = {
  messages: [] as ChatMessage[],
  toolCalls: {} as Record<string, ToolCall & { call_id: string }>,
  toolCallOrder: [] as string[],
  textAtToolCallStart: {} as Record<string, string>,
  pendingApprovals: [] as ExecApprovalRequest[],
  sessionTokens: 0,
  sessionCost: 0,
  isStreaming: false,
  isReplaying: false,
  // W3-9: cleared on session switch so new session gets a fresh replay tracking state.
  replayCompletedForSession: null as string | null,
  rateLimitEvent: null as RateLimitEventData | null,
} as const

// ── Frame-routing helpers — extracted from handleFrame to reduce duplication ──
//
// Both tool_call_start and tool_call_result share the same parent-span check +
// orphan-buffer logic. These module-level helpers encapsulate the shared parts.

/**
 * hasOpenSpan returns true when any assistant message in `messages` has a span
 * whose parentCallId matches `parentCallId`.
 */
function hasOpenSpan(messages: ChatMessage[], parentCallId: string): boolean {
  return messages.some(
    (m) => m.role === 'assistant' && m.spans?.some((s) => s.parentCallId === parentCallId)
  )
}

/**
 * bufferForSpan adds `frame` to the pending buffer for `parentCallId` and arms
 * the 10s orphan TTL if this is the first frame for that parent.
 * When the timer fires, `onTimeout` is called with the accumulated buffered frames.
 */
function bufferForSpan(
  parentCallId: string,
  frame: BufferedFrame['frame'],
  onTimeout: (buffered: BufferedFrame[]) => void,
): void {
  if (!pendingByParentCallId[parentCallId]) {
    pendingByParentCallId[parentCallId] = []
    if (!orphanTimers[parentCallId]) {
      orphanTimers[parentCallId] = setTimeout(() => {
        const buffered = pendingByParentCallId[parentCallId] ?? []
        delete pendingByParentCallId[parentCallId]
        delete orphanTimers[parentCallId]
        if (buffered.length > 0) {
          onTimeout(buffered)
        }
      }, ORPHAN_BUFFER_TTL_MS)
    }
  }
  pendingByParentCallId[parentCallId].push({ frame, arrivedAt: Date.now() })
}

export const useChatStore = create<ChatStore>((set, get) => ({
  messages: [],
  isStreaming: false,
  isReplaying: false,
  replayCompletedForSession: null,
  setReplaying: (value) => {
    // Minimum 250ms display window: (a) avoids flicker of the "Loading session
    // history..." placeholder on sub-frame replays, (b) gives E2E automation
    // an observable disabled-input window. Tracked module-local.
    if (value) {
      // Only reset the window start on a false→true transition. Repeated
      // setReplaying(true) calls while already replaying must NOT extend
      // the 250ms minimum — caught by W2-6c test.
      if (!get().isReplaying) {
        replayingStartedAt = Date.now()
      }
      set({ isReplaying: true })
      return
    }
    // No-op if already false (done on a live turn where no replay ran).
    if (!get().isReplaying) {
      // W1-7: if replay_message frames were processed this turn but we hit
      // the no-op path, something is wrong — likely attachToSession race.
      if (sawReplayMessageThisTurn) {
        console.warn(
          '[chat] setReplaying(false) ignored — isReplaying was already false despite replay_message having been processed. Likely attachToSession race.',
        )
      }
      return
    }
    // Clear the diagnostic flag on the true→false transition.
    sawReplayMessageThisTurn = false
    const elapsed = Date.now() - replayingStartedAt
    const MIN_REPLAY_DISPLAY_MS = 250
    if (elapsed >= MIN_REPLAY_DISPLAY_MS) {
      set({ isReplaying: false })
    } else {
      setTimeout(() => set({ isReplaying: false }), MIN_REPLAY_DISPLAY_MS - elapsed)
    }
  },
  setMessages: (messages) =>
    set({ ...CLEAN_SESSION_STATE, messages }),

  appendMessage: (message) =>
    set((state) => ({ messages: [...state.messages, message] })),

  updateLastAssistantMessage: (content, done = false) =>
    set((state) => {
      const msgs = [...state.messages]
      let lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
      if (lastIdx === -1) {
        // Token arrived before placeholder — create it now
        const placeholder: ChatMessage = {
          id: generateId(),
          role: 'assistant',
          content: '',
          timestamp: new Date().toISOString(),
          status: 'streaming',
          isStreaming: true,
          streamCursor: true,
        }
        msgs.push(placeholder)
        lastIdx = msgs.length - 1
      }
      msgs[lastIdx] = {
        ...msgs[lastIdx],
        content: msgs[lastIdx].content + content,
        isStreaming: !done,
        streamCursor: !done,
        status: done ? 'done' : 'streaming',
      }
      return { messages: msgs, isStreaming: !done }
    }),

  markLastMessageInterrupted: () =>
    set((state) => {
      const msgs = [...state.messages]
      const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
      if (lastIdx === -1) return {}
      msgs[lastIdx] = {
        ...msgs[lastIdx],
        isStreaming: false,
        streamCursor: false,
        status: 'interrupted',
      }
      return { messages: msgs, isStreaming: false }
    }),

  toolCalls: {},
  toolCallOrder: [],
  textAtToolCallStart: {},
  startToolCall: (callId, tool, params) =>
    set((state) => {
      // Capture the current assistant message text so we can interleave tool calls with text during rendering
      const lastMsg = state.messages[state.messages.length - 1]
      const textSnapshot = (lastMsg?.role === 'assistant' ? lastMsg.content : '') ?? ''
      return {
        toolCalls: {
          ...state.toolCalls,
          [callId]: { id: callId, call_id: callId, tool, params, status: 'running' },
        },
        toolCallOrder: [...state.toolCallOrder, callId],
        textAtToolCallStart: { ...state.textAtToolCallStart, [callId]: textSnapshot },
      }
    }),

  resolveToolCall: (callId, result, status, durationMs, error) =>
    set((state) => {
      if (!state.toolCalls[callId]) {
        // W3-7a: diagnostic for tool_call_result arriving for an unknown call_id.
        // Common causes: race between resetSession and a late result frame, or
        // a nested tool call that was handled via attachStepToSpan instead.
        console.debug('[chat] resolveToolCall for unknown call_id', callId)
        return state
      }
      return {
        toolCalls: {
          ...state.toolCalls,
          [callId]: {
            ...state.toolCalls[callId],
            result,
            status,
            duration_ms: durationMs,
            error,
          },
        },
      }
    }),

  cancelToolCall: (callId) =>
    set((state) => {
      if (!state.toolCalls[callId]) return state
      return {
        toolCalls: {
          ...state.toolCalls,
          [callId]: { ...state.toolCalls[callId], status: 'cancelled' },
        },
      }
    }),

  // FR-H-008: push a new span onto the current streaming assistant message
  startSpan: (frame) =>
    set((state) => {
      const msgs = [...state.messages]
      const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
      if (lastIdx === -1) return {}
      const span: SubagentSpanRunning = {
        spanId: frame.span_id,
        parentCallId: frame.parent_call_id,
        taskLabel: frame.task_label,
        status: 'running',
        steps: [],
      }
      // Drain any buffered frames that were waiting for this span
      const buffered = pendingByParentCallId[frame.parent_call_id] ?? []
      delete pendingByParentCallId[frame.parent_call_id]
      if (orphanTimers[frame.parent_call_id]) {
        clearTimeout(orphanTimers[frame.parent_call_id])
        delete orphanTimers[frame.parent_call_id]
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
    }),

  // FR-H-008: finalize span status, duration, finalResult
  endSpan: (frame) =>
    set((state) => {
      const msgs = [...state.messages]
      for (let i = msgs.length - 1; i >= 0; i--) {
        const msg = msgs[i]
        if (msg.role !== 'assistant' || !msg.spans) continue
        const spanIdx = msg.spans.findIndex((s) => s.spanId === frame.span_id)
        if (spanIdx === -1) continue
        const existingSpan = msg.spans[spanIdx]
        const updatedSpans = [...msg.spans]
        // Transition to terminal: carry forward all base fields + steps,
        // then add durationMs, finalResult, reason from the end frame.
        const terminalSpan: SubagentSpanTerminal = {
          spanId: existingSpan.spanId,
          parentCallId: existingSpan.parentCallId,
          taskLabel: existingSpan.taskLabel,
          steps: existingSpan.steps,
          status: frame.status,
          durationMs: frame.duration_ms ?? 0,
          finalResult: frame.final_result,
          // W1-9: propagate reason from backend when status is 'interrupted'
          reason: frame.reason,
        }
        updatedSpans[spanIdx] = terminalSpan
        msgs[i] = { ...msgs[i], spans: updatedSpans }
        return { messages: msgs }
      }
      // W3-6: no matching span found — log a diagnostic so operators can correlate
      // out-of-order subagent_end frames (e.g., end arrived before start on replay).
      console.warn('[chat] subagent_end received for unknown span_id', {
        spanId: frame.span_id,
      })
      return {}
    }),

  // FR-H-010: attach a step (tool_call_start) to an open span
  attachStepToSpan: (parentCallId, step) =>
    set((state) => {
      const msgs = [...state.messages]
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
          // Update existing step (tool_call_result arriving after start)
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
    }),

  pendingApprovals: [],
  addApprovalRequest: (req) =>
    set((state) => ({
      pendingApprovals: [
        ...state.pendingApprovals,
        { ...req, status: 'pending' },
      ],
    })),

  resolveApproval: (id, status) =>
    set((state) => ({
      pendingApprovals: state.pendingApprovals.map((a) =>
        a.id === id ? { ...a, status } : a
      ),
    })),

  sessionTokens: 0,
  sessionCost: 0,
  updateSessionStats: (tokens, cost) => set({ sessionTokens: tokens, sessionCost: cost }),

  rateLimitEvent: null,
  setRateLimitEvent: (event) => {
    // Cancel the previous auto-clear timer if one is pending. Without this,
    // a burst of rate-limit events would leak setTimeouts; each closure
    // holds a reference to the old event object until it fires.
    if (rateLimitClearTimer !== null) {
      clearTimeout(rateLimitClearTimer)
      rateLimitClearTimer = null
    }
    set({ rateLimitEvent: event })
    // Auto-clear after 60s so stale banners don't linger. This is a UX cap
    // independent of `retry_after_seconds` — longer retry windows (hourly
    // LLM limits, daily cost caps) would leave a banner up for impractical
    // durations; the underlying limit remains enforced by the backend.
    rateLimitClearTimer = setTimeout(() => {
      rateLimitClearTimer = null
      set((state) => {
        // Only clear if this is still the same event (avoids clobbering a newer one)
        if (state.rateLimitEvent === event) return { rateLimitEvent: null }
        return {}
      })
    }, 60_000)
  },
  clearRateLimitEvent: () => {
    if (rateLimitClearTimer !== null) {
      clearTimeout(rateLimitClearTimer)
      rateLimitClearTimer = null
    }
    set({ rateLimitEvent: null })
  },

  resetSession: () => {
    // W1-8b: clear module-level orphan buffers and their TTL timers so they
    // don't leak across session switches.
    for (const key of Object.keys(orphanTimers)) {
      clearTimeout(orphanTimers[key])
      delete orphanTimers[key]
    }
    for (const key of Object.keys(pendingByParentCallId)) {
      delete pendingByParentCallId[key]
    }
    // W1-7: reset the diagnostic flag on session reset.
    sawReplayMessageThisTurn = false
    set(CLEAN_SESSION_STATE)
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

    const userMsg: ChatMessage = {
      id: generateId(),
      session_id: activeSessionId ?? '',
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
      status: 'done',
    }

    // Streaming assistant placeholder — created alongside user message
    const assistantMsg: ChatMessage = {
      id: generateId(),
      session_id: activeSessionId ?? '',
      role: 'assistant',
      content: '',
      timestamp: new Date().toISOString(),
      status: 'streaming',
      isStreaming: true,
      streamCursor: true,
    }

    // Optimistic: add both messages + set isStreaming in ONE update to avoid race
    set((state) => ({
      messages: [...state.messages, userMsg, assistantMsg],
      isStreaming: true,
    }))

    const sent = connection.send({
      type: 'message',
      content,
      session_id: activeSessionId ?? undefined,
      // Always send agent_id when available — core and custom agents both route by ID.
      agent_id: activeAgentId ?? undefined,
    })

    if (!sent) {
      // Revert optimistic update — connection dropped between check and send
      set((state) => ({
        messages: state.messages.filter((m) => m.id !== userMsg.id && m.id !== assistantMsg.id),
        isStreaming: false,
      }))
      useConnectionStore.getState().setConnectionError('Message could not be sent — connection dropped. Please try again.')
    }
  },

  cancelStream: () => {
    const { connection } = useConnectionStore.getState()
    const { activeSessionId } = useSessionStore.getState()
    const { isStreaming } = get()

    if (!connection || !isStreaming) return
    if (!activeSessionId) {
      // No session to cancel — still unblock the UI
      set({ isStreaming: false })
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

    // Only mark interrupted when cancel was at least attempted (connection existed)
    get().markLastMessageInterrupted()

    // Cancel any running tool calls
    set((state) => {
      const updated = { ...state.toolCalls }
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
    const store = get()
    switch (frame.type) {
      case 'token':
        store.updateLastAssistantMessage(frame.content, false)
        break

      case 'done':
        store.updateLastAssistantMessage('', true)
        // Stats update is immediate.
        set((state) => {
          if (frame.stats?.tokens == null && frame.stats?.cost == null) return state
          return {
            sessionTokens: state.sessionTokens + (frame.stats?.tokens ?? 0),
            sessionCost: state.sessionCost + (frame.stats?.cost ?? 0),
          }
        })
        // W3-9: if a replay was in flight when this done frame arrived, record the
        // completed session ID so ChatScreen's REST fallback knows replay finished
        // and can skip the overwrite (even if storeMessageCount is 0 for an empty session).
        if (store.isReplaying) {
          const { activeSessionId } = useSessionStore.getState()
          if (activeSessionId) {
            set({ replayCompletedForSession: activeSessionId })
          }
        }
        // FR-I-014: first done after attach_session closes the replay window.
        // Route through setReplaying so the 250ms minimum-display window is honored
        // (avoids flicker + gives E2E the observable disabled-input window).
        // Harmless for live turns (isReplaying was already false).
        store.setReplaying(false)
        break

      case 'error':
        set((state) => {
          const msgs = [...state.messages]
          const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
          if (lastIdx !== -1) {
            // Message-level error: update the assistant bubble inline.
            // Do NOT set connectionError — the inline status is sufficient and avoids
            // conflating a per-message failure with a connection-level outage.
            msgs[lastIdx] = {
              ...msgs[lastIdx],
              content: msgs[lastIdx].content || frame.message,
              isStreaming: false,
              streamCursor: false,
              status: 'error',
            }
            return { messages: msgs, isStreaming: false }
          }
          // No assistant message exists yet — this is a connection-level error.
          // Append an error message AND set connectionError so the banner shows.
          msgs.push({
            id: generateId(),
            role: 'assistant',
            content: frame.message,
            timestamp: new Date().toISOString(),
            status: 'error',
            isStreaming: false,
            streamCursor: false,
          })
          useConnectionStore.getState().setConnectionError(frame.message)
          return { messages: msgs, isStreaming: false }
        })
        break

      case 'tool_call_start': {
        const parentCallId = frame.parent_call_id
        if (parentCallId) {
          // FR-H-009: check if a matching span is already open
          if (hasOpenSpan(store.messages, parentCallId)) {
            // Attach directly to the span (FR-H-010)
            store.attachStepToSpan(parentCallId, {
              id: frame.call_id,
              call_id: frame.call_id,
              tool: frame.tool,
              params: frame.params,
              status: 'running',
            })
          } else {
            // Buffer until subagent_start arrives or TTL expires (FR-H-009)
            bufferForSpan(parentCallId, frame, (buffered) => {
              console.warn(
                `[chat] orphan frame: parent_call_id="${parentCallId}" — subagent_start never arrived within ${ORPHAN_BUFFER_TTL_MS}ms. Releasing as flat tool calls.`
              )
              // W1-8c: give the user a visible signal in addition to the console warn.
              useUiStore.getState().addToast({
                variant: 'default',
                message: 'Some subagent steps arrived without their span — displayed as flat tool calls',
              })
              for (const { frame: bf } of buffered) {
                if (bf.type === 'tool_call_start') {
                  const s = get()
                  const lastMsg = s.messages[s.messages.length - 1]
                  if (!lastMsg || lastMsg.role !== 'assistant') {
                    s.updateLastAssistantMessage('', false)
                  }
                  s.startToolCall(bf.call_id ?? '', bf.tool ?? '', bf.params ?? {})
                } else if (bf.type === 'tool_call_result') {
                  get().resolveToolCall(bf.call_id ?? '', bf.result, bf.status ?? 'success', bf.duration_ms, bf.error)
                }
              }
            })
          }
        } else {
          // Non-nested tool call — original behavior
          const lastMsg = store.messages[store.messages.length - 1]
          if (!lastMsg || lastMsg.role !== 'assistant') {
            store.updateLastAssistantMessage('', false)
          }
          store.startToolCall(frame.call_id ?? '', frame.tool ?? '', frame.params ?? {})
        }
        break
      }

      case 'tool_call_result': {
        const parentCallId = frame.parent_call_id
        if (parentCallId) {
          if (hasOpenSpan(store.messages, parentCallId)) {
            // Update the step in the span
            store.attachStepToSpan(parentCallId, {
              id: frame.call_id,
              call_id: frame.call_id,
              tool: frame.tool,
              params: {},
              result: frame.result,
              status: frame.status ?? 'success',
              duration_ms: frame.duration_ms,
              error: frame.error,
            })
          } else {
            // Buffer until subagent_start arrives.
            // W1-8a: bufferForSpan arms the 10s TTL on first frame for this parent.
            bufferForSpan(parentCallId, frame, (buffered) => {
              console.warn(
                `[chat] orphan frame: parent_call_id="${parentCallId}" — subagent_start never arrived within ${ORPHAN_BUFFER_TTL_MS}ms. Releasing as flat tool calls.`
              )
              useUiStore.getState().addToast({
                variant: 'default',
                message: 'Some subagent steps arrived without their span — displayed as flat tool calls',
              })
              for (const { frame: bf } of buffered) {
                if (bf.type === 'tool_call_start') {
                  const s = get()
                  const lastMsg = s.messages[s.messages.length - 1]
                  if (!lastMsg || lastMsg.role !== 'assistant') {
                    s.updateLastAssistantMessage('', false)
                  }
                  s.startToolCall(bf.call_id ?? '', bf.tool ?? '', bf.params ?? {})
                } else if (bf.type === 'tool_call_result') {
                  get().resolveToolCall(bf.call_id ?? '', bf.result, bf.status ?? 'success', bf.duration_ms, bf.error)
                }
              }
            })
          }
        } else {
          store.resolveToolCall(frame.call_id ?? '', frame.result, frame.status ?? 'success', frame.duration_ms, frame.error)
        }
        break
      }

      case 'subagent_start': {
        const sf = frame as WsSubagentStartFrame
        store.startSpan(sf)
        break
      }

      case 'subagent_end': {
        const ef = frame as WsSubagentEndFrame
        store.endSpan(ef)
        break
      }

      case 'exec_approval_request':
        store.addApprovalRequest(frame)
        break

      case 'task_status_changed':
        queryClient.invalidateQueries({ queryKey: ['tasks'] })
        break

      case 'agent_switched': {
        // Handoff tool switched the active agent — update the session store
        // so the dropdown reflects the new agent and subsequent messages route correctly.
        // frame.agent_name is available for future display use without an extra lookup.
        const newAgentId = frame.agent_id
        if (newAgentId) {
          const sessionStore = useSessionStore.getState()
          sessionStore.setActiveSession(sessionStore.activeSessionId, newAgentId)
        }
        // Invalidate sessions so the panel refreshes agent_ids / active_agent_id
        queryClient.invalidateQueries({ queryKey: ['sessions'] })
        break
      }

      case 'replay_message': {
        // W1-7: mark that a replay_message was processed so setReplaying(false)
        // can warn if it hits the no-op path after replay ran.
        sawReplayMessageThisTurn = true
        const replayFrame = frame as WsReplayMessageFrame
        const role = (replayFrame.role || 'assistant') as 'user' | 'assistant'
        set((state) => ({
          messages: [
            ...state.messages,
            {
              id: generateId(),
              role,
              content: replayFrame.content ?? '',
              timestamp: new Date().toISOString(),
              status: 'done' as const,
            },
          ],
        }))
        break
      }

      case 'media': {
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
        set((state) => {
          const msgs = [...state.messages]
          const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
          if (lastIdx !== -1) {
            msgs[lastIdx] = {
              ...msgs[lastIdx],
              media: [...(msgs[lastIdx].media ?? []), ...attachments],
            }
            return { messages: msgs }
          }
          // No assistant message yet — create a media-only placeholder.
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
        get().setRateLimitEvent({
          scope: rlFrame.scope,
          resource: rlFrame.resource,
          policyRule: rlFrame.policy_rule,
          retryAfterSeconds: rlFrame.retry_after_seconds,
          agentId: rlFrame.agent_id,
          tool: rlFrame.tool,
        })
        break
      }

      // FR-082: tool_approval_required — forward to the tool approval store.
      case 'tool_approval_required': {
        useToolApprovalStore.getState().enqueue(frame as WsToolApprovalRequiredFrame)
        break
      }

      // FR-052, FR-081: session_state — reconcile stale approval modals on reconnect.
      case 'session_state': {
        useToolApprovalStore.getState().reconcileWithSessionState(frame as WsSessionStateFrame)
        break
      }

      // FR-016 (MAJ-009): system_overload — show a non-modal warning toast.
      case 'system_overload': {
        useUiStore.getState().addToast({
          message: 'System at capacity — agent action blocked. Retry shortly.',
          variant: 'warning',
        })
        break
      }

      default:
        // W3-7b: log the actual type string, not [object Object] from the whole frame.
        console.warn('[chat] Unknown frame type', { type: (frame as { type?: string }).type })
        break
    }
  },
}))

// Register resetSession and setReplaying with the session store to break the circular import.
// Both run after useChatStore is fully initialized.
registerChatResetSession(() => useChatStore.getState().resetSession())
registerChatSetReplaying((value) => useChatStore.getState().setReplaying(value))
