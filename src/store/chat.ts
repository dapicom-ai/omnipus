import { create } from 'zustand'
import { generateId } from '@/lib/constants'
import { useUiStore } from '@/store/ui'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore, registerChatResetSession } from '@/store/session'
import { queryClient } from '@/lib/queryClient'
import type { Message, ToolCall } from '@/lib/api'
import type { WsReceiveFrame, WsExecApprovalRequestFrame, WsReplayMessageFrame, WsRateLimitFrame } from '@/lib/ws'

export interface MediaAttachment {
  type: 'image' | 'audio' | 'video' | 'file'
  url: string
  filename: string
  contentType: string
  caption?: string
}

export interface ChatMessage extends Message {
  isStreaming?: boolean
  streamCursor?: boolean
  media?: MediaAttachment[]
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
  rateLimitEvent: null as RateLimitEventData | null,
} as const

export const useChatStore = create<ChatStore>((set, get) => ({
  messages: [],
  isStreaming: false,
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
      if (!state.toolCalls[callId]) return state
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

  resetSession: () => set(CLEAN_SESSION_STATE),

  sendMessage: (content) => {
    const { connection, isConnected } = useConnectionStore.getState()
    const { activeSessionId, activeAgentId, activeAgentType } = useSessionStore.getState()
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
      // Don't send agent_id for the system agent — it handles routing itself.
      // Use activeAgentType (set by setActiveSession) rather than matching a hardcoded ID string,
      // so this stays correct even if the system agent's ID ever changes.
      agent_id: activeAgentId && activeAgentType !== 'system' ? activeAgentId : undefined,
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
        if (frame.stats?.tokens != null || frame.stats?.cost != null) {
          set((state) => ({
            sessionTokens: state.sessionTokens + (frame.stats?.tokens ?? 0),
            sessionCost: state.sessionCost + (frame.stats?.cost ?? 0),
          }))
        }
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
        // During replay, tool_call frames may arrive before any assistant text.
        // Ensure an assistant message exists so tool calls have a parent to render against.
        const lastMsg = store.messages[store.messages.length - 1]
        if (!lastMsg || lastMsg.role !== 'assistant') {
          store.updateLastAssistantMessage('', false)
        }
        store.startToolCall(frame.call_id ?? '', frame.tool ?? '', frame.params ?? {})
        break
      }

      case 'tool_call_result':
        store.resolveToolCall(frame.call_id ?? '', frame.result, frame.status ?? 'success', frame.duration_ms, frame.error)
        break

      case 'exec_approval_request':
        store.addApprovalRequest(frame)
        break

      case 'task_status_changed':
        queryClient.invalidateQueries({ queryKey: ['tasks'] })
        break

      case 'replay_message': {
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

      default:
        console.warn('[chat] Unknown frame type:', (frame as { type: string }).type)
        break
    }
  },
}))

// Register resetSession with the session store to break the circular import.
// This runs after useChatStore is fully initialized.
registerChatResetSession(() => useChatStore.getState().resetSession())
