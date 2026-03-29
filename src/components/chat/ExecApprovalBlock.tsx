import { Terminal, CheckCircle, XCircle, Lock } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { useChatStore } from '@/store/chat'
import { cn } from '@/lib/utils'

type ApprovalStatus = 'pending' | 'allowed' | 'denied' | 'always_allowed'

interface ApprovalData {
  id: string
  command: string
  cwd?: string
  working_dir?: string
  policy?: string
  matched_policy?: string
  status?: ApprovalStatus
}

export interface ExecApprovalBlockProps {
  /** Pass the approval as a single object (preferred, used by tests and MessageItem) */
  approval?: ApprovalData
  /** Flat props — supported for backwards compatibility */
  id?: string
  command?: string
  cwd?: string
  working_dir?: string
  policy?: string
  matched_policy?: string
  status?: ApprovalStatus
  onDecision?: (args: { id: string; decision: 'allow' | 'deny' | 'always' }) => void
}

export function ExecApprovalBlock({
  approval,
  id: idProp,
  command: commandProp,
  cwd: cwdProp,
  working_dir: workingDirProp,
  policy: policyProp,
  matched_policy: matchedPolicyProp,
  status: statusProp,
  onDecision,
}: ExecApprovalBlockProps) {
  const id = approval?.id ?? idProp ?? ''
  const command = approval?.command ?? commandProp ?? ''
  const cwd = approval?.cwd ?? cwdProp
  const working_dir = approval?.working_dir ?? workingDirProp
  const policy = approval?.policy ?? policyProp
  const matched_policy = approval?.matched_policy ?? matchedPolicyProp
  const resolvedStatusProp = approval?.status ?? statusProp
  const { respondToApproval, pendingApprovals } = useChatStore()

  // Resolve status: explicit prop wins over store-based lookup
  const storeApproval = onDecision ? undefined : pendingApprovals.find((a) => a.id === id)
  const status = resolvedStatusProp ?? storeApproval?.status ?? 'pending'

  const workingDir = cwd ?? working_dir ?? storeApproval?.working_dir
  const matchedPolicy = policy ?? matched_policy ?? storeApproval?.matched_policy

  const isPending = status === 'pending'

  const handleDecision = (decision: 'allow' | 'deny' | 'always') => {
    if (onDecision) {
      onDecision({ id, decision })
    } else {
      respondToApproval(id, decision)
    }
  }

  return (
    <div
      className={cn(
        'mt-3 rounded-lg border p-4 space-y-3',
        isPending
          ? 'border-[var(--color-warning)]/40 bg-[var(--color-warning)]/5'
          : status === 'allowed' || status === 'always_allowed'
          ? 'border-[var(--color-success)]/30 bg-[var(--color-success)]/5'
          : 'border-[var(--color-error)]/30 bg-[var(--color-error)]/5'
      )}
    >
      {/* Header */}
      <div className="flex items-center gap-2">
        <Terminal size={16} className="text-[var(--color-warning)] shrink-0" weight="bold" />
        <span className="text-sm font-medium text-[var(--color-secondary)]">
          Exec Approval Required
        </span>
        {!isPending && (
          <span className="ml-auto flex items-center gap-1 text-xs">
            {status === 'allowed' || status === 'always_allowed' ? (
              <>
                <CheckCircle size={13} className="text-[var(--color-success)]" weight="fill" />
                {status === 'always_allowed' ? 'Always Allowed' : 'Allowed'}
              </>
            ) : (
              <><XCircle size={13} className="text-[var(--color-error)]" weight="fill" /> Denied</>
            )}
          </span>
        )}
      </div>

      {/* Command */}
      <pre className="font-mono text-xs bg-[var(--color-surface-2)] rounded px-3 py-2 text-[var(--color-secondary)] whitespace-pre-wrap break-all">
        {command}
      </pre>

      {/* Metadata */}
      <div className="flex flex-wrap gap-4 text-xs text-[var(--color-muted)]">
        {workingDir && (
          <span>
            <span className="text-[var(--color-border)]">dir: </span>
            <span className="font-mono">{workingDir}</span>
          </span>
        )}
        {matchedPolicy && (
          <span>
            <span className="text-[var(--color-border)]">policy: </span>
            <span className="font-mono">{matchedPolicy}</span>
          </span>
        )}
      </div>

      {/* Action buttons — only shown while pending */}
      {isPending && (
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="default"
            onClick={() => handleDecision('allow')}
            className="h-7 text-xs"
          >
            <CheckCircle size={13} weight="bold" /> Allow
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => handleDecision('deny')}
            className="h-7 text-xs"
          >
            <XCircle size={13} weight="bold" /> Deny
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => handleDecision('always')}
            className="h-7 text-xs text-[var(--color-muted)] hover:text-[var(--color-secondary)]"
          >
            <Lock size={13} /> Always Allow
          </Button>
        </div>
      )}
    </div>
  )
}
