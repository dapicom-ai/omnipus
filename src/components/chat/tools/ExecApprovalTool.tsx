// ExecApprovalTool — wraps the ExecApprovalBlock for display inside the message thread.
// Exec approval requests arrive as separate WS frames (exec_approval_request) and are
// handled by the ChatScreen's pending approval list. This component is provided as a
// convenience wrapper for any context where the approval data is passed directly.

import { ExecApprovalBlock } from '@/components/chat/ExecApprovalBlock'
import type { ExecApprovalBlockProps } from '@/components/chat/ExecApprovalBlock'

export function ExecApprovalTool(props: ExecApprovalBlockProps) {
  return <ExecApprovalBlock {...props} />
}
