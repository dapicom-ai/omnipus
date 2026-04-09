import { useState } from 'react'
import { makeAssistantToolUI } from '@assistant-ui/react'
import {
  Terminal,
  ArrowsClockwise,
  CheckCircle,
  XCircle,
  CaretDown,
  CaretUp,
} from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

interface ExecArgs {
  action?: string
  command?: string
  timeout?: number
  background?: boolean
  pty?: boolean
  session_id?: string
}

function TerminalOutputBlock({
  args,
  result,
  isRunning,
  isError,
}: {
  args: ExecArgs
  result: unknown
  isRunning: boolean
  isError?: boolean
}) {
  const [expanded, setExpanded] = useState(true)

  const command = args.command ?? args.session_id ?? '(unknown command)'
  const action = args.action ?? 'run'
  const output = result != null ? String(result) : ''

  // Determine label based on action
  const actionLabel =
    action === 'run' ? 'exec' :
    action === 'read' ? 'exec read' :
    action === 'write' ? 'exec write' :
    action === 'kill' ? 'exec kill' :
    action === 'send-keys' ? 'exec send-keys' :
    action

  return (
    <div
      className={cn(
        'mt-2 rounded-md border overflow-hidden font-mono text-xs',
        isRunning
          ? 'border-[var(--color-border)]'
          : isError
          ? 'border-[var(--color-error)]/30 bg-[var(--color-error)]/5'
          : 'border-[var(--color-success)]/20'
      )}
    >
      {/* Header */}
      <button
        type="button"
        onClick={() => setExpanded((e) => !e)}
        className="flex w-full items-center gap-2 px-3 py-2 bg-[var(--color-surface-1)] hover:bg-[var(--color-surface-2)] transition-colors text-left cursor-pointer"
        aria-expanded={expanded}
      >
        <Terminal
          size={13}
          weight="bold"
          className={cn(
            isRunning ? 'text-[var(--color-accent)]' :
            isError ? 'text-[var(--color-error)]' :
            'text-[var(--color-success)]'
          )}
        />
        <span className="text-[var(--color-muted)] shrink-0">{actionLabel}</span>
        <span className="text-[var(--color-secondary)] truncate flex-1 min-w-0">{command}</span>
        <span className="flex items-center gap-1 shrink-0">
          {isRunning ? (
            <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />
          ) : isError ? (
            <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />
          ) : (
            <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />
          )}
          <span className="text-[var(--color-muted)] ml-0.5">
            {expanded ? <CaretUp size={10} /> : <CaretDown size={10} />}
          </span>
        </span>
      </button>

      {/* Output panel */}
      {expanded && (
        <div className="bg-[#0d1117] border-t border-[var(--color-border)]">
          {isRunning && !output ? (
            <div className="px-3 py-2 text-[var(--color-muted)] italic flex items-center gap-2">
              <ArrowsClockwise size={11} className="animate-spin" />
              Executing...
            </div>
          ) : (
            <pre className="px-3 py-2 text-[10px] leading-5 text-[var(--color-secondary)] whitespace-pre-wrap break-all max-h-64 overflow-auto">
              {output || <span className="text-[var(--color-muted)] italic">(no output)</span>}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}

export const TerminalOutputUI = makeAssistantToolUI<ExecArgs, unknown>({
  toolName: 'exec',
  render: ({ args, result, status }) => (
    <TerminalOutputBlock
      args={args ?? {}}
      result={result}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})
