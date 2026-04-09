import { create } from 'zustand'
import { generateId } from '@/lib/constants'
import { useUiStore } from '@/store/ui'
import { queryClient } from '@/lib/queryClient'
import type { Message, ToolCall, Session } from '@/lib/api'
import type { WsConnection, WsReceiveFrame, WsExecApprovalRequestFrame, WsReplayMessageFrame } from '@/lib/ws'

export interface ChatMessage extends Message {
  isStreaming?: boolean
  streamCursor?: boolean
}

export interface ExecApprovalRequest {
  id: string
  command: string
  working_dir?: string
  matched_policy?: string
  status: 'pending' | 'allowed' | 'denied' | 'always_allowed'
}

interface ChatStore {
  // Connection
  connection: WsConnection | null
  isConnected: boolean
  connectionError: string | null
  setConnection: (conn: WsConnection | null) => void
  setConnected: (connected: boolean) => void
  setConnectionError: (error: string | null) => void

  // Session & agent selection
  activeSessionId: string | null
  activeAgentId: string | null
  /** The type of the currently active agent ('system' | 'core' | 'custom' | null).
   *  Set by setActiveSession so all callers stay in sync without manual tracking. */
  activeAgentType: 'system' | 'core' | 'custom' | null
  setActiveSession: (sessionId: string | null, agentId?: string | null, agentType?: 'system' | 'core' | 'custom' | null) => void

  // Attached session context — tracks when viewing a task/channel session
  attachedSessionType: 'chat' | 'task' | 'channel' | null
  attachedTaskTitle: string | null
  attachToSession: (sessionId: string, type: Session['type'], title?: string, agentId?: string) => void

  // Messages
  messages: ChatMessage[]
  isStreaming: boolean
  setMessages: (messages: Message[]) => void
  appendMessage: (message: ChatMessage) => void
  updateLastAssistantMessage: (content: string, done?: boolean) => void
  markLastMessageInterrupted: () => void

  // Tool calls (keyed by call_id) + insertion order for interleaved rendering
  // TODO: standardize call_id/id naming at deserialization boundary
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

  // Actions
  sendMessage: (content: string) => void
  cancelStream: () => void
  reconnect: () => void
  respondToApproval: (id: string, decision: 'allow' | 'deny' | 'always') => void
  respondToPairing: (deviceId: string, decision: 'approve' | 'reject') => void

  // Inbound frame handler
  handleFrame: (frame: WsReceiveFrame) => void
}

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
} as const

export const useChatStore = create<ChatStore>((set, get) => ({
  connection: null,
  isConnected: false,
  connectionError: null,
  setConnection: (conn) => set({ connection: conn }),
  setConnected: (connected) => set({ isConnected: connected, connectionError: connected ? null : get().connectionError }),
  setConnectionError: (error) => set({ connectionError: error }),

  activeSessionId: null,
  activeAgentId: null,
  activeAgentType: null,
  setActiveSession: (sessionId, agentId, agentType) =>
    set({
      activeSessionId: sessionId,
      activeAgentId: agentId ?? get().activeAgentId,
      activeAgentType: agentType ?? get().activeAgentType,
      ...CLEAN_SESSION_STATE,
      attachedSessionType: null,
      attachedTaskTitle: null,
    }),

  attachedSessionType: null,
  attachedTaskTitle: null,
  attachToSession: (sessionId, type, title, agentId) => {
    const { connection } = get()
    set({
      activeSessionId: sessionId,
      attachedSessionType: type,
      attachedTaskTitle: title ?? null,
      ...(agentId ? { activeAgentId: agentId } : {}),
      ...CLEAN_SESSION_STATE,
    })
    if (connection) {
      connection.send({ type: 'attach_session', session_id: sessionId })
    } else {
      console.warn('[chat] attachToSession: no connection — attach_session not sent')
    }
  },

  messages: [],
  isStreaming: false,
  setMessages: (messages) => set({ messages, toolCalls: {}, toolCallOrder: [], textAtToolCallStart: {}, pendingApprovals: [], sessionTokens: 0, sessionCost: 0 }),

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

  sendMessage: (content) => {
    const { connection, activeSessionId, activeAgentId, activeAgentType, isStreaming } = get()
    if (isStreaming) {
      set({ connectionError: 'Please wait — a response is still generating.' })
      return
    }
    if (!connection) {
      set({ connectionError: 'Cannot send message — not connected to the server. Check your connection and try again.' })
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
        connectionError: 'Message could not be sent — connection dropped. Please try again.',
      }))
    }
  },

  cancelStream: () => {
    const { connection, activeSessionId, isStreaming } = get()
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

  reconnect: () => {
    const { connection } = get()
    if (!connection) {
      set({ connectionError: 'Cannot reconnect — please refresh the page.' })
      return
    }
    set({ connectionError: null })
    connection.connect()
  },

  respondToApproval: (id, decision) => {
    const { connection } = get()
    if (!connection) {
      set({ connectionError: 'Cannot respond to approval — not connected. Reconnect and try again.' })
      return
    }

    const sent = connection.send({ type: 'exec_approval_response', id, decision })
    if (!sent) {
      set({ connectionError: 'Failed to send approval response — connection dropped. Reconnect and try again.' })
      return
    }
    const statusMap = { allow: 'allowed', deny: 'denied', always: 'always_allowed' } as const
    get().resolveApproval(id, statusMap[decision])
  },

  respondToPairing: (deviceId, decision) => {
    const { connection } = get()
    if (!connection) {
      set({ connectionError: 'Cannot respond to pairing — not connected. Reconnect and try again.' })
      return
    }

    const sent = connection.send({ type: 'device_pairing_response', device_id: deviceId, decision })
    if (!sent) {
      set({ connectionError: 'Failed to send pairing response — connection dropped. Reconnect and try again.' })
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
          return { messages: msgs, isStreaming: false, connectionError: frame.message }
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

      default:
        console.warn('[chat] Unknown frame type:', (frame as { type: string }).type)
        break
    }
  },
}))
