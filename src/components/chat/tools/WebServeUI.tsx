/**
 * WebServeUI — AssistantUI tool component for the `web_serve` tool.
 *
 * Handles both static-serve and dev-server modes based on the `kind` field
 * in the tool result. Also used as the canonical implementation backing the
 * back-compat aliases ServeWorkspaceUI and RunInWorkspaceUI.
 *
 * Spec: FR-008 / FR-008a / FR-010 / FR-011 / FR-012 / FR-013 / FR-014 / FR-015 / FR-019.
 *
 * kind="static": Globe icon + path label, iframe mounts immediately (no warmup).
 * kind="dev":    Terminal icon + command + port label, warmup state machine (3s grace).
 *
 * The `registerToolName` prop selects which tool name to pass to
 * makeAssistantToolUI — this lets the same component register as:
 *   web_serve        (new canonical name)
 *   serve_workspace  (back-compat alias)
 *   run_in_workspace (back-compat alias)
 */

import { makeAssistantToolUI } from '@assistant-ui/react'
import { Globe, Terminal, CheckCircle, ArrowsClockwise, XCircle } from '@phosphor-icons/react'
import { type ServeWorkspaceResult as ServeWorkspaceIframeResult, type RunInWorkspaceResult as RunInWorkspaceIframeResult } from '@/lib/api'
import { hasPreviewShape } from '@/lib/preview-url'
import { IframePreview } from '../IframePreview'
import { PreviewToolHeader } from './PreviewToolHeader'
import { cn } from '@/lib/utils'

// ── Result types ──────────────────────────────────────────────────────────────

/**
 * The result shape emitted by the `web_serve` tool.
 *
 * `kind` discriminates between the two modes. Back-compat: when replaying a
 * legacy `serve_workspace` or `run_in_workspace` transcript, `kind` may be
 * absent; we infer mode from the presence of `command` / `port` fields.
 */
export interface WebServeResult {
  /** Discriminator for the two modes. */
  kind?: 'static' | 'dev'
  /** Relative preview path, e.g. "/preview/<agent>/<token>/". */
  url: string
  /** ISO-8601 token expiry timestamp. */
  expires_at: string
  /** Dev-mode: the command that was executed (e.g. "vite dev"). */
  command?: string
  /** Dev-mode: the local port the dev server is listening on. */
  port?: number
  /** Static-mode: the workspace path that was served. */
  path?: string
}

interface WebServeArgs {
  path?: string
  command?: string
  port?: number
  duration_seconds?: number
}

/**
 * Infer effective kind from result, falling back to presence of command/port
 * for legacy transcript replay where `kind` was not emitted.
 */
function inferKind(result: WebServeResult): 'static' | 'dev' {
  if (result.kind === 'static' || result.kind === 'dev') return result.kind
  // Legacy back-compat: run_in_workspace results have command + port
  if (typeof result.command === 'string' && typeof result.port === 'number') return 'dev'
  return 'static'
}

function isWebServeResult(value: unknown): value is WebServeResult {
  if (!value || typeof value !== 'object') return false
  const v = value as Record<string, unknown>
  // New web_serve shape: has url + expires_at
  if (typeof v.url === 'string' && typeof v.expires_at === 'string') return true
  // Legacy serve_workspace / run_in_workspace shape: hasPreviewShape checks path + url
  return (
    hasPreviewShape(value) &&
    typeof (value as Record<string, unknown>).expires_at === 'string'
  )
}

// ── Block component ───────────────────────────────────────────────────────────

export function WebServeBlock({
  args,
  result,
  isRunning,
  toolName,
}: {
  args: WebServeArgs
  result: unknown
  isRunning: boolean
  toolName: string
}) {
  const typedResult = isWebServeResult(result) ? result : null
  const effectiveKind = typedResult ? inferKind(typedResult) : null

  // For dev mode: derive command and port from result or args
  const command = typedResult?.command ?? args.command ?? ''
  const port = typedResult?.port ?? args.port

  // For static mode: derive path label from result or args
  const pathLabel = typedResult?.path ?? args.path

  const isDevMode = effectiveKind === 'dev' ||
    (effectiveKind === null && (typeof args.command === 'string' || typeof args.port === 'number'))

  const statusIcon = isRunning ? (
    <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />
  ) : typedResult ? (
    <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />
  ) : (
    <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />
  )

  const portChip =
    isDevMode && port !== undefined ? (
      <span className="text-[var(--color-muted)] font-mono">:{port}</span>
    ) : undefined

  // IframePreview kind: map to the existing discriminated union.
  // For web_serve static → 'serve_workspace', dev → 'run_in_workspace'.
  // Back-compat aliases pass through their own kind directly via toolName inference.
  const iframeKind =
    isDevMode ? 'run_in_workspace' : 'serve_workspace'

  // Build the result shape expected by IframePreview — it uses path + url.
  // Pass path directly; IframePreview.extractPath falls back to url when
  // path is absent, so there is no need to duplicate the fallback logic here.
  const iframeResult = typedResult
    ? iframeKind === 'run_in_workspace'
      ? {
          path: typedResult.path,
          url: typedResult.url,
          expires_at: typedResult.expires_at,
          command: typedResult.command ?? command,
          port: typedResult.port ?? port ?? 0,
        }
      : {
          path: typedResult.path,
          url: typedResult.url,
          expires_at: typedResult.expires_at,
        }
    : null

  return (
    <div className="mt-2 text-xs">
      <PreviewToolHeader
        icon={
          isDevMode ? (
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
          ) : (
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
          )
        }
        toolName={toolName}
        label={isDevMode ? (command || undefined) : (pathLabel || undefined)}
        trailing={isDevMode ? portChip : statusIcon}
        isRunning={isRunning}
        hasResult={typedResult !== null}
      />

      {isDevMode ? (
        <IframePreview
          kind="run_in_workspace"
          result={iframeResult as RunInWorkspaceIframeResult | null}
        />
      ) : (
        <IframePreview
          kind="serve_workspace"
          result={iframeResult as ServeWorkspaceIframeResult | null}
        />
      )}
    </div>
  )
}

// ── Factory ───────────────────────────────────────────────────────────────────

/**
 * Creates a registered AssistantUI tool component for the given tool name.
 * The toolName is threaded into WebServeBlock so the header displays the
 * correct name for each alias.
 */
export function makeWebServeUI(toolName: string) {
  return makeAssistantToolUI<WebServeArgs, unknown>({
    toolName,
    render: ({ args, result, status }) => (
      <WebServeBlock
        args={args ?? {}}
        result={result}
        isRunning={status.type === 'running'}
        toolName={toolName}
      />
    ),
  })
}

// ── Canonical registration ────────────────────────────────────────────────────

export const WebServeUI = makeWebServeUI('web_serve')
