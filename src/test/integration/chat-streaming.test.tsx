import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AssistantRuntimeProvider, ThreadPrimitive, MessagePrimitive } from '@assistant-ui/react'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore } from '@/store/session'
import { useOmnipusRuntime } from '@/lib/omnipus-runtime'
import { SubagentBlock } from '@/components/chat/SubagentBlock'

// test_chat_streaming_integration (test #24)
// test_cancel_integration (test #40)
// Traces to: wave5a-wire-ui-spec.md — Scenario: User sends message and receives streaming response
//             wave5a-wire-ui-spec.md — Scenario: Cancel during streaming preserves partial response
//
// These tests run against the real AssistantUI surface via useOmnipusRuntime,
// mirroring what ChatScreen mounts in production. A minimal harness is used
// instead of ChatScreen because ChatScreen requires a TanStack Router context
// (irrelevant to the behaviours under test).

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchSessionMessages: vi.fn().mockResolvedValue([]) }
})

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function SubagentSpanPart(props: { data: { spanId: string } }) {
  const messages = useChatStore((s) => s.messages)
  const span = messages
    .flatMap((m) => (m.role === 'assistant' ? m.spans ?? [] : []))
    .find((s) => s.spanId === props.data.spanId)
  if (!span) return null
  return <SubagentBlock span={span} />
}

function ChatHarness() {
  const runtime = useOmnipusRuntime()
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ThreadPrimitive.Root>
        <div role="log" aria-label="Chat messages">
          <ThreadPrimitive.Messages
            components={{
              UserMessage: () => (
                <MessagePrimitive.Root>
                  <MessagePrimitive.Parts />
                </MessagePrimitive.Root>
              ),
              AssistantMessage: () => (
                <MessagePrimitive.Root>
                  <MessagePrimitive.Parts
                    components={{
                      data: {
                        // eslint-disable-next-line @typescript-eslint/no-explicit-any
                        by_name: { 'subagent-span': SubagentSpanPart as any },
                      },
                    }}
                  />
                </MessagePrimitive.Root>
              ),
              SystemMessage: () => (
                <MessagePrimitive.Root>
                  <MessagePrimitive.Parts />
                </MessagePrimitive.Root>
              ),
            }}
          />
        </div>
      </ThreadPrimitive.Root>
    </AssistantRuntimeProvider>
  )
}

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>
}

beforeEach(() => {
  act(() => {
    useChatStore.getState().resetSession()
    useConnectionStore.setState({ connection: null, isConnected: false, connectionError: null })
    useSessionStore.setState({ activeSessionId: null, activeAgentId: null })
  })
})

describe('chat streaming integration (test #24) — token rendering', () => {
  it('renders tokens incrementally as handleFrame is called', async () => {
    render(<ChatHarness />, { wrapper })

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
    render(<ChatHarness />, { wrapper })

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
    render(<ChatHarness />, { wrapper })

    act(() => {
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'error', message: 'Connection lost' })
    })

    await waitFor(() => {
      expect(useChatStore.getState().isStreaming).toBe(false)
      expect(useConnectionStore.getState().connectionError).toBe('Connection lost')
    })
  })

  it('does NOT set connectionError on error frame when assistant message exists', async () => {
    render(<ChatHarness />, { wrapper })

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
      expect(useConnectionStore.getState().connectionError).toBeNull()
      const msg = useChatStore.getState().messages.find((m) => m.id === 'asst_3')
      expect(msg?.status).toBe('error')
    })
  })
})

describe('cancel integration (test #40)', () => {
  it('sends cancel frame and marks partial response as interrupted', async () => {
    const mockSend = vi.fn()
    render(<ChatHarness />, { wrapper })

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
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
        } as any,
        isConnected: true,
      })
      useSessionStore.setState({ activeSessionId: 'sess_cancel' })
      useChatStore.getState().cancelStream()
    })

    expect(mockSend).toHaveBeenCalledWith({ type: 'cancel', session_id: 'sess_cancel' })

    await waitFor(() => {
      const msg = useChatStore.getState().messages.find((m) => m.id === 'asst_cancel')
      expect(msg?.status).toBe('interrupted')
      expect(msg?.content).toBe('Here is the analysis of...')
    })
  })
})

describe('subagent inline ordering — regression for "span at bottom" bug', () => {
  it('renders SubagentBlock between text runs when the span fires mid-stream', async () => {
    render(<ChatHarness />, { wrapper })

    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_span',
        session_id: 'sess_test',
        role: 'assistant',
        content: '',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      // Stream text, then open a span mid-stream, then stream more text.
      useChatStore.getState().handleFrame({ type: 'token', content: 'Let me research ' })
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_inline',
        parent_call_id: 'p_inline',
        task_label: 'research request',
      })
      useChatStore.getState().handleFrame({ type: 'token', content: 'and then summarise.' })
    })

    // Both text segments present.
    await waitFor(() => {
      expect(screen.getByText(/Let me research/i)).toBeInTheDocument()
      expect(screen.getByText(/and then summarise/i)).toBeInTheDocument()
    })

    // The subagent block must sit between them in document order —
    // that's the whole point of this regression test.
    const research = screen.getByText(/Let me research/i)
    const summary = screen.getByText(/and then summarise/i)
    const subagentHeader = screen.getByTestId('subagent-collapsed')
    const order = [research, subagentHeader, summary]
    // compareDocumentPosition: a.compareDocumentPosition(b) & FOLLOWING returns non-zero when b follows a.
    expect(order[0].compareDocumentPosition(order[1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(order[1].compareDocumentPosition(order[2]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})
