import { create } from 'zustand'
import type { Message, ToolCall } from '@/lib/api'
import type { WsConnection, WsReceiveFrame } from '@/lib/ws'
import type {
  WsExecApprovalRequestFrame,
} from '@/lib/ws'

export interface ChatMessage extends Message {
  isStreaming?: boolean
  streamCursor?: boolean
}

export interface ExecApprovalRequest {
  id: string
  command: string
  working_dir?: string
  matched_policy?: string
  status?: 'pending' | 'allowed' | 'denied' | 'always_allowed'
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
  setActiveSession: (sessionId: string | null, agentId?: string | null) => void

  // Messages
  messages: ChatMessage[]
  isStreaming: boolean
  setMessages: (messages: Message[]) => void
  appendMessage: (message: ChatMessage) => void
  updateLastAssistantMessage: (content: string, done?: boolean) => void
  markLastMessageInterrupted: () => void

  // Tool calls (keyed by call_id)
  toolCalls: Record<string, ToolCall & { call_id: string }>
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
  respondToApproval: (id: string, decision: 'allow' | 'deny' | 'always') => void

  // Compat aliases used by pre-written tests
  addMessage: (message: ChatMessage) => void
  startStreaming: (messageId: string) => void
  cancelStreaming: () => void
  streamingMessageId: string | null

  // Inbound frame handler
  handleFrame: (frame: WsReceiveFrame) => void
}

export const useChatStore = create<ChatStore>((set, get) => ({
  connection: null,
  isConnected: false,
  connectionError: null,
  setConnection: (conn) => set({ connection: conn }),
  setConnected: (connected) => set({ isConnected: connected, connectionError: connected ? null : get().connectionError }),
  setConnectionError: (error) => set({ connectionError: error }),

  activeSessionId: null,
  activeAgentId: null,
  setActiveSession: (sessionId, agentId) =>
    set({ activeSessionId: sessionId, activeAgentId: agentId ?? get().activeAgentId }),

  messages: [],
  isStreaming: false,
  setMessages: (messages) => set({ messages, toolCalls: {}, pendingApprovals: [], sessionTokens: 0, sessionCost: 0 }),

  appendMessage: (message) =>
    set((state) => ({ messages: [...state.messages, message] })),

  updateLastAssistantMessage: (content, done = false) =>
    set((state) => {
      const msgs = [...state.messages]
      let lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
      if (lastIdx === -1) {
        // Token arrived before placeholder — create it now
        const placeholder: ChatMessage = {
          id: `assistant-${Date.now()}`,
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
  startToolCall: (callId, tool, params) =>
    set((state) => ({
      toolCalls: {
        ...state.toolCalls,
        [callId]: { id: callId, call_id: callId, tool, params, status: 'running' },
      },
    })),

  resolveToolCall: (callId, result, status, durationMs, error) =>
    set((state) => ({
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
    })),

  cancelToolCall: (callId) =>
    set((state) => ({
      toolCalls: {
        ...state.toolCalls,
        [callId]: { ...state.toolCalls[callId], status: 'cancelled' },
      },
    })),

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
    const { connection, activeSessionId, activeAgentId, isStreaming } = get()
    if (!connection || isStreaming) return

    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      session_id: activeSessionId ?? '',
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
      status: 'done',
    }

    // Optimistic: add user message immediately
    set((state) => ({
      messages: [...state.messages, userMsg],
      isStreaming: true,
    }))

    // Add streaming assistant placeholder
    const assistantMsg: ChatMessage = {
      id: `assistant-${Date.now()}`,
      session_id: activeSessionId ?? '',
      role: 'assistant',
      content: '',
      timestamp: new Date().toISOString(),
      status: 'streaming',
      isStreaming: true,
      streamCursor: true,
    }
    set((state) => ({ messages: [...state.messages, assistantMsg] }))

    const sent = connection.send({
      type: 'message',
      content,
      session_id: activeSessionId ?? undefined,
      agent_id: activeAgentId ?? undefined,
    })

    if (!sent) {
      // Revert optimistic update — connection dropped between check and send
      set((state) => ({
        messages: state.messages.filter((m) => m.id !== userMsg.id && m.id !== assistantMsg.id),
        isStreaming: false,
      }))
    }
  },

  cancelStream: () => {
    const { connection, activeSessionId, isStreaming } = get()
    if (!connection || !isStreaming) return

    connection.send({ type: 'cancel', session_id: activeSessionId ?? '' })
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
    const { connection } = get()
    if (!connection) return

    connection.send({ type: 'exec_approval_response', id, decision })
    const statusMap = { allow: 'allowed', deny: 'denied', always: 'always_allowed' } as const
    get().resolveApproval(id, statusMap[decision])
  },

  // Compat aliases
  streamingMessageId: null,
  addMessage: (message) => get().appendMessage(message),
  startStreaming: (messageId) => {
    set({ isStreaming: true, streamingMessageId: messageId })
  },
  cancelStreaming: () => get().cancelStream(),

  handleFrame: (frame) => {
    const store = get()
    switch (frame.type) {
      case 'token':
        store.updateLastAssistantMessage(frame.content, false)
        break

      case 'done':
        store.updateLastAssistantMessage('', true)
        if (frame.stats.tokens != null || frame.stats.cost != null) {
          set((state) => ({
            sessionTokens: state.sessionTokens + (frame.stats.tokens ?? 0),
            sessionCost: state.sessionCost + (frame.stats.cost ?? 0),
          }))
        }
        break

      case 'error':
        set((state) => {
          const msgs = [...state.messages]
          const lastIdx = msgs.map((m) => m.role).lastIndexOf('assistant')
          if (lastIdx !== -1) {
            msgs[lastIdx] = {
              ...msgs[lastIdx],
              content: msgs[lastIdx].content || frame.message,
              isStreaming: false,
              streamCursor: false,
              status: 'error',
            }
          }
          return { messages: msgs, isStreaming: false, connectionError: frame.message }
        })
        break

      case 'tool_call_start':
        store.startToolCall(frame.call_id, frame.tool, frame.params)
        break

      case 'tool_call_result':
        store.resolveToolCall(frame.call_id, frame.result, frame.status, frame.duration_ms, frame.error)
        break

      case 'exec_approval_request':
        store.addApprovalRequest(frame)
        break
    }
  },
}))
