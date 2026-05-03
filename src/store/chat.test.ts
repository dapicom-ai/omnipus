import { describe, it, expect, beforeEach, vi } from 'vitest'
import { act } from 'react'
import { useChatStore } from './chat'
import { useConnectionStore } from './connection'
import { useSessionStore } from './session'

// test_chat_store (test #22)
// Traces to: wave5a-wire-ui-spec.md — Scenario: User sends message and receives streaming response
//             wave5a-wire-ui-spec.md — Scenario: Cancel during streaming preserves partial response

const TEST_SESSION_ID = 'test-session-1'

function resetStore() {
  act(() => {
    useChatStore.setState({
      sessionsById: {},
      messages: [],
      isStreaming: false,
      toolCalls: {},
      toolCallOrder: [],
      textAtToolCallStart: {},
      pendingApprovals: [],
      sessionTokens: 0,
      sessionCost: 0,
      isReplaying: false,
      replayCompletedForSession: null,
      rateLimitEvent: null,
    })
    useConnectionStore.setState({
      connection: null,
      isConnected: false,
      connectionError: null,
    })
    useSessionStore.setState({
      activeSessionId: TEST_SESSION_ID,
      activeAgentId: null,
      activeAgentType: null,
    })
  })
}

beforeEach(resetStore)

describe('chat store — initial state', () => {
  it('initializes with empty messages, not streaming; activeSessionId set by beforeEach', () => {
    const chatState = useChatStore.getState()
    const sessionState = useSessionStore.getState()
    expect(chatState.messages).toEqual([])
    expect(chatState.isStreaming).toBe(false)
    // beforeEach sets activeSessionId to TEST_SESSION_ID so per-session actions have a target.
    expect(sessionState.activeSessionId).toBe(TEST_SESSION_ID)
    expect(sessionState.activeAgentId).toBeNull()
  })
})

describe('chat store — session management', () => {
  it('setActiveSession updates activeSessionId and activeAgentId without wiping buckets', () => {
    // Seed a message in the first session bucket.
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'msg_original',
        session_id: TEST_SESSION_ID,
        role: 'user',
        content: 'Original session message',
        timestamp: '2026-03-29T10:00:00Z',
        status: 'done',
      })
    })
    // Switch to a different session — the original bucket must survive.
    act(() => {
      useSessionStore.getState().setActiveSession('sess_abc', 'general-assistant')
    })
    const sessionState = useSessionStore.getState()
    expect(sessionState.activeSessionId).toBe('sess_abc')
    expect(sessionState.activeAgentId).toBe('general-assistant')
    // Original bucket is intact (not wiped by session switch).
    const bucket = useChatStore.getState().sessionsById[TEST_SESSION_ID]
    expect(bucket?.messages).toHaveLength(1)
    expect(bucket?.messages[0].content).toBe('Original session message')
  })
})

describe('chat store — message handling', () => {
  it('appendMessage adds a user message to the thread', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: User message appears optimistically
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'user_1',
        session_id: 'sess_1',
        role: 'user',
        content: 'Hello, world!',
        timestamp: '2026-03-29T10:00:00Z',
        status: 'done',
      })
    })
    const { messages } = useChatStore.getState()
    expect(messages).toHaveLength(1)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('Hello, world!')
  })

  it('setMessages replaces all messages and resets tool calls', () => {
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'old_1',
        session_id: 'sess_1',
        role: 'user',
        content: 'Old message',
        timestamp: '2026-03-29T09:00:00Z',
        status: 'done',
      })
      useChatStore.getState().setMessages([
        {
          id: 'new_1',
          session_id: 'sess_2',
          role: 'user',
          content: 'New message',
          timestamp: '2026-03-29T10:00:00Z',
          status: 'done',
        },
      ])
    })
    const { messages, toolCalls, sessionTokens } = useChatStore.getState()
    expect(messages).toHaveLength(1)
    expect(messages[0].content).toBe('New message')
    expect(Object.keys(toolCalls)).toHaveLength(0)
    expect(sessionTokens).toBe(0)
  })
})

describe('chat store — streaming via handleFrame', () => {
  it('handleFrame(token) appends content to the last assistant message', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Streaming response — tokens append
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_1',
        session_id: 'sess_1',
        role: 'assistant',
        content: '',
        timestamp: '2026-03-29T10:00:01Z',
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'token', content: 'Hello', session_id: TEST_SESSION_ID })
      useChatStore.getState().handleFrame({ type: 'token', content: ' world', session_id: TEST_SESSION_ID })
    })
    const { messages } = useChatStore.getState()
    const asst = messages.find((m) => m.id === 'asst_1')
    expect(asst?.content).toBe('Hello world')
    expect(asst?.isStreaming).toBe(true)
  })

  it('handleFrame(done) marks last assistant message as done and sets isStreaming false', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Streaming completes with markdown rendering
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_2',
        session_id: 'sess_1',
        role: 'assistant',
        content: '# Heading\nParagraph',
        timestamp: '2026-03-29T10:00:01Z',
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'done', stats: { tokens: 150, cost: 0.02, duration_ms: 0 }, session_id: TEST_SESSION_ID })
    })
    const state = useChatStore.getState()
    expect(state.isStreaming).toBe(false)
    const asst = state.messages.find((m) => m.id === 'asst_2')
    expect(asst?.status).toBe('done')
    expect(asst?.isStreaming).toBe(false)
    expect(state.sessionTokens).toBe(150)
    expect(state.sessionCost).toBeCloseTo(0.02)
  })

  it('handleFrame(error) sets message to error status — message-level error does NOT set connectionError', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: WebSocket connection error during streaming
    // When an assistant message already exists, the error is message-level (e.g. LLM rejected
    // the request). The inline bubble is updated; connectionError is NOT set to avoid falsely
    // showing a connection-down banner when the connection is fine.
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_3',
        session_id: 'sess_1',
        role: 'assistant',
        content: '',
        timestamp: '2026-03-29T10:00:01Z',
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({ connectionError: null })
      useChatStore.getState().handleFrame({ type: 'error', message: 'LLM quota exceeded' })
    })
    const chatState = useChatStore.getState()
    expect(chatState.isStreaming).toBe(false)
    const asst = chatState.messages.find((m) => m.id === 'asst_3')
    expect(asst?.status).toBe('error')
    // Message-level error must NOT propagate to the connection error banner
    expect(useConnectionStore.getState().connectionError).toBeNull()
  })

  it('handleFrame(error) with no assistant message sets connectionError banner', () => {
    // When no assistant message exists, the error is connection-level (e.g. the WS frame
    // arrived before the server could even start a reply). Both the error message AND
    // connectionError are set so the banner shows.
    act(() => {
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().handleFrame({ type: 'error', message: 'Connection lost' })
    })
    const chatState = useChatStore.getState()
    expect(chatState.isStreaming).toBe(false)
    expect(useConnectionStore.getState().connectionError).toBe('Connection lost')
    const errMsg = chatState.messages.find((m) => m.status === 'error')
    expect(errMsg).toBeDefined()
  })
})

describe('chat store — tool calls', () => {
  it('startToolCall registers a running tool call', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Running tool call shows spinner
    act(() => {
      useChatStore.getState().startToolCall('tc_1', 'web_search', { query: 'AWS pricing' })
    })
    const { toolCalls } = useChatStore.getState()
    expect(toolCalls['tc_1']).toMatchObject({
      call_id: 'tc_1',
      tool: 'web_search',
      status: 'running',
    })
  })

  it('resolveToolCall updates status to success with result', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Successful tool call collapses by default
    act(() => {
      useChatStore.getState().startToolCall('tc_2', 'exec', { command: 'ls' })
      useChatStore.getState().resolveToolCall('tc_2', { exit_code: 0 }, 'success', 250)
    })
    const { toolCalls } = useChatStore.getState()
    expect(toolCalls['tc_2'].status).toBe('success')
    expect(toolCalls['tc_2'].duration_ms).toBe(250)
  })

  it('resolveToolCall updates status to error with error message', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Failed tool call shows error with retry
    act(() => {
      useChatStore.getState().startToolCall('tc_3', 'exec', { command: 'ls' })
      useChatStore.getState().resolveToolCall('tc_3', null, 'error', 30000, 'Timeout after 30s')
    })
    const { toolCalls } = useChatStore.getState()
    expect(toolCalls['tc_3'].status).toBe('error')
    expect(toolCalls['tc_3'].error).toBe('Timeout after 30s')
  })

  it('cancelToolCall sets tool status to cancelled', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during tool execution
    act(() => {
      useChatStore.getState().startToolCall('tc_4', 'web_search', {})
      useChatStore.getState().cancelToolCall('tc_4')
    })
    expect(useChatStore.getState().toolCalls['tc_4'].status).toBe('cancelled')
  })
})

describe('chat store — exec approval', () => {
  it('addApprovalRequest queues a pending approval', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Approval block renders with command details
    act(() => {
      useChatStore.getState().addApprovalRequest({
        type: 'exec_approval_request',
        id: 'appr_1',
        command: 'git pull origin main',
        working_dir: '~/projects/omnipus',
        matched_policy: 'tools.exec.approval=ask',
        session_id: TEST_SESSION_ID,
      })
    })
    const { pendingApprovals } = useChatStore.getState()
    expect(pendingApprovals).toHaveLength(1)
    expect(pendingApprovals[0].command).toBe('git pull origin main')
    expect(pendingApprovals[0].status).toBe('pending')
  })

  it('resolveApproval updates approval status to allowed', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval prompt
    act(() => {
      useChatStore.getState().addApprovalRequest({
        type: 'exec_approval_request',
        id: 'appr_1',
        command: 'git pull origin main',
        session_id: TEST_SESSION_ID,
      })
      useChatStore.getState().resolveApproval('appr_1', 'allowed')
    })
    const { pendingApprovals } = useChatStore.getState()
    expect(pendingApprovals[0].status).toBe('allowed')
  })
})

describe('chat store — cancel/interrupt (test_cancel_preserves_partial)', () => {
  it('markLastMessageInterrupted sets status to interrupted and clears streaming', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during streaming preserves partial response (AC1)
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_cancel',
        session_id: 'sess_1',
        role: 'assistant',
        content: 'Here is the analysis of...',
        timestamp: '2026-03-29T10:00:01Z',
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useChatStore.getState().markLastMessageInterrupted()
    })
    const state = useChatStore.getState()
    expect(state.isStreaming).toBe(false)
    const msg = state.messages.find((m) => m.id === 'asst_cancel')
    expect(msg?.status).toBe('interrupted')
    expect(msg?.content).toBe('Here is the analysis of...')
  })

  it('cancelStream calls connection.send with cancel frame', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during streaming (AC1 — WebSocket frame sent)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.getState().appendMessage({
        id: 'asst_5',
        session_id: TEST_SESSION_ID,
        role: 'assistant',
        content: 'Partial...',
        timestamp: '2026-03-29T10:00:01Z',
        status: 'streaming',
        isStreaming: true,
      })
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
      // activeSessionId is already TEST_SESSION_ID from resetStore
      useChatStore.getState().cancelStream()
    })
    expect(mockSend).toHaveBeenCalledWith({ type: 'cancel', session_id: TEST_SESSION_ID })
    expect(useChatStore.getState().isStreaming).toBe(false)
  })

  it('cancelStream is a no-op when not streaming', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel when idle is a no-op (AC3)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
      useChatStore.getState().cancelStream()
    })
    expect(mockSend).not.toHaveBeenCalled()
  })
})

describe('chat store — sendMessage optimistic render', () => {
  it('sendMessage appends user message immediately and sets isStreaming', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: User message appears optimistically
    // mockSend must return true — sendMessage reverts the optimistic update if send() returns falsy
    const mockSend = vi.fn().mockReturnValue(true)
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
      useSessionStore.setState({
        activeSessionId: TEST_SESSION_ID,
        activeAgentId: 'general-assistant',
      })
      useChatStore.getState().sendMessage('Hello, world!')
    })
    const state = useChatStore.getState()
    // User message appended immediately
    const userMsg = state.messages.find((m) => m.role === 'user')
    expect(userMsg?.content).toBe('Hello, world!')
    // isStreaming set to true
    expect(state.isStreaming).toBe(true)
    // WS frame sent
    expect(mockSend).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'message', content: 'Hello, world!' })
    )
  })
})

// ── Sprint H: subagent span tests ─────────────────────────────────────────────
// TDD row 11: ChatStore_GroupsFramesBySpan
// Traces to: sprint-h-subagent-block-spec.md Scenarios 2, 4, 5, 8

describe('ChatStore_GroupsFramesBySpan', () => {
  /** Seed an assistant placeholder so spans have a message to attach to. */
  function seedAssistant() {
    act(() => {
      useChatStore.getState().updateLastAssistantMessage('', false)
    })
  }

  it('in-order: subagent_start → tool_call_start → tool_call_result → subagent_end populates span', () => {
    seedAssistant()

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_c1',
        parent_call_id: 'c1',
        task_label: 'audit go files',
        agent_id: 'max',
        session_id: TEST_SESSION_ID,
      })
    })

    let msgs = useChatStore.getState().messages
    let span = msgs[msgs.length - 1].spans?.[0]
    expect(span).toBeDefined()
    expect(span?.spanId).toBe('span_c1')
    expect(span?.taskLabel).toBe('audit go files')
    expect(span?.status).toBe('running')
    expect(span?.steps).toHaveLength(0)

    // tool_call_start with matching parent_call_id
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 't1',
        tool: 'fs.list',
        params: { path: '/tmp' },
        parent_call_id: 'c1',
        session_id: TEST_SESSION_ID,
      })
    })

    msgs = useChatStore.getState().messages
    span = msgs[msgs.length - 1].spans?.[0]
    expect(span?.steps).toHaveLength(1)
    const s0 = span?.steps[0]
    expect(s0?.kind === 'tool' ? s0.tool.tool : undefined).toBe('fs.list')
    expect(s0?.kind === 'tool' ? s0.tool.status : undefined).toBe('running')

    // tool_call_result
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_result',
        call_id: 't1',
        tool: 'fs.list',
        result: 'file.go',
        status: 'success',
        duration_ms: 100,
        parent_call_id: 'c1',
        session_id: TEST_SESSION_ID,
      })
    })

    msgs = useChatStore.getState().messages
    span = msgs[msgs.length - 1].spans?.[0]
    const s0after = span?.steps[0]
    expect(s0after?.kind === 'tool' ? s0after.tool.status : undefined).toBe('success')
    expect(s0after?.kind === 'tool' ? s0after.tool.result : undefined).toBe('file.go')

    // subagent_end
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'subagent_end',
        span_id: 'span_c1',
        status: 'success',
        duration_ms: 4210,
        final_result: 'Found 1 Go file',
        session_id: TEST_SESSION_ID,
      })
    })

    msgs = useChatStore.getState().messages
    span = msgs[msgs.length - 1].spans?.[0]
    expect(span?.status).toBe('success')
    // Narrow to terminal span to access durationMs and finalResult.
    const terminalSpan = span?.status !== 'running' ? span : undefined
    expect((terminalSpan as import('@/store/chat').SubagentSpanTerminal | undefined)?.durationMs).toBe(4210)
    expect((terminalSpan as import('@/store/chat').SubagentSpanTerminal | undefined)?.finalResult).toBe('Found 1 Go file')
  })

  it('out-of-order: tool_call_start arrives before subagent_start — buffered then drained', () => {
    seedAssistant()

    // tool_call_start arrives BEFORE subagent_start
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 't2',
        tool: 'shell',
        params: { cmd: 'ls' },
        parent_call_id: 'c2',
        session_id: TEST_SESSION_ID,
      })
    })

    // No span yet — should not appear in flat toolCalls either yet
    let msgs = useChatStore.getState().messages
    expect(msgs[msgs.length - 1].spans ?? []).toHaveLength(0)

    // Now subagent_start arrives — should drain the buffer
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_c2',
        parent_call_id: 'c2',
        task_label: 'list files',
        session_id: TEST_SESSION_ID,
      })
    })

    msgs = useChatStore.getState().messages
    const span = msgs[msgs.length - 1].spans?.[0]
    expect(span).toBeDefined()
    expect(span?.spanId).toBe('span_c2')
    expect(span?.steps).toHaveLength(1)
    const step0 = span?.steps[0]
    expect(step0?.kind).toBe('tool')
    expect(step0?.kind === 'tool' ? step0.tool.tool : undefined).toBe('shell')
  })

  it('step count increments +1 per tool_call_start, not per result (FR-H-010)', () => {
    seedAssistant()

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_c3',
        parent_call_id: 'c3',
        task_label: 'multi-step task',
        session_id: TEST_SESSION_ID,
      })
    })

    for (let i = 1; i <= 3; i++) {
      act(() => {
        useChatStore.getState().handleFrame({
          type: 'tool_call_start',
          call_id: `t_${i}`,
          tool: 'fs.list',
          params: {},
          parent_call_id: 'c3',
          session_id: TEST_SESSION_ID,
        })
      })
      const msgs = useChatStore.getState().messages
      const span = msgs[msgs.length - 1].spans?.[0]
      expect(span?.steps).toHaveLength(i)
    }
  })

  it('two sibling spans accumulate steps independently', () => {
    seedAssistant()

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_s1',
        parent_call_id: 's1',
        task_label: 'first',
        session_id: TEST_SESSION_ID,
      })
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'span_s2',
        parent_call_id: 's2',
        task_label: 'second',
        session_id: TEST_SESSION_ID,
      })
    })

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'ts1',
        tool: 'exec',
        params: {},
        parent_call_id: 's1',
        session_id: TEST_SESSION_ID,
      })
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'ts2a',
        tool: 'web_search',
        params: {},
        parent_call_id: 's2',
        session_id: TEST_SESSION_ID,
      })
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'ts2b',
        tool: 'file.read',
        params: {},
        parent_call_id: 's2',
        session_id: TEST_SESSION_ID,
      })
    })

    const msgs = useChatStore.getState().messages
    const spans = msgs[msgs.length - 1].spans ?? []
    expect(spans).toHaveLength(2)
    expect(spans[0].steps).toHaveLength(1)
    expect(spans[1].steps).toHaveLength(2)
  })
})

// TDD row 12: ChatStore_OrphanFrame_FallsBackFlat
// Traces to: sprint-h-subagent-block-spec.md Edge (out-of-order), FR-H-009

describe('ChatStore_OrphanFrame_FallsBackFlat', () => {
  it('frame with unknown parent_call_id + no subagent_start within 10s → flat + dev warning', async () => {
    // Use fake timers to simulate the 10s TTL without waiting
    vi.useFakeTimers()
    const warnSpy = vi.spyOn(console, 'warn')

    act(() => {
      useChatStore.getState().updateLastAssistantMessage('', false)
    })

    // tool_call_start with a parent_call_id that has no matching subagent_start
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'orphan_t1',
        tool: 'fs.list',
        params: {},
        parent_call_id: 'orphan_parent',
        session_id: TEST_SESSION_ID,
      })
    })

    // No span yet, not in toolCalls yet (buffered)
    expect(useChatStore.getState().toolCalls['orphan_t1']).toBeUndefined()

    // Advance time past 10s TTL
    await act(async () => {
      vi.advanceTimersByTime(10_001)
    })

    // Now the buffered frame should be released as a flat tool call
    const state = useChatStore.getState()
    expect(state.toolCalls['orphan_t1']).toBeDefined()
    expect(state.toolCalls['orphan_t1'].tool).toBe('fs.list')

    // A dev console warning must have been emitted with the stable prefix.
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining('[chat] orphan frame'),
    )

    vi.useRealTimers()
    warnSpy.mockRestore()
  })
})

// Regression: TestChatRouter_NonSpawnCall_NoSpan
// flat tool_call_start (no parent_call_id) is NOT grouped into any span

describe('ChatStore regression: flat tool call without parent_call_id', () => {
  it('renders as a flat ToolCallBadge (not attached to any span)', () => {
    act(() => {
      useChatStore.getState().updateLastAssistantMessage('', false)
    })

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'flat_1',
        tool: 'exec',
        params: { cmd: 'pwd' },
        session_id: TEST_SESSION_ID,
        // no parent_call_id
      })
    })

    const state = useChatStore.getState()
    // Tool call appears in the flat toolCalls map
    expect(state.toolCalls['flat_1']).toBeDefined()
    expect(state.toolCalls['flat_1'].tool).toBe('exec')

    // No span was created
    const lastMsg = state.messages[state.messages.length - 1]
    expect(lastMsg.spans ?? []).toHaveLength(0)
  })
})

// ── Sprint I: replay parity tests ─────────────────────────────────────────────

// TDD row 18: ChatStore_ReplaySequence_MatchesLiveSequence
// Traces to: sprint-i-historical-replay-fidelity-spec.md FR-I-010
// Hard Constraint: "one reducer path" — live and replay sequences must produce
// identical ChatMessage shapes (excluding cursor/isStreaming flags).
describe('ChatStore_ReplaySequence_MatchesLiveSequence', () => {
  it('live token sequence and replay_message produce equivalent content, tool-call count, and ordering', () => {
    // Full reset including toolCallOrder (beforeEach only resets a subset of state)
    act(() => { useChatStore.getState().resetSession() })

    // ── Live sequence ──────────────────────────────────────────────────────────
    // Emit token frames producing text "A", then a tool call, then text "B", then done.
    act(() => {
      // Seed an assistant placeholder (sendMessage path does this; replicate here)
      useChatStore.getState().handleFrame({ type: 'token', content: 'A', session_id: TEST_SESSION_ID })
    })
    act(() => {
      // tool_call_start (no parent_call_id — flat call)
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'tc_live_1',
        tool: 'shell',
        params: { cmd: 'echo hi' },
        session_id: TEST_SESSION_ID,
      })
    })
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_result',
        call_id: 'tc_live_1',
        tool: 'shell',
        result: { stdout: 'hi\n' },
        status: 'success',
        duration_ms: 42,
        session_id: TEST_SESSION_ID,
      })
    })
    act(() => {
      useChatStore.getState().handleFrame({ type: 'token', content: 'B', session_id: TEST_SESSION_ID })
    })
    act(() => {
      useChatStore.getState().handleFrame({ type: 'done', session_id: TEST_SESSION_ID })
    })

    const liveState = useChatStore.getState()
    // Extract the single assistant message
    const liveAssistant = liveState.messages.find((m) => m.role === 'assistant')
    expect(liveAssistant).toBeDefined()
    const liveContent = liveAssistant!.content           // "AB"
    const liveToolCallOrder = liveState.toolCallOrder    // ['tc_live_1']
    const liveToolCall = liveState.toolCalls['tc_live_1']
    expect(liveContent).toBe('AB')
    expect(liveToolCallOrder).toHaveLength(1)
    expect(liveToolCall.tool).toBe('shell')
    expect(liveToolCall.status).toBe('success')
    // Live sequence: streaming flags settled
    expect(liveAssistant!.isStreaming).toBe(false)

    // ── Reset ─────────────────────────────────────────────────────────────────
    act(() => {
      useChatStore.getState().resetSession()
    })

    // ── Replay sequence ────────────────────────────────────────────────────────
    // replay_message for the completed assistant text, then tool_call_start/result, then done.
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'replay_message',
        role: 'assistant',
        content: 'AB',
        session_id: TEST_SESSION_ID,
      })
    })
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'tc_replay_1',
        tool: 'shell',
        params: { cmd: 'echo hi' },
        session_id: TEST_SESSION_ID,
      })
    })
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_result',
        call_id: 'tc_replay_1',
        tool: 'shell',
        result: { stdout: 'hi\n' },
        status: 'success',
        duration_ms: 42,
        session_id: TEST_SESSION_ID,
      })
    })
    act(() => {
      useChatStore.getState().handleFrame({ type: 'done', session_id: TEST_SESSION_ID })
    })

    const replayState = useChatStore.getState()
    const replayAssistant = replayState.messages.find((m) => m.role === 'assistant')
    expect(replayAssistant).toBeDefined()

    // ── Assert shape parity ────────────────────────────────────────────────────
    // Content must match
    expect(replayAssistant!.content).toBe(liveContent)

    // Tool-call count must match
    expect(replayState.toolCallOrder).toHaveLength(liveToolCallOrder.length)

    // Tool-call properties must match
    const replayToolCall = replayState.toolCalls['tc_replay_1']
    expect(replayToolCall.tool).toBe(liveToolCall.tool)
    expect(replayToolCall.status).toBe(liveToolCall.status)

    // Cursor/streaming flags: replay_message arrives as a completed message (no cursor)
    // Live message: also settled after done. Both must be false.
    expect(replayAssistant!.isStreaming).toBe(false)
    // Live and replay both settle identically after done
    expect(replayAssistant!.isStreaming).toBe(liveAssistant!.isStreaming)
  })
})

// TDD row 19: ChatStore_ReplayMessageThenToolCall_InterleavesCorrectly
// Traces to: sprint-i-historical-replay-fidelity-spec.md FR-I-010
// Verifies that textAtToolCallStart is captured correctly when a tool_call_start
// follows a replay_message (completed, non-streaming assistant message).
describe('ChatStore_ReplayMessageThenToolCall_InterleavesCorrectly', () => {
  it('textAtToolCallStart snapshot equals the replay_message content when tool_call_start follows', () => {
    act(() => { useChatStore.getState().resetSession() })
    // Simulate: replay_message with content "Hello from replay" arrives, then tool_call_start
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'replay_message',
        role: 'assistant',
        content: 'Hello from replay',
        session_id: TEST_SESSION_ID,
      })
    })

    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'tc_interleave_1',
        tool: 'fs.read',
        params: { path: '/etc/hosts' },
        session_id: TEST_SESSION_ID,
      })
    })

    const state = useChatStore.getState()

    // The tool call must be registered
    expect(state.toolCalls['tc_interleave_1']).toBeDefined()
    expect(state.toolCalls['tc_interleave_1'].tool).toBe('fs.read')

    // textAtToolCallStart must capture the replay_message content at the
    // point the tool call started — this is the visual text position for interleaving.
    const snapshot = state.textAtToolCallStart['tc_interleave_1']
    expect(snapshot).toBe('Hello from replay')
  })

  it('textAtToolCallStart is empty string when tool_call_start arrives before any assistant message', () => {
    act(() => { useChatStore.getState().resetSession() })
    // During replay, tool_call_start may arrive after an entry with no text content.
    // The snapshot should be '' not undefined.
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'tc_no_text',
        tool: 'web_search',
        params: { query: 'test' },
        session_id: TEST_SESSION_ID,
      })
    })

    const state = useChatStore.getState()
    const snapshot = state.textAtToolCallStart['tc_no_text']
    expect(snapshot).toBe('')
  })
})

// TDD row 18 supplement: isReplaying flag transitions
describe('ChatStore_isReplaying_flag', () => {
  it('starts false, can be set true via setReplaying, cleared to false on done (with 250ms minimum display window)', async () => {
    // W2-6(a): Fix comment: "100ms minimum" → "250ms minimum" (matches MIN_REPLAY_DISPLAY_MS constant).
    // Traces to: temporal-puzzling-melody.md W2-6(a)
    // Initial state
    expect(useChatStore.getState().isReplaying).toBe(false)

    // Simulate attach_session triggering setReplaying(true)
    act(() => {
      useChatStore.getState().setReplaying(true)
    })
    expect(useChatStore.getState().isReplaying).toBe(true)

    // done frame schedules clear — but minimum 250ms display window is enforced
    // so the placeholder doesn't flicker on sub-frame replays.
    act(() => {
      useChatStore.getState().handleFrame({ type: 'done', session_id: TEST_SESSION_ID })
    })
    // Still true immediately after done (inside the window).
    expect(useChatStore.getState().isReplaying).toBe(true)

    // After >= 250ms the setTimeout fires and flips it.
    await new Promise((r) => setTimeout(r, 300))
    expect(useChatStore.getState().isReplaying).toBe(false)
  })

  it('done frame while not replaying is harmless — isReplaying stays false', () => {
    expect(useChatStore.getState().isReplaying).toBe(false)
    act(() => {
      useChatStore.getState().handleFrame({ type: 'done', session_id: TEST_SESSION_ID })
    })
    expect(useChatStore.getState().isReplaying).toBe(false)
  })

  it('resetSession clears isReplaying', () => {
    act(() => {
      useChatStore.getState().setReplaying(true)
    })
    expect(useChatStore.getState().isReplaying).toBe(true)
    act(() => {
      useChatStore.getState().resetSession()
    })
    expect(useChatStore.getState().isReplaying).toBe(false)
  })

  // W2-6(b): setReplaying(false) when already false is a no-op.
  it('setReplaying(false) when already false is a no-op — isReplaying stays false', () => {
    // BDD: Given isReplaying is already false
    // BDD: When setReplaying(false) is called
    // BDD: Then isReplaying stays false (no state change)
    // Traces to: temporal-puzzling-melody.md W2-6(b)
    expect(useChatStore.getState().isReplaying).toBe(false)
    act(() => {
      useChatStore.getState().setReplaying(false)
    })
    expect(useChatStore.getState().isReplaying).toBe(false)
  })

  // W2-6(c): setReplaying(true) when already true does NOT reset replayingStartedAt.
  it('setReplaying(true) when already true does not extend the minimum window', async () => {
    // BDD: Given setReplaying(true) was called at T=0, starting the 250ms minimum window
    // BDD: When setReplaying(true) is called again at T=200ms (before window ends)
    // BDD: Then the window does NOT extend — done frame at T=210ms still fires within 250ms of T=0
    // Traces to: temporal-puzzling-melody.md W2-6(c)

    act(() => {
      useChatStore.getState().setReplaying(true)
    })
    expect(useChatStore.getState().isReplaying).toBe(true)

    // Wait 200ms (still inside the 250ms window from the first call)
    await new Promise((r) => setTimeout(r, 200))

    // Call setReplaying(true) again — if it reset replayingStartedAt, the window would
    // extend by another 250ms. The test verifies it does NOT by checking that
    // isReplaying flips to false within ~100ms after this second call (total ~300ms > 250ms from T=0).
    act(() => {
      useChatStore.getState().setReplaying(true)
    })
    expect(useChatStore.getState().isReplaying).toBe(true)

    // Issue done to schedule the clear.
    act(() => {
      useChatStore.getState().handleFrame({ type: 'done', session_id: TEST_SESSION_ID })
    })

    // After another 100ms (total ~300ms from first call), should have cleared.
    // If the window was reset on the second setReplaying(true), it would still be true here.
    await new Promise((r) => setTimeout(r, 100))
    // isReplaying should be false by now (250ms from original T=0 has elapsed).
    expect(useChatStore.getState().isReplaying).toBe(false)
  })
})

// W2-10: Sibling-spans cross-wire test.
// Two spans A (parentCallId "cA") and B (parentCallId "cB") open.
// Emit 2 tool_call_start frames both with parent_call_id "cA".
// Assert A.steps.length === 2 AND B.steps.length === 0.
// Guards against a routing bug that could increment both spans' counters.
//
// Traces to: temporal-puzzling-melody.md W2-10
describe('ChatStore_sibling_spans_crosswire (W2-10)', () => {
  it('tool_call_start with parent_call_id "cA" routes to span A only, not span B', () => {
    // BDD: Given two open spans A (parentCallId "cA") and B (parentCallId "cB")
    // BDD: When 2 tool_call_start frames arrive with parent_call_id "cA"
    // BDD: Then span A has 2 steps and span B has 0 steps
    // Traces to: temporal-puzzling-melody.md W2-10

    act(() => {
      // Create an assistant message to host the spans
      useChatStore.getState().appendMessage({
        id: 'asst-sibling-1',
        role: 'assistant',
        content: 'Working...',
        timestamp: new Date().toISOString(),
        status: 'streaming',
        isStreaming: true,
      })

      // Start span A (parentCallId = "cA")
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'spanA',
        parent_call_id: 'cA',
        task_label: 'Span A task',
        agent_id: 'agent-a',
        session_id: TEST_SESSION_ID,
      })

      // Start span B (parentCallId = "cB")
      useChatStore.getState().handleFrame({
        type: 'subagent_start',
        span_id: 'spanB',
        parent_call_id: 'cB',
        task_label: 'Span B task',
        agent_id: 'agent-b',
        session_id: TEST_SESSION_ID,
      })
    })

    // Emit 2 tool_call_start frames, both targeting span A (parent_call_id: "cA")
    act(() => {
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'step_a_1',
        tool: 'web_search',
        params: { query: 'query 1' },
        parent_call_id: 'cA',
        session_id: TEST_SESSION_ID,
      })
      useChatStore.getState().handleFrame({
        type: 'tool_call_start',
        call_id: 'step_a_2',
        tool: 'fs.read',
        params: { path: '/tmp/test' },
        parent_call_id: 'cA',
        session_id: TEST_SESSION_ID,
      })
    })

    const state = useChatStore.getState()
    const asstMsg = state.messages.find((m) => m.id === 'asst-sibling-1')
    expect(asstMsg).toBeDefined()
    expect(asstMsg?.spans).toHaveLength(2)

    const spanA = asstMsg!.spans!.find((s) => s.spanId === 'spanA')
    const spanB = asstMsg!.spans!.find((s) => s.spanId === 'spanB')
    expect(spanA).toBeDefined()
    expect(spanB).toBeDefined()

    // Span A must have exactly 2 steps (both tool_call_start frames targeted "cA")
    expect(spanA!.steps).toHaveLength(2)
    const stepA0 = spanA!.steps[0]
    const stepA1 = spanA!.steps[1]
    expect(stepA0.kind === 'tool' ? stepA0.tool.call_id : undefined).toBe('step_a_1')
    expect(stepA1.kind === 'tool' ? stepA1.tool.call_id : undefined).toBe('step_a_2')

    // Span B must have exactly 0 steps (no frames targeted "cB")
    expect(spanB!.steps).toHaveLength(0)
  })
})
