import { makeAssistantToolUI } from '@assistant-ui/react'
import {
  PencilSimple,
  FloppyDisk,
  Plus,
  ArrowsClockwise,
  CheckCircle,
  XCircle,
} from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

interface WriteFileArgs {
  path?: string
  content?: string
}

interface EditFileArgs {
  path?: string
  old_string?: string
  new_string?: string
  replace_all?: boolean
}

interface AppendFileArgs {
  path?: string
  content?: string
}

function basename(p: string): string {
  return p.split(/[/\\]/).pop() ?? p
}

function byteCount(s?: string): string {
  if (!s) return '0 B'
  const bytes = new TextEncoder().encode(s).length
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function FileOpBlock({
  icon: Icon,
  label,
  path,
  detail,
  isRunning,
  isError,
}: {
  icon: React.ComponentType<{ size?: number; weight?: string; className?: string }>
  label: string
  path: string
  detail?: string
  isRunning: boolean
  isError?: boolean
}) {
  return (
    <div
      className={cn(
        'mt-2 rounded-md border overflow-hidden text-xs',
        isRunning
          ? 'border-[var(--color-border)]'
          : isError
          ? 'border-[var(--color-error)]/30'
          : 'border-[var(--color-success)]/20'
      )}
    >
      <div className="flex items-center gap-2 px-3 py-2 bg-[var(--color-surface-1)]">
        <Icon
          size={13}
          weight="duotone"
          className={cn(
            isRunning ? 'text-[var(--color-accent)]' :
            isError ? 'text-[var(--color-error)]' :
            'text-[var(--color-success)]'
          )}
        />
        <span className="text-[var(--color-muted)] shrink-0">{label}</span>
        <span className="font-mono text-[var(--color-secondary)] truncate flex-1 min-w-0">
          {basename(path)}
        </span>
        <span className="flex items-center gap-1 shrink-0">
          {isRunning ? (
            <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />
          ) : isError ? (
            <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />
          ) : (
            <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />
          )}
          {detail && !isRunning && (
            <span className="text-[var(--color-muted)] ml-1">{detail}</span>
          )}
        </span>
      </div>
    </div>
  )
}

export const FileWriteConfirmUI = makeAssistantToolUI<WriteFileArgs, unknown>({
  toolName: 'write_file',
  render: ({ args, status }) => (
    <FileOpBlock
      icon={FloppyDisk}
      label="write_file"
      path={args?.path ?? '(unknown)'}
      detail={byteCount(args?.content)}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})

export const EditFileConfirmUI = makeAssistantToolUI<EditFileArgs, unknown>({
  toolName: 'edit_file',
  render: ({ args, status }) => (
    <FileOpBlock
      icon={PencilSimple}
      label="edit_file"
      path={args?.path ?? '(unknown)'}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})

export const AppendFileConfirmUI = makeAssistantToolUI<AppendFileArgs, unknown>({
  toolName: 'append_file',
  render: ({ args, status }) => (
    <FileOpBlock
      icon={Plus}
      label="append_file"
      path={args?.path ?? '(unknown)'}
      detail={byteCount(args?.content)}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})
