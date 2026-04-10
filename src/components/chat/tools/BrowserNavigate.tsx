import { useState } from 'react'
import { makeAssistantToolUI } from '@assistant-ui/react'
import {
  Globe,
  ArrowsClockwise,
  CheckCircle,
  XCircle,
  CaretDown,
  CaretUp,
  Camera,
} from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

interface BrowserNavigateArgs {
  url?: string
  screenshot?: boolean
  wait_for?: string
}

interface BrowserResult {
  url?: string
  title?: string
  screenshot?: string // base64-encoded PNG
  content?: string
  error?: string
}

function displayUrl(url: string): string {
  try {
    const u = new URL(url)
    return u.hostname + (u.pathname !== '/' ? u.pathname : '')
  } catch {
    // URL parsing failed — display raw string. Expected for malformed URLs.
    return url
  }
}

function parseResult(result: unknown): BrowserResult {
  if (!result) return {}
  if (typeof result === 'string') {
    // Try JSON first
    try {
      return JSON.parse(result) as BrowserResult
    } catch {
      // Not JSON — display as plain content. Expected when backend returns text summary.
      return { content: result }
    }
  }
  if (typeof result === 'object') return result as BrowserResult
  return {}
}

function BrowserNavigateBlock({
  args,
  result,
  isRunning,
  isError,
}: {
  args: BrowserNavigateArgs
  result: unknown
  isRunning: boolean
  isError?: boolean
}) {
  const [expanded, setExpanded] = useState(false)

  const url = args.url ?? '(unknown URL)'
  const parsed = parseResult(result)
  const hasResult = result != null
  const screenshotData = parsed.screenshot
  const pageTitle = parsed.title
  const hasDetail = !isRunning && hasResult

  return (
    <div
      className={cn(
        'mt-2 rounded-md border overflow-hidden text-xs',
        isError && !isRunning
          ? 'border-[var(--color-error)]/30'
          : 'border-[var(--color-border)]'
      )}
    >
      {/* Header */}
      <button
        type="button"
        onClick={() => hasDetail && setExpanded((e) => !e)}
        className={cn(
          'flex w-full items-center gap-2 px-3 py-2 bg-[var(--color-surface-1)] transition-colors text-left',
          hasDetail && 'hover:bg-[var(--color-surface-2)] cursor-pointer',
          !hasDetail && 'cursor-default'
        )}
        aria-expanded={expanded}
        disabled={!hasDetail}
      >
        <Globe
          size={13}
          weight="duotone"
          className={cn(
            isRunning ? 'text-[var(--color-accent)]' :
            isError ? 'text-[var(--color-error)]' :
            'text-[var(--color-secondary)]'
          )}
        />
        <span className="text-[var(--color-muted)] shrink-0">browser.navigate</span>
        <span className="font-mono text-[var(--color-accent)] truncate flex-1 min-w-0 text-[10px]">
          {displayUrl(url)}
        </span>
        {pageTitle && !isRunning && (
          <span className="text-[var(--color-muted)] truncate max-w-[120px] text-[10px] hidden sm:inline">
            {pageTitle}
          </span>
        )}
        <span className="flex items-center gap-1 shrink-0">
          {isRunning ? (
            <ArrowsClockwise size={12} className="animate-spin text-[var(--color-accent)]" />
          ) : isError ? (
            <XCircle size={12} weight="fill" className="text-[var(--color-error)]" />
          ) : hasResult ? (
            <>
              <CheckCircle size={12} weight="fill" className="text-[var(--color-success)]" />
              {screenshotData && <Camera size={11} className="text-[var(--color-muted)]" />}
            </>
          ) : null}
          {hasDetail && (
            <span className="ml-1 text-[var(--color-muted)]">
              {expanded ? <CaretUp size={10} /> : <CaretDown size={10} />}
            </span>
          )}
        </span>
      </button>

      {/* Detail panel */}
      {expanded && hasDetail && (
        <div className="border-t border-[var(--color-border)]">
          {/* Full URL breadcrumb */}
          <div className="px-2 py-0.5 bg-[var(--color-surface-1)] border-b border-[var(--color-border)]">
            <span className="text-[10px] text-[var(--color-muted)] font-mono break-all">{url}</span>
          </div>

          {/* Screenshot thumbnail */}
          {screenshotData && (
            <div className="px-3 py-2 bg-[var(--color-surface-1)] border-b border-[var(--color-border)]">
              <div className="flex items-center gap-1.5 mb-1.5">
                <Camera size={11} className="text-[var(--color-muted)]" />
                <span className="text-[10px] text-[var(--color-muted)]">Screenshot</span>
              </div>
              <img
                src={`data:image/png;base64,${screenshotData}`}
                alt={`Screenshot of ${url}`}
                className="max-w-full rounded border border-[var(--color-border)] max-h-48 object-contain"
              />
            </div>
          )}

          {/* Page content preview */}
          {parsed.content && (
            <pre className="px-3 py-2 text-[10px] leading-5 text-[var(--color-secondary)] whitespace-pre-wrap break-all max-h-48 overflow-auto bg-[var(--color-surface-1)]">
              {parsed.content.slice(0, 2000)}
              {parsed.content.length > 2000 && (
                <span className="text-[var(--color-muted)] italic">
                  {'\n'}... (content truncated)
                </span>
              )}
            </pre>
          )}

          {/* Error */}
          {parsed.error && (
            <div className="px-3 py-2 text-[var(--color-error)] text-[10px]">{parsed.error}</div>
          )}
        </div>
      )}
    </div>
  )
}

export const BrowserNavigateUI = makeAssistantToolUI<BrowserNavigateArgs, unknown>({
  toolName: 'browser.navigate',
  render: ({ args, result, status }) => (
    <BrowserNavigateBlock
      args={args ?? {}}
      result={result}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})

// Underscore alias for the same tool
export const BrowserNavigateUnderscoreUI = makeAssistantToolUI<BrowserNavigateArgs, unknown>({
  toolName: 'browser_navigate',
  render: ({ args, result, status }) => (
    <BrowserNavigateBlock
      args={args ?? {}}
      result={result}
      isRunning={status.type === 'running'}
      isError={status.type === 'incomplete'}
    />
  ),
})
