import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { act } from 'react'
import { MessageInput } from './MessageInput'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore } from '@/store/session'

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
      isStreaming: false,
      messages: [],
      toolCalls: {},
      pendingApprovals: [],
    })
    useConnectionStore.setState({ connection: null, isConnected: false })
    useSessionStore.setState({ activeSessionId: null, activeAgentId: null })
  })
})

describe('MessageInput — stop button during streaming (test #36)', () => {
  it('renders Stop button while isStreaming', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC1: send button transforms into Stop during streaming
    // FR-21: aria-label is now the current label state ("Stop" initially)
    act(() => {
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({ isConnected: true })
    })
    render(<MessageInput />)
    // aria-label is "Stop" (the label from stopButtonLabel('stop'))
    expect(screen.getByRole('button', { name: /^stop$/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /send message/i })).toBeNull()
  })

  it('calls cancelStream when Stop button is clicked', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Cancel during streaming (AC1)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({
        isConnected: true,
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
      })
      useSessionStore.setState({ activeSessionId: 'sess_1' })
    })
    render(<MessageInput />)
    fireEvent.click(screen.getByRole('button', { name: /^stop$/i }))
    // cancelStream sends cancel frame (or is no-op if we set isStreaming false)
    // It calls connection.send with cancel frame
    expect(useChatStore.getState().isStreaming).toBe(false)
  })
})

describe('MessageInput — send button when idle (test #36)', () => {
  it('renders Send button (aria-label="Send message") when idle', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: Stop reverts to Send after cancel/done
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({ isConnected: true })
    })
    render(<MessageInput />)
    expect(screen.getByRole('button', { name: /send message/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /stop generation/i })).toBeNull()
  })

  it('Send button is disabled when input is empty', () => {
    // Traces to: wave5a-wire-ui-spec.md — Edge case: empty message cannot be submitted
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({ isConnected: true })
    })
    render(<MessageInput />)
    const sendBtn = screen.getByRole('button', { name: /send message/i })
    expect(sendBtn).toBeDisabled()
  })

  it('Send button is disabled when disconnected', () => {
    // Traces to: wave5a-wire-ui-spec.md — Disconnected: send is blocked
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({ isConnected: false })
    })
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
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({
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

// ── T14: Escape fires cancel ONLY during streaming ────────────────────────────

describe('T14: Escape fires cancelStream only during streaming', () => {
  it('pressing Escape during streaming calls cancelStream', () => {
    // T14: cancel fires during streaming
    const mockSend = vi.fn().mockReturnValue(true)
    act(() => {
      useChatStore.setState({ isStreaming: true })
      useConnectionStore.setState({
        isConnected: true,
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
      })
      useSessionStore.setState({ activeSessionId: 'sess_escape_test' })
    })
    render(<MessageInput />)
    const textarea = screen.getByRole('textbox')
    fireEvent.keyDown(textarea, { key: 'Escape' })
    // After Escape during streaming, isStreaming is false (cancel fired)
    expect(useChatStore.getState().isStreaming).toBe(false)
    // The cancel frame was sent over the connection
    expect(mockSend).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'cancel', session_id: 'sess_escape_test' })
    )
  })

  it('pressing Escape when NOT streaming does not call cancelStream (T14 complement)', () => {
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({ isStreaming: false })
      useConnectionStore.setState({
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
    // isStreaming was already false; mockSend must not be called
    expect(mockSend).not.toHaveBeenCalled()
    expect(useChatStore.getState().isStreaming).toBe(false)
  })
})
