/**
 * ServeWorkspaceUI — AssistantUI tool component for the `serve_workspace` tool.
 *
 * Spec: FR-008 / FR-010 / FR-011 / FR-012 / FR-015 / FR-019.
 *
 * serve_workspace does NOT require warmup — the gateway's preview listener is
 * already bound when it issues the token. IframePreview mounts the iframe
 * immediately at phase 'ready'.
 */

import { makeAssistantToolUI } from '@assistant-ui/react'
import { Globe, CheckCircle, ArrowsClockwise, XCircle } from '@phosphor-icons/react'
import type { ServeWorkspaceResult } from '@/lib/api'
import { hasPreviewShape } from '@/lib/preview-url'
import { IframePreview } from '../IframePreview'
import { PreviewToolHeader } from './PreviewToolHeader'
import { cn } from '@/lib/utils'

// ── Args / result type guard ──────────────────────────────────────────────────

interface ServeWorkspaceArgs {
  path?: string
  duration_seconds?: number
}

function isServeWorkspaceResult(value: unknown): value is ServeWorkspaceResult {
  return (
    hasPreviewShape(value) &&
    typeof (value as Record<string, unknown>).expires_at === 'string'
  )
}

// ── Block component ───────────────────────────────────────────────────────────

function ServeWorkspaceBlock({
  args,
  result,
  isRunning,
}: {
  args: ServeWorkspaceArgs
  result: unknown
  isRunning: boolean
}) {
  const typedResult = isServeWorkspaceResult(result) ? result : null

  const statusIcon = isRunning ? (
    <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />
  ) : typedResult ? (
    <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />
  ) : (
    <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />
  )

  return (
    <div className="mt-2 text-xs">
      <PreviewToolHeader
        icon={
          <Globe
            size={13}
            weight="duotone"
            className={cn(
              isRunning
                ? 'text-[var(--color-accent)]'
                : typedResult
                ? 'text-[var(--color-success)]'
                : 'text-[var(--color-error)]',
            )}
          />
        }
        toolName="serve_workspace"
        label={args.path}
        trailing={statusIcon}
        isRunning={isRunning}
        hasResult={typedResult !== null}
      />

      {/* IframePreview — no warmup for serve_workspace */}
      <IframePreview
        kind="serve_workspace"
        result={typedResult}
      />
    </div>
  )
}

// ── AssistantUI registration ──────────────────────────────────────────────────

export const ServeWorkspaceUI = makeAssistantToolUI<ServeWorkspaceArgs, unknown>({
  toolName: 'serve_workspace',
  render: ({ args, result, status }) => (
    <ServeWorkspaceBlock
      args={args ?? {}}
      result={result}
      isRunning={status.type === 'running'}
    />
  ),
})
