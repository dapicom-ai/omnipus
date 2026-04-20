// SubagentBlock — FR-H-008
// Renders one subagent span as a collapsible block.
// Collapsed header: icon + task label + step count + status pill + duration + caret.
// Expanded body: nested ToolCallBadges + optional final result section.
// Visual grammar matches ToolCallBadge (same border/surface palette).

import { useState } from 'react'
import {
  ArrowsClockwise,
  CheckCircle,
  XCircle,
  Prohibit,
  CaretDown,
  CaretUp,
  UserCircle,
} from '@phosphor-icons/react'
import { ToolCallBadge } from './ToolCallBadge'
import type { SubagentSpan } from '@/store/chat'
import { cn } from '@/lib/utils'

// ── Label truncation — graceme-safe (FR-H-009, Scenario 14) ──────────────────

/** Truncate to 60 grapheme clusters. Uses Array.from for emoji/CJK safety. */
function truncateLabel(raw: string): string {
  const clusters = Array.from(raw)
  if (clusters.length <= 60) return raw
  return clusters.slice(0, 60).join('') + '\u2026'
}

function deriveLabel(span: SubagentSpan): string {
  if (span.taskLabel) return truncateLabel(span.taskLabel)
  return truncateLabel(span.taskLabel)
}

// ── Duration formatting ───────────────────────────────────────────────────────

function formatDuration(ms?: number): string {
  if (ms == null) return ''
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

// ── Status config ─────────────────────────────────────────────────────────────

type SpanStatus = SubagentSpan['status']

interface StatusConfig {
  icon: React.ReactNode
  label: string
  border: string
  pill: string
}

function getStatusConfig(status: SpanStatus, durationMs?: number): StatusConfig {
  switch (status) {
    case 'running':
      return {
        icon: <ArrowsClockwise size={13} className="animate-spin text-[var(--color-accent)]" aria-hidden="true" />,
        label: 'working',
        border: 'border-[var(--color-border)]',
        pill: 'bg-[var(--color-accent)]/10 text-[var(--color-accent)]',
      }
    case 'success':
      return {
        icon: <CheckCircle size={13} className="text-[var(--color-success)]" weight="fill" aria-hidden="true" />,
        label: durationMs ? formatDuration(durationMs) : 'done',
        border: 'border-[var(--color-success)]/20',
        pill: 'bg-[var(--color-success)]/10 text-[var(--color-success)]',
      }
    case 'error':
      return {
        icon: <XCircle size={13} className="text-[var(--color-error)]" weight="fill" aria-hidden="true" />,
        label: durationMs ? formatDuration(durationMs) : 'failed',
        border: 'border-[var(--color-error)]/20',
        pill: 'bg-[var(--color-error)]/10 text-[var(--color-error)]',
      }
    case 'cancelled':
      return {
        icon: <Prohibit size={13} className="text-[var(--color-cancelled)]" weight="fill" aria-hidden="true" />,
        label: 'cancelled',
        border: 'border-[var(--color-cancelled)]/20',
        pill: 'bg-[var(--color-cancelled)]/10 text-[var(--color-cancelled)]',
      }
    case 'interrupted':
      return {
        icon: <Prohibit size={13} className="text-[var(--color-muted)]" weight="fill" aria-hidden="true" />,
        label: 'interrupted',
        border: 'border-[var(--color-muted)]/20',
        pill: 'bg-[var(--color-muted)]/10 text-[var(--color-muted)]',
      }
  }
}

// ── Step count text ───────────────────────────────────────────────────────────

function stepCountText(count: number): string {
  if (count === 1) return '1 step'
  return `${count} steps`
}

// ── SubagentBlock ─────────────────────────────────────────────────────────────

export interface SubagentBlockProps {
  span: SubagentSpan
}

export function SubagentBlock({ span }: SubagentBlockProps) {
  const [expanded, setExpanded] = useState(false)
  const isTerminal = span.status !== 'running'

  const config = getStatusConfig(span.status, span.durationMs)
  const label = deriveLabel(span)
  const stepCount = span.steps.length
  const hasFinalResult = Boolean(span.finalResult)

  function toggle() {
    setExpanded((e) => !e)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLButtonElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggle()
    }
  }

  return (
    <div
      className={cn(
        'mt-2 rounded-md border bg-[var(--color-surface-1)] text-xs font-mono overflow-hidden',
        config.border,
      )}
    >
      {/* Collapsed header — FR-H-008 */}
      <button
        type="button"
        data-testid="subagent-collapsed"
        onClick={toggle}
        onKeyDown={handleKeyDown}
        aria-expanded={expanded}
        aria-label={`Subagent: ${label}, ${stepCountText(stepCount)}, status ${span.status}`}
        className="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--color-surface-2)] cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-accent)]/50"
      >
        {/* Subagent icon */}
        <UserCircle size={13} className="text-[var(--color-muted)] shrink-0" aria-hidden="true" />

        {/* Task label */}
        <span className="text-[var(--color-secondary)] font-medium truncate flex-1 min-w-0">
          {label}
        </span>

        {/* Step count */}
        <span className="text-[var(--color-muted)] shrink-0 tabular-nums">
          {stepCountText(stepCount)}
        </span>

        {/* Status indicator */}
        <span className={cn('flex items-center gap-1 shrink-0 rounded px-1.5 py-0.5', config.pill)}>
          {config.icon}
          <span>{config.label}</span>
        </span>

        {/* Duration — only shown in terminal state */}
        {isTerminal && span.durationMs != null && (
          <span className="text-[var(--color-muted)] shrink-0 tabular-nums">
            {formatDuration(span.durationMs)}
          </span>
        )}

        {/* Caret */}
        <span className="ml-auto text-[var(--color-muted)] shrink-0">
          {expanded ? <CaretUp size={12} aria-hidden="true" /> : <CaretDown size={12} aria-hidden="true" />}
        </span>
      </button>

      {/* Expanded body — FR-H-008 */}
      {expanded && (
        <div
          data-testid="subagent-expanded"
          className="border-t border-[var(--color-border)] px-3 py-2 space-y-1"
          style={{ maxHeight: '400px', overflowY: 'auto' }}
        >
          {span.steps.length === 0 && !hasFinalResult && (
            <p className="text-[var(--color-muted)] text-[11px] py-1">No steps recorded.</p>
          )}

          {/* Nested tool call badges — in arrival order */}
          {span.steps.map((step) => (
            <ToolCallBadge key={step.call_id} toolCall={step} />
          ))}

          {/* Final result section — visually distinguishable from tool calls */}
          {hasFinalResult && (
            <div className="mt-2 rounded border border-[var(--color-success)]/30 bg-[var(--color-surface-2)] px-3 py-2">
              <div className="text-[var(--color-muted)] mb-1 text-[10px] uppercase tracking-wide font-sans">
                Final result
              </div>
              <pre className="text-[10px] text-[var(--color-secondary)] whitespace-pre-wrap break-all font-mono">
                {span.finalResult}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
