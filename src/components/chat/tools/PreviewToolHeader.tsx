/**
 * PreviewToolHeader — shared header badge for preview tool UI components.
 *
 * Used by ServeWorkspaceUI (serve_workspace) and RunInWorkspaceUI
 * (run_in_workspace) to avoid duplicating the icon + tool-name chip +
 * label chip + status icon pattern.
 *
 * Spec: FR-008 / FR-008a.
 */

import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface PreviewToolHeaderProps {
  /** Phosphor icon element (e.g. <Globe />, <Terminal />). */
  icon: ReactNode
  /** Tool name shown in monospace (e.g. "serve_workspace"). */
  toolName: string
  /** Optional code chip shown after the tool name (path or command). */
  label?: string
  /** Element rendered at the far right (status icon or port chip). */
  trailing?: ReactNode
  /** Whether the tool is still running (drives icon pulse / border colour). */
  isRunning: boolean
  /** Whether the tool completed successfully (drives border / icon colour). */
  hasResult: boolean
}

export function PreviewToolHeader({
  icon,
  toolName,
  label,
  trailing,
  isRunning,
  hasResult,
}: PreviewToolHeaderProps) {
  return (
    <div
      className={cn(
        'flex items-center gap-2 px-3 py-2 rounded-t-md border bg-[var(--color-surface-1)]',
        isRunning
          ? 'border-[var(--color-border)]'
          : hasResult
          ? 'border-[var(--color-success)]/20'
          : 'border-[var(--color-error)]/20',
      )}
    >
      {icon}
      <span className="text-[var(--color-muted)] font-mono">{toolName}</span>
      {label && (
        <code className="ml-1 text-[var(--color-accent)] font-mono text-[10px] truncate max-w-[280px]">
          {label}
        </code>
      )}
      {trailing && <span className="ml-auto shrink-0">{trailing}</span>}
    </div>
  )
}
