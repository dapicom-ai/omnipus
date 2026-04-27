/**
 * RunInWorkspaceUI — AssistantUI tool component for the `run_in_workspace` tool.
 *
 * Spec: FR-008a / CR-03 / FR-013 / FR-014.
 *
 * Warmup is required: the dev server started by run_in_workspace needs time to
 * bind. IframePreview handles the warmup state machine; this component is
 * responsible only for extracting the typed result and passing props.
 */

import { useQuery } from '@tanstack/react-query'
import { makeAssistantToolUI } from '@assistant-ui/react'
import { Terminal } from '@phosphor-icons/react'
import type { RunInWorkspaceResult } from '@/lib/api'
import { fetchAboutInfo } from '@/lib/api'
import { hasPreviewShape } from '@/lib/preview-url'
import { IframePreview } from '../IframePreview'
import { PreviewToolHeader } from './PreviewToolHeader'
import { cn } from '@/lib/utils'

// ── Args / result type guard ──────────────────────────────────────────────────

interface RunInWorkspaceArgs {
  path?: string
  command?: string
  port?: number
  duration_seconds?: number
}

function isRunInWorkspaceResult(value: unknown): value is RunInWorkspaceResult {
  return (
    hasPreviewShape(value) &&
    typeof (value as Record<string, unknown>).command === 'string' &&
    typeof (value as Record<string, unknown>).port === 'number'
  )
}

// ── Block component ───────────────────────────────────────────────────────────

function RunInWorkspaceBlock({
  args,
  result,
  isRunning,
}: {
  args: RunInWorkspaceArgs
  result: unknown
  isRunning: boolean
}) {
  const { data: aboutInfo } = useQuery({
    queryKey: ['about'],
    queryFn: fetchAboutInfo,
    staleTime: 5 * 60 * 1000,
  })

  const typedResult = isRunInWorkspaceResult(result) ? result : null

  const command = typedResult?.command ?? args.command ?? ''
  const port = typedResult?.port ?? args.port

  const portChip = port !== undefined ? (
    <span className="text-[var(--color-muted)] font-mono">:{port}</span>
  ) : undefined

  return (
    <div className="mt-2 text-xs">
      <PreviewToolHeader
        icon={
          <Terminal
            size={13}
            className={cn(
              isRunning
                ? 'text-[var(--color-accent)] animate-pulse'
                : typedResult
                ? 'text-[var(--color-success)]'
                : 'text-[var(--color-error)]',
            )}
          />
        }
        toolName="run_in_workspace"
        label={command || undefined}
        trailing={portChip}
        isRunning={isRunning}
        hasResult={typedResult !== null}
      />

      {/* IframePreview handles the warmup state machine and iframe rendering */}
      <IframePreview
        kind="run_in_workspace"
        result={typedResult}
        warmupTimeoutSeconds={aboutInfo?.warmup_timeout_seconds}
      />
    </div>
  )
}

// ── AssistantUI registration ──────────────────────────────────────────────────

export const RunInWorkspaceUI = makeAssistantToolUI<RunInWorkspaceArgs, unknown>({
  toolName: 'run_in_workspace',
  render: ({ args, result, status }) => (
    <RunInWorkspaceBlock
      args={args ?? {}}
      result={result}
      isRunning={status.type === 'running'}
    />
  ),
})
