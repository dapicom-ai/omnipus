import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { act } from 'react'
import { ExecApprovalBlock } from '@/components/chat/ExecApprovalBlock'
import { useChatStore } from '@/store/chat'

// test_approval_flow (test #30)
// Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval prompt

beforeEach(() => {
  act(() => {
    useChatStore.setState({
      connection: null,
      isConnected: false,
      pendingApprovals: [],
      messages: [],
      isStreaming: false,
      toolCalls: {},
    })
  })
})

describe('approval flow integration (test #30)', () => {
  it.each([
    ['Allow', 'allow'],
    ['Deny', 'deny'],
    ['Always Allow', 'always'],
  ] as const)('clicking "%s" sends correct decision over WebSocket', (buttonLabel, decision) => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario Outline: User responds to approval prompt (AC2-4)
    const mockSend = vi.fn()
    act(() => {
      useChatStore.setState({
        connection: {
          send: mockSend,
          disconnect: vi.fn(),
          connect: vi.fn(),
          isConnected: true,
        } as any,
        isConnected: true,
      })
      useChatStore.getState().addApprovalRequest({
        type: 'exec_approval_request',
        id: 'appr_integration',
        command: 'git pull origin main',
        working_dir: '~/projects/omnipus',
        matched_policy: 'tools.exec.approval=ask',
      })
    })

    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_integration',
          command: 'git pull origin main',
          working_dir: '~/projects/omnipus',
          matched_policy: 'tools.exec.approval=ask',
          status: 'pending',
        }}
      />
    )

    // Use anchored regex to avoid "Allow" matching "Always Allow" as well
    fireEvent.click(screen.getByRole('button', { name: new RegExp(`^${buttonLabel}$`, 'i') }))

    expect(mockSend).toHaveBeenCalledWith({
      type: 'exec_approval_response',
      id: 'appr_integration',
      decision,
    })
  })

  it('shows "Allowed" status when approval is resolved as allowed', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC2: block updates to allowed state
    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_allowed',
          command: 'git pull origin main',
          status: 'allowed',
        }}
      />
    )
    expect(screen.getByText(/Allowed/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Allow$/i })).toBeNull()
  })

  it('shows "Denied" status when approval is resolved as denied', () => {
    // Traces to: wave5a-wire-ui-spec.md — AC3: block updates to denied state
    render(
      <ExecApprovalBlock
        approval={{
          id: 'appr_denied',
          command: 'rm -rf /tmp',
          status: 'denied',
        }}
      />
    )
    expect(screen.getByText(/Denied/i)).toBeInTheDocument()
  })
})
