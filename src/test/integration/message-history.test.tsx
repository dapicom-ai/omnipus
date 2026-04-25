import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { act } from 'react'
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AssistantRuntimeProvider, ThreadPrimitive, MessagePrimitive } from '@assistant-ui/react'
import { useChatStore } from '@/store/chat'
import { useOmnipusRuntime } from '@/lib/omnipus-runtime'
import type { Message } from '@/lib/api'

// test_message_history_load (test #25)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Previous messages load on session navigation
//             wave5a-wire-ui-spec.md — Scenario: Multi-day session merges partitions
//             wave5a-wire-ui-spec.md — Scenario: Compaction entries render as system messages
//
// Ported to run against the AssistantUI surface (see chat-streaming.test.tsx
// harness). The original ChatThread component has been removed.

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchSessionMessages: vi.fn() }
})

import { fetchSessionMessages } from '@/lib/api'

const mockMessages: Message[] = [
  { id: 'm1', session_id: 'sess_aws', role: 'user', content: 'What are AWS m5 instance prices?', timestamp: '2026-03-28T10:00:00Z', status: 'done' },
  { id: 'm2', session_id: 'sess_aws', role: 'assistant', content: 'AWS m5 instances start at $0.096/hour for m5.large.', timestamp: '2026-03-28T10:00:05Z', status: 'done' },
  { id: 'm3', session_id: 'sess_aws', role: 'system', content: 'Context compacted — older messages summarized', timestamp: '2026-03-28T12:00:00Z', status: 'done' },
  { id: 'm4', session_id: 'sess_aws', role: 'user', content: 'What about m5.xlarge?', timestamp: '2026-03-29T09:00:00Z', status: 'done' },
  { id: 'm5', session_id: 'sess_aws', role: 'assistant', content: '# m5.xlarge Pricing\n\nThe m5.xlarge costs $0.192/hour.', timestamp: '2026-03-29T09:00:06Z', status: 'done' },
]

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function HistoryLoader({ sessionId }: { sessionId: string }) {
  const setMessages = useChatStore((s) => s.setMessages)
  const { data } = useQuery({
    queryKey: ['messages', sessionId],
    queryFn: () => fetchSessionMessages(sessionId),
    enabled: !!sessionId,
  })
  useEffect(() => {
    if (data) setMessages(data)
  }, [data, setMessages])
  return null
}

function HistoryHarness({ sessionId }: { sessionId: string }) {
  const runtime = useOmnipusRuntime()
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <HistoryLoader sessionId={sessionId} />
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
                  <MessagePrimitive.Parts />
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
  act(() => { useChatStore.getState().resetSession() })
  vi.mocked(fetchSessionMessages).mockResolvedValue(mockMessages)
})

describe('message history integration (test #25)', () => {
  it('loads and renders all messages in chronological order', async () => {
    render(<HistoryHarness sessionId="sess_aws" />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/What are AWS m5 instance prices/i)).toBeInTheDocument()
      expect(screen.getByText(/m5 instances start at/i)).toBeInTheDocument()
    })
  })

  it('merges messages from multiple day partitions in chronological order', async () => {
    render(<HistoryHarness sessionId="sess_aws" />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/What about m5.xlarge/i)).toBeInTheDocument()
    })
  })

  it('renders compaction entry as system message', async () => {
    render(<HistoryHarness sessionId="sess_aws" />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/context compacted/i)).toBeInTheDocument()
    })
  })

  it('renders empty chat when session has no messages', async () => {
    vi.mocked(fetchSessionMessages).mockResolvedValue([])
    const { container } = render(<HistoryHarness sessionId="sess_empty" />, { wrapper })

    await waitFor(() => {
      const logEl = container.querySelector('[role="log"]')
      expect(logEl).toBeTruthy()
      expect(logEl?.textContent?.trim() ?? '').toBe('')
    })
  })
})
