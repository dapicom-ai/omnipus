import { useState } from 'react'
import {
  Wrench,
  ArrowsClockwise,
  CheckCircle,
  XCircle,
  Prohibit,
  CaretDown,
  CaretUp,
} from '@phosphor-icons/react'
import { cn } from '@/lib/utils'
import type { MessagePartStatus } from '@assistant-ui/react'

interface GenericToolCallProps {
  toolName: string
  args?: unknown
  result?: unknown
  status: MessagePartStatus
  /** Optional error text from the store */
  error?: string
  /** Optional duration in milliseconds */
  durationMs?: number
}

function formatDuration(ms?: number): string {
  if (!ms) return ''
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function safeJson(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function GenericToolCall({
  toolName,
  args,
  result,
  status,
  error,
  durationMs,
}: GenericToolCallProps) {
  const [expanded, setExpanded] = useState(false)

  const isRunning = status.type === 'running'
  const isError = status.type === 'incomplete' || !!error
  // AssistantUI's MessagePartStatus does not expose `reason` on the `incomplete` variant in its
  // public types, so we narrow with `'reason' in status` before casting to access it safely.
  const isCancelled = status.type === 'incomplete' && 'reason' in status && (status as { reason?: string }).reason === 'cancelled'

  const statusConfig = isRunning
    ? { icon: <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />, label: 'Running...', border: 'border-[var(--color-border)]' }
    : isCancelled
    ? { icon: <Prohibit size={12} weight="fill" className="text-[var(--color-muted)]" />, label: 'Cancelled', border: 'border-[var(--color-border)]' }
    : isError
    ? { icon: <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />, label: 'Failed', border: 'border-[var(--color-error)]/20' }
    : { icon: <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />, label: formatDuration(durationMs) || 'Done', border: 'border-[var(--color-success)]/20' }

  const hasDetail = !isRunning && (args !== undefined || result !== undefined || error)

  return (
    <div
      data-testid="tool-call-badge"
      data-tool={toolName}
      className={cn('mt-2 rounded-md border bg-[var(--color-surface-1)] text-xs font-mono overflow-hidden', statusConfig.border)}
    >
      {/* Header row */}
      <button
        type="button"
        onClick={() => hasDetail && setExpanded((e) => !e)}
        className={cn(
          'flex w-full items-center gap-2 px-3 py-2 text-left transition-colors',
          hasDetail && 'hover:bg-[var(--color-surface-2)] cursor-pointer',
          !hasDetail && 'cursor-default'
        )}
        aria-expanded={expanded}
        disabled={!hasDetail}
      >
        <Wrench size={13} className="text-[var(--color-muted)] shrink-0" />
        <span className="text-[var(--color-secondary)] font-medium">{toolName}</span>
        <span className="flex items-center gap-1 ml-1">
          {statusConfig.icon}
          <span className="text-[var(--color-muted)]">{statusConfig.label}</span>
        </span>
        {hasDetail && (
          <span className="ml-auto text-[var(--color-muted)]">
            {expanded ? <CaretUp size={12} /> : <CaretDown size={12} />}
          </span>
        )}
      </button>

      {/* Expanded detail */}
      {expanded && hasDetail && (
        <div className="border-t border-[var(--color-border)] px-3 py-2 space-y-2">
          {args !== undefined && (
            <div>
              <div className="text-[var(--color-muted)] mb-1 font-sans">Parameters</div>
              <pre className="text-[10px] text-[var(--color-secondary)] whitespace-pre-wrap break-all max-h-48 overflow-auto">
                {safeJson(args)}
              </pre>
            </div>
          )}
          {result !== undefined && (
            <div>
              <div className="text-[var(--color-muted)] mb-1 font-sans">Result</div>
              <pre className="text-[10px] text-[var(--color-secondary)] whitespace-pre-wrap break-all max-h-48 overflow-auto">
                {safeJson(result)}
              </pre>
            </div>
          )}
          {error && (
            <div className="text-[var(--color-error)] text-[10px] font-sans">{error}</div>
          )}
        </div>
      )}
    </div>
  )
}
