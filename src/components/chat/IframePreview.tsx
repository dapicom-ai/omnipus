/**
 * IframePreview — shared iframe chrome component used by ServeWorkspaceUI and
 * RunInWorkspaceUI.
 *
 * Spec references: FR-010 / FR-010a / FR-010b / FR-011 / FR-012 / FR-012a /
 * FR-012b / FR-013 / FR-014 / FR-015 / FR-019.
 *
 * URL construction: uses buildIframeURL from preview-url.ts.
 * Preview config: sourced from /api/v1/about via TanStack Query (['about']).
 * Toast system: useUiStore().addToast (existing pattern).
 *
 * Warmup schedule note (FR-013 / MN-02):
 *   The probe schedule is driven by a single setInterval that fires every 2 s,
 *   independent of whether the previous probe completed. Each interval tick
 *   re-mounts the probe iframe with a new React `key` (= fresh element, not a
 *   src swap) so onload/onerror fire against a fresh navigation. This satisfies
 *   MN-02: "schedule is fixed — probe N starts at t=(N-1)*2s regardless of
 *   probe N-1's actual completion."
 *   An individual probe times out at 1.8 s (no onload within that window).
 */

import { useCallback, useEffect, useRef, useState, type SyntheticEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowsClockwise, ArrowSquareOut, Copy } from '@phosphor-icons/react'
import { buildIframeURL } from '@/lib/preview-url'
import { fetchAboutInfo } from '@/lib/api'
import type { ServeWorkspaceResult, RunInWorkspaceResult } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { cn } from '@/lib/utils'

// ── Types ─────────────────────────────────────────────────────────────────────

export type IframePreviewProps =
  | { kind: 'serve_workspace'; result: ServeWorkspaceResult | null; warmupTimeoutSeconds?: number }
  | { kind: 'run_in_workspace'; result: RunInWorkspaceResult | null; warmupTimeoutSeconds?: number }
  | { kind: 'web_serve'; result: ServeWorkspaceResult | null; warmupTimeoutSeconds?: number }

// Internal warmup state machine phases.
type WarmupPhase = 'starting' | 'probing' | 'ready' | 'error'

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Minimal shape accepted by extractPath — both result types satisfy this. */
type PreviewResult = { path?: string; url?: string } | null

/**
 * Extracts the preview path from a tool result.
 *
 * FR-019 replay safety: if `path` is absent but `url` is present (legacy
 * transcript), parse the URL and extract pathname+search+hash as the path.
 * If the legacy URL is malformed, returns null (triggers link-only fallback).
 */
function extractPath(result: PreviewResult): string | null {
  if (!result) return null

  if (result.path) return result.path

  if (result.url) {
    try {
      const parsed = new URL(result.url)
      const path = parsed.pathname + parsed.search + parsed.hash
      return path || null
    } catch (err) {
      console.warn('preview.legacy_url_parse_failed', { url: result.url, err })
      return null
    }
  }

  return null
}

// ── Link-only fallback ────────────────────────────────────────────────────────

/**
 * Validates that a URL string uses only http: or https: schemes.
 * Returns true only when the URL is parseable and its scheme is safe.
 * Rejects javascript:, data:, and any other non-http(s) scheme (F-10).
 */
function isSafeHref(href: string): boolean {
  try {
    const parsed = new URL(href)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function LinkOnlyFallback({ href, label }: { href: string; label: string }) {
  const safe = isSafeHref(href)

  return (
    <div className="mt-2 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] px-3 py-2 text-xs">
      <span className="text-[var(--color-muted)]">{label}: </span>
      {safe ? (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-[var(--color-accent)] underline underline-offset-2 hover:opacity-80 font-mono break-all"
        >
          {href}
        </a>
      ) : (
        <span className="text-[var(--color-muted)] italic">
          Cannot render link — invalid scheme:{' '}
          <code className="font-mono text-[10px] bg-[var(--color-surface-2)] px-1 rounded break-all">
            {href}
          </code>
        </span>
      )}
    </div>
  )
}

// ── Error block ───────────────────────────────────────────────────────────────

function ErrorBlock({
  message,
  href,
  onRetry,
}: {
  message: string
  href?: string
  onRetry?: () => void
}) {
  return (
    <div className="mt-2 rounded-md border border-[var(--color-error)]/30 bg-[var(--color-error)]/5 px-3 py-2 text-xs space-y-1.5">
      <p className="text-[var(--color-error)]">{message}</p>
      {href && (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          className="text-[var(--color-accent)] underline underline-offset-2 hover:opacity-80 font-mono break-all block"
        >
          {href}
        </a>
      )}
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          aria-label="Retry warmup"
          className="mt-1 flex items-center gap-1 px-2 py-1 rounded border border-[var(--color-border)] text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors"
        >
          <ArrowsClockwise size={11} />
          Retry
        </button>
      )}
    </div>
  )
}

// ── Chrome bar ────────────────────────────────────────────────────────────────

function ChromeBar({
  absoluteUrl,
  toolName,
  phase,
  cacheBusterRef,
  onReloadOrRetry,
}: {
  absoluteUrl: string
  toolName: string
  phase: WarmupPhase
  cacheBusterRef: React.MutableRefObject<number>
  onReloadOrRetry: () => void
}) {
  const { addToast } = useUiStore()

  const isReady = phase === 'ready'
  const reloadLabel = isReady ? 'Reload preview' : 'Retry'

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(absoluteUrl)
      addToast({
        message: 'Link copied. Anyone with this link can view the preview until it expires.',
        variant: 'success',
      })
    } catch (err) {
      console.warn('preview.copy_failed', err)
      if (err instanceof Error && err.name === 'NotAllowedError') {
        addToast({
          message: 'Clipboard access denied (HTTPS or user permission required)',
          variant: 'error',
        })
      } else {
        addToast({ message: 'Could not copy link to clipboard', variant: 'error' })
      }
    }
  }

  function handleOpen() {
    window.open(absoluteUrl, '_blank', 'noopener,noreferrer')
  }

  function handleReload() {
    cacheBusterRef.current = Date.now()
    onReloadOrRetry()
  }

  return (
    <div className="flex items-center gap-1.5 px-2 py-1 bg-[var(--color-surface-2)] border-b border-[var(--color-border)] rounded-t-md">
      {/* Tool label */}
      <span className="text-[10px] text-[var(--color-muted)] font-mono flex-1 truncate">
        {toolName}
      </span>

      {/* Reload / Retry */}
      <button
        type="button"
        onClick={handleReload}
        aria-label={reloadLabel}
        title={reloadLabel}
        className="p-1 rounded text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-3)] transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"
      >
        <ArrowsClockwise size={13} />
      </button>

      {/* Open in new tab */}
      <button
        type="button"
        onClick={handleOpen}
        aria-label="Open preview in new tab"
        title="Open in new tab"
        className="p-1 rounded text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-3)] transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"
      >
        <ArrowSquareOut size={13} />
      </button>

      {/* Copy link */}
      <button
        type="button"
        onClick={handleCopy}
        aria-label="Copy preview link"
        title="Copy link"
        className="p-1 rounded text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-3)] transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]"
      >
        <Copy size={13} />
      </button>
    </div>
  )
}

// ── Warmup placeholder ────────────────────────────────────────────────────────

function WarmupPlaceholder({ toolName }: { toolName: string }) {
  return (
    <div
      aria-live="polite"
      className="flex items-center gap-2 px-4 py-6 text-sm text-[var(--color-muted)]"
    >
      <span className="flex gap-1">
        <span
          className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce"
          style={{ animationDelay: '0ms' }}
        />
        <span
          className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce"
          style={{ animationDelay: '150ms' }}
        />
        <span
          className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce"
          style={{ animationDelay: '300ms' }}
        />
      </span>
      <span>
        Starting {toolName === 'run_in_workspace' ? 'dev server' : 'preview'}…
      </span>
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export function IframePreview(props: IframePreviewProps) {
  const { kind, result, warmupTimeoutSeconds } = props
  const isWarmupRequired = kind === 'run_in_workspace'
  const toolName = kind
  const { data: aboutInfo, isLoading: aboutIsLoading } = useQuery({
    queryKey: ['about'],
    queryFn: fetchAboutInfo,
    staleTime: 5 * 60 * 1000,
  })

  // Warmup state machine
  const [warmupPhase, setWarmupPhase] = useState<WarmupPhase>(
    isWarmupRequired ? 'starting' : 'ready'
  )
  // probeKey triggers a fresh probe iframe mount each cycle (MN-02)
  const [probeKey, setProbeKey] = useState(0)
  // iframeKey triggers a fresh visible iframe mount (reload action)
  const [iframeKey, setIframeKey] = useState(0)
  // cacheBuster is appended to the visible iframe src on reload
  const cacheBusterRef = useRef<number>(Date.now())

  // F-38: forceFetchRef gates the cache-buster query string. Only the
  // user-triggered Reload action sets this to true; warmup probe re-mounts
  // do NOT, so probes can share the browser cache and avoid redundant round-trips.
  const forceFetchRef = useRef(false)

  // F-7: visible iframe load error state
  const [iframeFailed, setIframeFailed] = useState(false)

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const probeTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const probeCountRef = useRef(0)

  // F-6: Track the current probe id to discard stale onload/onerror events
  // from probes that were unmounted due to a Retry action.
  const currentProbeIdRef = useRef(0)

  // F-37: Count consecutive probe errors for fast-fail. Reset on any successful
  // probe load. When >= 3, short-circuit the warmup state machine to the error
  // phase without waiting for the full polling timeout.
  const consecutiveErrorsRef = useRef(0)

  // F-6: Guard against state updates after unmount (event-loop callbacks
  // that arrive after the interval cleanup may still reference closed-over
  // setters — this ref lets them exit early).
  const mountedRef = useRef(true)
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const effectiveTimeout = warmupTimeoutSeconds ?? aboutInfo?.warmup_timeout_seconds ?? 60
  const maxProbes = Math.floor(effectiveTimeout / 2)

  // Build the iframe URL
  const path = extractPath(result)

  // F-3: previewPort must not fall back to 80 — that would silently place the
  // iframe on the same origin as the SPA, allowing it to access parent storage.
  // Use null so URL construction is gated on the about query resolving.
  const previewPort = aboutInfo?.preview_port ?? null
  const previewOrigin = aboutInfo?.preview_origin
  const hostname = window.location.hostname
  const protocol = window.location.protocol

  const urlResult =
    path && previewPort !== null
      ? buildIframeURL({ path, previewOrigin, previewPort, hostname, protocol })
      : null

  const iframeUrl = urlResult && 'url' in urlResult ? urlResult.url : null
  const absoluteUrl = iframeUrl
  const buildError = urlResult && 'error' in urlResult ? urlResult.error : null

  // ── Warmup polling (FR-013 / FR-014) ─────────────────────────────────────

  const stopPolling = useCallback(() => {
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current)
      intervalRef.current = null
    }
    if (probeTimeoutRef.current !== null) {
      clearTimeout(probeTimeoutRef.current)
      probeTimeoutRef.current = null
    }
  }, [])

  const startPolling = useCallback(() => {
    if (!isWarmupRequired || !absoluteUrl) return
    stopPolling()
    probeCountRef.current = 0
    setWarmupPhase('probing')
    // F-6: increment probe id so any in-flight events from the previous probe
    // sequence are ignored. We use a functional updater here so the ref is set
    // to the *actual* next key value without needing to read state.
    setProbeKey((k) => {
      const next = k + 1
      currentProbeIdRef.current = next
      return next
    })

    intervalRef.current = setInterval(() => {
      if (!mountedRef.current) return
      probeCountRef.current += 1
      if (probeCountRef.current >= maxProbes) {
        stopPolling()
        setWarmupPhase('error')
        console.warn('preview.warmup_timeout', { tool: toolName, path })
        return
      }
      setProbeKey((k) => {
        const next = k + 1
        currentProbeIdRef.current = next
        return next
      })
    }, 2000)
  }, [isWarmupRequired, absoluteUrl, maxProbes, stopPolling, toolName, path])

  // Start polling when component mounts (if warmup required).
  // The effect re-runs when absoluteUrl resolves (after the about query loads),
  // which is the earliest point we can start probing — absoluteUrl is null
  // until aboutInfo arrives, satisfying F-3.
  useEffect(() => {
    if (isWarmupRequired && absoluteUrl && warmupPhase === 'starting') {
      startPolling()
    }
    // stopPolling clears both interval and probeTimeout (F-6 unmount cleanup)
    return () => stopPolling()
    // Run once on mount; absoluteUrl stabilises after the about query resolves
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [absoluteUrl])

  // F-6: Read the probe-id stamped on the iframe element and discard the event
  // if it belongs to a probe that was superseded by a Retry action.
  function handleProbeLoad(e: SyntheticEvent<HTMLIFrameElement>) {
    if (!mountedRef.current) return
    const probeId = Number((e.currentTarget as HTMLIFrameElement).dataset.probeId ?? -1)
    if (probeId !== currentProbeIdRef.current) return // stale event — ignore
    // F-37: successful probe resets the consecutive error counter.
    consecutiveErrorsRef.current = 0
    if (warmupPhase === 'probing' || warmupPhase === 'starting') {
      stopPolling()
      setWarmupPhase('ready')
    }
  }

  function handleProbeError(e: SyntheticEvent<HTMLIFrameElement>) {
    if (!mountedRef.current) return
    const probeId = Number((e.currentTarget as HTMLIFrameElement).dataset.probeId ?? -1)
    if (probeId !== currentProbeIdRef.current) return // stale event — ignore
    // Probe failed — clear the per-probe timeout so the next interval tick can
    // mount the next probe cleanly without a redundant timeout racing it.
    if (probeTimeoutRef.current !== null) {
      clearTimeout(probeTimeoutRef.current)
      probeTimeoutRef.current = null
    }
    // F-37: fast-fail after 3 consecutive probe errors rather than waiting for
    // the full polling timeout — the dev server is clearly not responding.
    consecutiveErrorsRef.current += 1
    if (consecutiveErrorsRef.current >= 3) {
      stopPolling()
      setWarmupPhase('error')
      console.warn('preview.warmup_fast_fail', { errors: consecutiveErrorsRef.current, tool: toolName, path })
    }
  }

  // Per-probe 1.8 s timeout — if onload hasn't fired, cancel this probe's
  // pending timeout so the interval tick can mount the next one cleanly.
  useEffect(() => {
    if (warmupPhase !== 'probing') return
    probeTimeoutRef.current = setTimeout(() => {
      probeTimeoutRef.current = null
      // The interval will fire in at most 200 ms and mount the next probe
    }, 1800)
    return () => {
      if (probeTimeoutRef.current !== null) {
        clearTimeout(probeTimeoutRef.current)
        probeTimeoutRef.current = null
      }
    }
  }, [probeKey, warmupPhase])

  // ── Reload / Retry handler ────────────────────────────────────────────────

  function handleReloadOrRetry() {
    if (warmupPhase === 'ready') {
      // Reload: re-mount visible iframe with fresh cache-buster; do NOT restart polling.
      // F-38: set forceFetch so the cache-buster is included in the new src.
      forceFetchRef.current = true
      cacheBusterRef.current = Date.now()
      setIframeFailed(false) // F-7: clear any previous visible-iframe error
      setIframeKey((k) => k + 1)
    } else {
      // Retry: reset warmup state machine.
      // F-6: stopPolling() is called inside startPolling(), but we also need to
      // pre-bump currentProbeIdRef *before* any stale onload from the old probe
      // sequence can be processed. startPolling() will do the actual bump when
      // it calls setProbeKey(), but we defensively increment here too so the
      // window between stopPolling() and setProbeKey() is safe.
      currentProbeIdRef.current += 1
      setWarmupPhase('starting')
      probeCountRef.current = 0
      if (absoluteUrl) startPolling()
    }
  }

  // ── Visible iframe onerror ────────────────────────────────────────────────

  function handleIframeError() {
    // F-7: Surface the error visibly instead of leaving a blank white box.
    console.warn('preview.iframe_error', { tool: toolName, path })
    setIframeFailed(true)
  }

  function handleIframeLoad() {
    // F-7: A successful load clears any previous error state (dev server recovered).
    if (iframeFailed) setIframeFailed(false)
  }

  // ── Render guards ─────────────────────────────────────────────────────────

  // No result yet (tool still running)
  if (!result) {
    return (
      <div className="mt-2 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] text-xs px-3 py-2 text-[var(--color-muted)]">
        Waiting for {toolName}…
      </div>
    )
  }

  // F-3: Never mount an iframe until the about query has resolved and given us
  // a real preview_port. Without this guard, previewPort would fall back to
  // null (no URL built), but an interim render could still race a stale URL.
  // Showing a placeholder until aboutInfo arrives is cheap and closes T-01.
  if (aboutIsLoading || !aboutInfo) {
    return (
      <div className="mt-2 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] text-xs px-3 py-2 text-[var(--color-muted)] flex items-center gap-2">
        <span
          className="inline-block w-3 h-3 rounded-full border-2 border-[var(--color-accent)] border-t-transparent animate-spin"
          aria-hidden="true"
        />
        Loading preview…
      </div>
    )
  }

  // buildIframeURL returned scheme-mismatch error
  if (buildError === 'scheme-mismatch') {
    return (
      <ErrorBlock
        message="Cannot embed preview: the preview origin uses a different scheme (HTTP/HTTPS mismatch). Contact your administrator."
      />
    )
  }

  // F-31: buildIframeURL returned misconfigured-origin — operator deployment
  // problem (gateway.preview_origin is unparseable). Distinct from invalid-path
  // (corrupt tool result) — actionable message directs the user to an admin.
  if (buildError === 'misconfigured-origin') {
    return (
      <div className="mt-2 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs space-y-1.5">
        <p className="text-amber-400">Preview origin is misconfigured. Contact your administrator.</p>
      </div>
    )
  }

  // buildIframeURL returned invalid-path error — link-only fallback (FR-015).
  // Also fires when extractPath returned null but a legacy URL exists: this
  // covers legacy transcripts whose URL has an unparseable shape.
  if (buildError === 'invalid-path' || (!absoluteUrl && result.url)) {
    console.warn('preview.invalid_path', { tool: toolName, path })
    const legacyUrl = result.url ?? ''
    return <LinkOnlyFallback href={legacyUrl} label="Preview" />
  }

  // No URL at all and no legacy fallback
  if (!absoluteUrl) {
    return (
      <div className="mt-2 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] text-xs px-3 py-2 text-[var(--color-muted)]">
        Preview URL unavailable.
      </div>
    )
  }

  // F-38: include the cache-buster only when a force-fetch was requested (i.e.
  // user clicked Reload). Warmup re-mounts omit it so the browser cache is shared.
  const shouldBust = forceFetchRef.current
  if (shouldBust) forceFetchRef.current = false // check-and-clear
  const visibleIframeSrc = shouldBust
    ? `${absoluteUrl}?_=${cacheBusterRef.current}`
    : absoluteUrl

  return (
    <div className="mt-2 rounded-md border border-[var(--color-border)] overflow-hidden">
      {/* Chrome bar */}
      <ChromeBar
        absoluteUrl={absoluteUrl}
        toolName={toolName}
        phase={warmupPhase}
        cacheBusterRef={cacheBusterRef}
        onReloadOrRetry={handleReloadOrRetry}
      />

      {/* Hidden probe iframe (warmup only) */}
      {/* F-6: data-probe-id lets handleProbeLoad/handleProbeError discard stale events */}
      {isWarmupRequired && warmupPhase === 'probing' && (
        <iframe
          key={probeKey}
          src={absoluteUrl}
          title="probe"
          aria-hidden="true"
          className="hidden"
          data-probe-id={probeKey}
          // FR-011 sandbox
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals"
          onLoad={handleProbeLoad}
          onError={handleProbeError}
        />
      )}

      {/* Warmup placeholder */}
      {isWarmupRequired && (warmupPhase === 'starting' || warmupPhase === 'probing') && (
        <div className="bg-[var(--color-surface-1)] min-h-[240px] flex items-center justify-center">
          <WarmupPlaceholder toolName={toolName} />
        </div>
      )}

      {/* Warmup timeout error */}
      {isWarmupRequired && warmupPhase === 'error' && (
        <div className="bg-[var(--color-surface-1)] min-h-[120px] flex items-center justify-center p-4">
          <ErrorBlock
            message="Dev server did not respond in time. It may still be starting."
            href={absoluteUrl}
            onRetry={handleReloadOrRetry}
          />
        </div>
      )}

      {/* Visible iframe — F-7: replaced by error block if load fails */}
      {(!isWarmupRequired || warmupPhase === 'ready') && (
        iframeFailed ? (
          <div className="bg-[var(--color-surface-1)] min-h-[120px] flex items-center justify-center p-4">
            <ErrorBlock
              message="Preview failed to load. The dev server may have crashed or the path may be invalid."
              href={absoluteUrl}
              onRetry={() => {
                setIframeFailed(false)
                handleReloadOrRetry()
              }}
            />
          </div>
        ) : (
          <iframe
            key={iframeKey}
            src={visibleIframeSrc}
            title={`${toolName} preview`}
            className={cn(
              'w-full border-0 bg-white',
              'min-h-[400px]',
            )}
            // FR-011 sandbox — NO allow-top-navigation, NO allow-popups-to-escape-sandbox
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals"
            onLoad={handleIframeLoad}
            onError={handleIframeError}
          />
        )
      )}
    </div>
  )
}
