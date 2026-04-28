// Zustand store for tool approval queue (FR-011, FR-082).
// Manages pending tool-policy approvals that arrive via the tool_approval_required
// WS event and are resolved via POST /api/v1/tool-approvals/{id}.
//
// Design:
// - Approvals are queued in arrival order; the modal shows one at a time.
// - expires_in_ms is relative to receipt: expiresAt = Date.now() + expires_in_ms.
// - session_state reset (FR-052, FR-073, FR-081): on WS connect, clear any
//   queued approvals NOT present in the session_state payload.

import { create } from 'zustand'
import type { WsToolApprovalRequiredFrame, WsSessionStateFrame } from '@/lib/ws'

export interface PendingToolApproval {
  approvalId: string
  toolCallId: string
  toolName: string
  args: Record<string, unknown>
  agentId: string
  sessionId: string
  turnId: string
  /** Absolute local clock expiry (ms). Computed as Date.now() + expires_in_ms on receipt. */
  expiresAt: number
}

interface ToolApprovalStore {
  /** Ordered queue of pending approvals. The first entry is the currently displayed one. */
  queue: PendingToolApproval[]

  /** Add an approval from a WS tool_approval_required frame. */
  enqueue: (frame: WsToolApprovalRequiredFrame) => void

  /** Remove an approval by id (called after approve/deny/cancel resolves). */
  dequeue: (approvalId: string) => void

  /**
   * Reconcile the queue with a session_state reset frame (FR-052, FR-081).
   * Removes any queued approvals NOT present in the session_state payload and
   * refreshes expiresAt for those that are.
   */
  reconcileWithSessionState: (frame: WsSessionStateFrame) => void
}

export const useToolApprovalStore = create<ToolApprovalStore>((set) => ({
  queue: [],

  enqueue: (frame) => {
    const expiresAt = Date.now() + frame.expires_in_ms
    set((state) => {
      // Deduplicate: if already queued, replace with updated expiry
      const existing = state.queue.findIndex((a) => a.approvalId === frame.approval_id)
      if (existing !== -1) {
        const updated = [...state.queue]
        updated[existing] = {
          ...updated[existing],
          expiresAt,
        }
        return { queue: updated }
      }
      return {
        queue: [
          ...state.queue,
          {
            approvalId: frame.approval_id,
            toolCallId: frame.tool_call_id,
            toolName: frame.tool_name,
            args: frame.args,
            agentId: frame.agent_id,
            sessionId: frame.session_id,
            turnId: frame.turn_id,
            expiresAt,
          },
        ],
      }
    })
  },

  dequeue: (approvalId) => {
    set((state) => ({
      queue: state.queue.filter((a) => a.approvalId !== approvalId),
    }))
  },

  reconcileWithSessionState: (frame) => {
    const liveIds = new Set(frame.pending_approvals.map((a) => a.approval_id))
    set((state) => {
      // Keep only those still in the server's live set; refresh their expiresAt.
      const updated = state.queue
        .filter((a) => liveIds.has(a.approvalId))
        .map((a) => {
          const serverEntry = frame.pending_approvals.find((s) => s.approval_id === a.approvalId)
          if (!serverEntry) return a
          return { ...a, expiresAt: Date.now() + serverEntry.expires_in_ms }
        })
      return { queue: updated }
    })
  },
}))
