// Unit tests for ToolApprovalModal — FR-011, FR-082, FR-052
//
// Tests:
//  1. Modal does not render when queue is empty
//  2. Modal renders when queue has an entry
//  3. Approve button calls POST /api/v1/tool-approvals/{id} with action:"approve"
//  4. Deny button calls POST with action:"deny"
//  5. Cancel button calls POST with action:"cancel"
//  6. On 410 response, modal entry is dismissed without a toast
//  7. On 403 response, shows admin-required toast
//  8. On 401 response, shows re-auth toast
//  9. Countdown uses expires_in_ms — expiresAt computed as Date.now() + expires_in_ms
// 10. session_state reconciliation removes stale approvals

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { act } from 'react'

vi.mock('@/lib/api', () => ({
  postToolApproval: vi.fn(),
}))

vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn((selector) => {
    const state = {
      addToast: vi.fn(),
      toasts: [],
      removeToast: vi.fn(),
    }
    return selector ? selector(state) : state
  }),
}))

import * as api from '@/lib/api'
import { useToolApprovalStore } from '@/store/toolApproval'
import { ToolApprovalModal } from './ToolApprovalModal'
import type { WsSessionStateFrame } from '@/lib/ws'

// Capture the mock addToast for assertion
let capturedAddToast: ReturnType<typeof vi.fn>

beforeEach(async () => {
  capturedAddToast = vi.fn()
  const { useUiStore } = await import('@/store/ui')
  vi.mocked(useUiStore).mockImplementation((selector?: (s: unknown) => unknown) => {
    const state = { addToast: capturedAddToast, toasts: [], removeToast: vi.fn() }
    return selector ? selector(state) : state
  })

  // Reset store
  act(() => {
    useToolApprovalStore.setState({ queue: [] })
  })
  vi.clearAllMocks()
  vi.mocked(api.postToolApproval).mockResolvedValue(undefined)
})

const SAMPLE_APPROVAL = {
  approvalId: 'appr-001',
  toolCallId: 'call-001',
  toolName: 'web_fetch',
  args: { url: 'https://example.com' },
  agentId: 'agent-main',
  sessionId: 'sess-001',
  turnId: 'turn-001',
  expiresAt: Date.now() + 300_000, // 5 minutes
}

describe('ToolApprovalModal — rendering', () => {
  it('renders nothing when the queue is empty', () => {
    const { container } = render(<ToolApprovalModal />)
    expect(container.firstChild).toBeNull()
  })

  it('renders the modal when the queue has an entry', () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)
    expect(screen.getByText('web_fetch')).toBeInTheDocument()
    expect(screen.getByText('Tool Approval Required')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Approve/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Deny/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Cancel/i })).toBeInTheDocument()
  })

  it('shows the tool args in the modal', () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)
    expect(screen.getByText(/https:\/\/example\.com/)).toBeInTheDocument()
  })

  it('shows queue depth indicator when multiple approvals are pending', () => {
    act(() => {
      useToolApprovalStore.setState({
        queue: [
          SAMPLE_APPROVAL,
          { ...SAMPLE_APPROVAL, approvalId: 'appr-002', toolName: 'exec' },
        ],
      })
    })
    render(<ToolApprovalModal />)
    expect(screen.getByText('+1 more')).toBeInTheDocument()
  })
})

describe('ToolApprovalModal — button dispatch', () => {
  it('Approve button calls postToolApproval with action:"approve"', async () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Approve/i }))

    await waitFor(() => {
      expect(api.postToolApproval).toHaveBeenCalledWith('appr-001', 'approve')
    })
  })

  it('Deny button calls postToolApproval with action:"deny"', async () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Deny/i }))

    await waitFor(() => {
      expect(api.postToolApproval).toHaveBeenCalledWith('appr-001', 'deny')
    })
  })

  it('Cancel button calls postToolApproval with action:"cancel"', async () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /^Cancel$/i }))

    await waitFor(() => {
      expect(api.postToolApproval).toHaveBeenCalledWith('appr-001', 'cancel')
    })
  })

  it('removes approval from queue after successful Approve', async () => {
    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Approve/i }))

    await waitFor(() => {
      expect(useToolApprovalStore.getState().queue).toHaveLength(0)
    })
  })
})

describe('ToolApprovalModal — error handling', () => {
  it('dismisses silently on 410 Gone (already resolved)', async () => {
    vi.mocked(api.postToolApproval).mockRejectedValue(new Error('410: Gone'))

    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Deny/i }))

    await waitFor(() => {
      // Entry removed from queue on 410
      expect(useToolApprovalStore.getState().queue).toHaveLength(0)
    })
    // No toast should be shown for 410
    expect(capturedAddToast).not.toHaveBeenCalled()
  })

  it('shows admin-required toast on 403', async () => {
    vi.mocked(api.postToolApproval).mockRejectedValue(new Error('403: Forbidden'))

    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Approve/i }))

    await waitFor(() => {
      expect(capturedAddToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringMatching(/must be an admin/i),
          variant: 'error',
        })
      )
    })
    // Approval stays in queue on 403 (non-dismissive error)
    expect(useToolApprovalStore.getState().queue).toHaveLength(1)
  })

  it('shows re-auth toast on 401', async () => {
    vi.mocked(api.postToolApproval).mockRejectedValue(new Error('401: Unauthorized'))

    act(() => {
      useToolApprovalStore.setState({ queue: [SAMPLE_APPROVAL] })
    })
    render(<ToolApprovalModal />)

    fireEvent.click(screen.getByRole('button', { name: /Approve/i }))

    await waitFor(() => {
      expect(capturedAddToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringMatching(/log in again/i),
          variant: 'error',
        })
      )
    })
  })
})

describe('ToolApprovalModal — expires_in_ms countdown (FR-082)', () => {
  it('expiresAt is computed as Date.now() + expires_in_ms on enqueue', () => {
    const before = Date.now()
    const EXPIRES_IN_MS = 300_000

    act(() => {
      useToolApprovalStore.getState().enqueue({
        type: 'tool_approval_required',
        approval_id: 'appr-countdown',
        tool_call_id: 'call-x',
        tool_name: 'exec',
        args: {},
        agent_id: 'a',
        session_id: 's',
        turn_id: 't',
        expires_in_ms: EXPIRES_IN_MS,
      })
    })

    const after = Date.now()
    const entry = useToolApprovalStore.getState().queue[0]
    expect(entry).toBeDefined()
    // expiresAt should be within [before + EXPIRES_IN_MS, after + EXPIRES_IN_MS]
    expect(entry.expiresAt).toBeGreaterThanOrEqual(before + EXPIRES_IN_MS)
    expect(entry.expiresAt).toBeLessThanOrEqual(after + EXPIRES_IN_MS)
  })
})

describe('ToolApprovalModal — session_state reset handler (FR-052, FR-073, FR-081)', () => {
  it('removes stale approvals not present in session_state', () => {
    act(() => {
      useToolApprovalStore.setState({
        queue: [
          { ...SAMPLE_APPROVAL, approvalId: 'appr-stale-1' },
          { ...SAMPLE_APPROVAL, approvalId: 'appr-stale-2' },
          { ...SAMPLE_APPROVAL, approvalId: 'appr-live' },
        ],
      })
    })

    const sessionStateFrame: WsSessionStateFrame = {
      type: 'session_state',
      user_id: 'user-1',
      pending_approvals: [
        {
          approval_id: 'appr-live',
          session_id: 'sess-001',
          tool_name: 'web_fetch',
          agent_id: 'agent-main',
          expires_in_ms: 299_000,
        },
      ],
      emitted_at: new Date().toISOString(),
    }

    act(() => {
      useToolApprovalStore.getState().reconcileWithSessionState(sessionStateFrame)
    })

    const queue = useToolApprovalStore.getState().queue
    expect(queue).toHaveLength(1)
    expect(queue[0].approvalId).toBe('appr-live')
  })

  it('refreshes expiresAt for approvals present in session_state', () => {
    const oldExpiresAt = Date.now() + 10_000 // was 10s remaining

    act(() => {
      useToolApprovalStore.setState({
        queue: [{ ...SAMPLE_APPROVAL, approvalId: 'appr-live', expiresAt: oldExpiresAt }],
      })
    })

    const newExpiresInMs = 299_000 // gateway says 299s remaining

    const before = Date.now()
    act(() => {
      useToolApprovalStore.getState().reconcileWithSessionState({
        type: 'session_state',
        user_id: 'user-1',
        pending_approvals: [
          {
            approval_id: 'appr-live',
            session_id: 'sess-001',
            tool_name: 'web_fetch',
            agent_id: 'agent-main',
            expires_in_ms: newExpiresInMs,
          },
        ],
        emitted_at: new Date().toISOString(),
      })
    })
    const after = Date.now()

    const entry = useToolApprovalStore.getState().queue[0]
    // expiresAt refreshed to approximately now + newExpiresInMs
    expect(entry.expiresAt).toBeGreaterThanOrEqual(before + newExpiresInMs)
    expect(entry.expiresAt).toBeLessThanOrEqual(after + newExpiresInMs)
    // Must be different from (and greater than) the old value
    expect(entry.expiresAt).toBeGreaterThan(oldExpiresAt)
  })

  it('clears all approvals when session_state has empty pending_approvals', () => {
    act(() => {
      useToolApprovalStore.setState({
        queue: [SAMPLE_APPROVAL, { ...SAMPLE_APPROVAL, approvalId: 'appr-002' }],
      })
    })

    act(() => {
      useToolApprovalStore.getState().reconcileWithSessionState({
        type: 'session_state',
        user_id: 'user-1',
        pending_approvals: [],
        emitted_at: new Date().toISOString(),
      })
    })

    expect(useToolApprovalStore.getState().queue).toHaveLength(0)
  })
})
