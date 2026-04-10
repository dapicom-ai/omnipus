import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore } from '@/store/session'
import { ChatThread } from '@/components/chat/ChatThread'

// test_chat_streaming_integration (test #24)
// test_cancel_integration (test #40)
// Traces to: wave5a-wire-ui-spec.md — Scenario: User sends message and receives streaming response
//             wave5a-wire-ui-spec.md — Scenario: Cancel during streaming preserves partial response

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchSessionMessages: vi.fn().mockResolvedValue([]) }
})

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>
}

beforeEach(() => {
  act(() => {
    useChatStore.setState({
      messages: [],
      isStreaming: false,
      toolCalls: {},
      pendingApprovals: [],
    })
    useConnectionStore.setState({ connection: null, isConnected: false, connectionError: null })
    useSessionStore.setState({ activeSessionId: null, activeAgentId: null })
  })
})

describe('chat streaming integration (test #24) — token rendering', () => {
  it('renders tokens incrementally as handleFrame is called', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: User sends message and receives streaming response
    render(<ChatThread />, { wrapper })

    act(() => {
      useChatStore.getState().appendMessage({
        id: 'user_1',
        session_id: 'sess_test',
        role: 'user',
        content: 'Hello, world!',
        timestamp: new Date().toISOString(),
        status: 'done',
      })
      useChatStore.getState().appendMessage({
        id: 'asst_1',
        session_id: 'sess_test',
        role: 'assistant',
        content: '',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
        streamCursor: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'token', content: 'Hello' })
      useChatStore.getState().handleFrame({ type: 'token', content: ' world' })
    })

    await waitFor(() => {
      expect(screen.getByText(/Hello world/i)).toBeInTheDocument()
    })
  })

  it('clears streaming state and renders final content on done frame', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Streaming response completes with markdown
    render(<ChatThread />, { wrapper })

    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_2',
        session_id: 'sess_test',
        role: 'assistant',
        content: '# Heading',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
        streamCursor: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'done', stats: { tokens: 150, cost: 0.02, duration_ms: 0 } })
    })

    await waitFor(() => {
      expect(useChatStore.getState().isStreaming).toBe(false)
    })
    const msg = useChatStore.getState().messages.find((m) => m.id === 'asst_2')
    expect(msg?.status).toBe('done')
    expect(msg?.streamCursor).toBe(false)
  })

  it('records connectionError on error frame when no assistant message exists', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: WebSocket connection error during streaming
    // When NO assistant message exists, error frames are connection-level and set connectionError.
    render(<ChatThread />, { wrapper })

    act(() => {
      // No assistant message appended — error is connection-level
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'error', message: 'Connection lost' })
    })

    await waitFor(() => {
      expect(useChatStore.getState().isStreaming).toBe(false)
      expect(useConnectionStore.getState().connectionError).toBe('Connection lost')
    })
  })

  it('does NOT set connectionError on error frame when assistant message exists', async () => {
    // Message-level errors update the message inline without setting the global banner
    render(<ChatThread />, { wrapper })

    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_3',
        session_id: 'sess_test',
        role: 'assistant',
        content: '',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'error', message: 'Connection lost' })
    })

    await waitFor(() => {
      expect(useChatStore.getState().isStreaming).toBe(false)
      // Message-level error does NOT set connectionError
      expect(useConnectionStore.getState().connectionError).toBeNull()
      // Instead, the message itself has error status
      const msg = useChatStore.getState().messages.find((m) => m.id === 'asst_3')
      expect(msg?.status).toBe('error')
    })
  })
})

describe('cancel integration (test #40)', () => {
  it('sends cancel frame and marks partial response as interrupted', async () => {
    // Traces to: wave5a-wire-ui-spec.md — test_cancel_integration (test #40)
    const mockSend = vi.fn()
    render(<ChatThread />, { wrapper })

    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_cancel',
        session_id: 'sess_cancel',
        role: 'assistant',
        content: 'Here is the analysis of...',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
        isConnected: true,
      })
      useSessionStore.setState({ activeSessionId: 'sess_cancel' })
      useChatStore.getState().cancelStream()
    })

    // Cancel frame sent
    expect(mockSend).toHaveBeenCalledWith({ type: 'cancel', session_id: 'sess_cancel' })

    // Partial response preserved with interrupted status
    await waitFor(() => {
      const msg = useChatStore.getState().messages.find((m) => m.id === 'asst_cancel')
      expect(msg?.status).toBe('interrupted')
      expect(msg?.content).toBe('Here is the analysis of...')
    })
  })
})
