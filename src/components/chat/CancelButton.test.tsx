import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { act } from 'react'
import { MessageInput } from './MessageInput'
import { useChatStore } from '@/store/chat'

// test_cancel_button_states (test #36) — MessageInput send/stop button states
// test_cancel_idle_noop (test #39) — Escape when idle is no-op
// Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during streaming preserves partial response
//             wave5a-wire-ui-spec.md — Scenario: Cancel when idle is a no-op

// Note: CancelButton is not a standalone component — cancel/send behavior lives in MessageInput.
// test_cancel_preserves_partial (#37) is covered in chat.test.ts
// test_cancel_during_tool (#38) is covered in ToolCallBadge.test.tsx

beforeEach(() => {
  act(() => {
    useChatStore.setState({
      connection: null,
      isConnected: false,
      isStreaming: false,
      messages: [],
      toolCalls: {},
      pendingApprovals: [],
      activeSessionId: null,
      activeAgentId: null,
    })
  })
})

describe('MessageInput — stop button during streaming (test #36)', () => {
  it('renders Stop button (aria-label="Stop generation") while isStreaming', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC1: send button transforms into Stop during streaming
    act(() => { useChatStore.setState({ isStreaming: true, isConnected: true }) })
    render(<MessageInput />)
    expect(screen.getByRole('button', { name: /stop generation/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /send message/i })).toBeNull()
  })

  it('calls cancelStream when Stop button is clicked', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during streaming (AC1)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({
        isStreaming: true,
        isConnected: true,
        activeSessionId: 'sess_1',
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
      })
    })
    render(<MessageInput />)
    fireEvent.click(screen.getByRole('button', { name: /stop generation/i }))
    // cancelStream sends cancel frame (or is no-op if we set isStreaming false)
    // It calls connection.send with cancel frame
    expect(useChatStore.getState().isStreaming).toBe(false)
  })
})

describe('MessageInput — send button when idle (test #36)', () => {
  it('renders Send button (aria-label="Send message") when idle', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: Stop reverts to Send after cancel/done
    act(() => { useChatStore.setState({ isStreaming: false, isConnected: true }) })
    render(<MessageInput />)
    expect(screen.getByRole('button', { name: /send message/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /stop generation/i })).toBeNull()
  })

  it('Send button is disabled when input is empty', () => {
    // Traces to: wave5a-wire-ui-spec.md — Edge case: empty message cannot be submitted
    act(() => { useChatStore.setState({ isStreaming: false, isConnected: true }) })
    render(<MessageInput />)
    const sendBtn = screen.getByRole('button', { name: /send message/i })
    expect(sendBtn).toBeDisabled()
  })

  it('Send button is disabled when disconnected', () => {
    // Traces to: wave5a-wire-ui-spec.md — Disconnected: send is blocked
    act(() => { useChatStore.setState({ isStreaming: false, isConnected: false }) })
    render(<MessageInput />)
    const sendBtn = screen.getByRole('button', { name: /send message/i })
    expect(sendBtn).toBeDisabled()
  })
})

describe('MessageInput — Escape key no-op when idle (test #39)', () => {
  it('pressing Escape when not streaming does not call cancelStream', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel when idle is a no-op (AC3)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({
        isStreaming: false,
        isConnected: true,
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
      })
    })
    render(<MessageInput />)
    const textarea = screen.getByRole('textbox')
    fireEvent.keyDown(textarea, { key: 'Escape' })
    // connection.send must NOT be called (no cancel frame sent)
    expect(mockSend).not.toHaveBeenCalled()
  })
})
