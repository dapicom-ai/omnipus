import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { act } from 'react'
import { ExecApprovalBlock } from './ExecApprovalBlock'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'

// test_exec_approval_block (test #9)
// test_exec_approval_actions (test #10)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Approval block renders with command details
//             wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval prompt

beforeEach(() => {
  act(() => {
    useChatStore.setState({
      pendingApprovals: [],
      messages: [],
      isStreaming: false,
      toolCalls: {},
    })
    useConnectionStore.setState({ connection: null, isConnected: false })
  })
})

describe('ExecApprovalBlock — rendering (test #9)', () => {
  it('renders command, working directory, matched policy, and three action buttons', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Approval block renders with command details (AC1)
    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_1',
          command: 'git pull origin main',
          working_dir: '~/projects/omnipus',
          matched_policy: 'tools.exec.approval=ask',
          status: 'pending',
        }}
      />
    )
    // Command is split across spans (binary highlighted separately from args) so
    // getByText regex won't match a single element. Assert via pre element textContent.
    const pre = document.querySelector('pre')
    expect(pre?.textContent).toMatch(/git pull origin main/i)
    expect(screen.getByText(/~\/projects\/omnipus/i)).toBeInTheDocument()
    expect(screen.getByText(/tools\.exec\.approval=ask/i)).toBeInTheDocument()
    // Use anchored regex: /^Allow$/i to avoid matching "Always Allow"
    expect(screen.getByRole('button', { name: /^Allow$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Deny$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^Always Allow$/i })).toBeInTheDocument()
  })

  it('renders with a warning border/background when status is pending', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC5: warning border for pending approval
    const { container } = render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_1',
          command: 'rm -rf /tmp',
          status: 'pending',
        }}
      />
    )
    // Should have warning styling — component uses CSS variable class border-[var(--color-warning)]
    const block = container.firstElementChild as HTMLElement
    expect(block?.className).toMatch(/warning/)
  })

  it('shows "Allowed" state when approval has been allowed', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval (AC2 status)
    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_1',
          command: 'git pull origin main',
          status: 'allowed',
        }}
      />
    )
    expect(screen.getByText(/Allowed/i)).toBeInTheDocument()
    // Buttons should be hidden when resolved
    expect(screen.queryByRole('button', { name: /^Allow$/i })).toBeNull()
  })

  it('shows "Denied" state when approval has been denied', () => {
    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_1',
          command: 'git pull origin main',
          status: 'denied',
        }}
      />
    )
    expect(screen.getByText(/Denied/i)).toBeInTheDocument()
  })
})

describe('ExecApprovalBlock — user actions (test #10)', () => {
  it('clicking Allow calls respondToApproval(id, "allow")', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval (AC2)
    // We mock the connection so respondToApproval can actually call connection.send
    const mockSend = vi.fn()
    act(() => {
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
      useChatStore.getState().addApprovalRequest({
        type: 'exec_approval_request',
        id: 'appr_action_1',
        command: 'git pull origin main',
      })
    })

    render(
      <ExecApprovalBlock
        approval={{ id: 'appr_action_1', command: 'git pull origin main', status: 'pending' }}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: /^Allow$/i }))
    expect(mockSend).toHaveBeenCalledWith({ type: 'exec_approval_response', id: 'appr_action_1', decision: 'allow' })
  })

  it('clicking Deny calls respondToApproval(id, "deny")', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval (AC3)
    const mockSend = vi.fn()
    act(() => {
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
    })

    render(
      <ExecApprovalBlock
        approval={{ id: 'appr_action_2', command: 'rm -rf /tmp', status: 'pending' }}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: /deny/i }))
    expect(mockSend).toHaveBeenCalledWith({ type: 'exec_approval_response', id: 'appr_action_2', decision: 'deny' })
  })

  it('clicking Always Allow calls respondToApproval(id, "always")', () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval (AC4)
    const mockSend = vi.fn()
    act(() => {
      useConnectionStore.setState({
        connection: { send: mockSend, disconnect: vi.fn(), connect: vi.fn(), isConnected: true } as any,
        isConnected: true,
      })
    })

    render(
      <ExecApprovalBlock
        approval={{ id: 'appr_action_3', command: 'git pull origin main', status: 'pending' }}
      />
    )

    fireEvent.click(screen.getByRole('button', { name: /always allow/i }))
    expect(mockSend).toHaveBeenCalledWith({ type: 'exec_approval_response', id: 'appr_action_3', decision: 'always' })
  })
})
