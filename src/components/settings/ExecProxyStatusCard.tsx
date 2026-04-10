import { useQuery } from '@tanstack/react-query'
import { Globe, ArrowsClockwise, Warning, CheckCircle, XCircle } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { fetchExecProxyStatus } from '@/lib/api'

// ── Status indicator ──────────────────────────────────────────────────────────

type StatusVariant = 'running' | 'disabled' | 'stopped'

function resolveStatus(enabled: boolean, running: boolean): StatusVariant {
  if (!enabled) return 'disabled'
  if (running) return 'running'
  return 'stopped'
}

const STATUS_CONFIG: Record<
  StatusVariant,
  { label: string; dotColor: string; textColor: string; Icon: typeof CheckCircle }
> = {
  running: {
    label: 'Running',
    dotColor: 'var(--color-success)',
    textColor: 'var(--color-success)',
    Icon: CheckCircle,
  },
  disabled: {
    label: 'Disabled',
    dotColor: 'var(--color-muted)',
    textColor: 'var(--color-muted)',
    Icon: XCircle,
  },
  stopped: {
    label: 'Stopped',
    dotColor: 'var(--color-warning)',
    textColor: 'var(--color-warning)',
    Icon: Warning,
  },
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ExecProxyStatusCard(): React.ReactElement {
  const {
    data,
    isLoading,
    isError,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['exec-proxy-status'],
    queryFn: fetchExecProxyStatus,
    refetchInterval: 10_000,
  })

  const status = data ? resolveStatus(data.enabled, data.running) : null
  const cfg = status ? STATUS_CONFIG[status] : null

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
          <Globe size={14} className="text-[var(--color-muted)]" />
          Exec HTTP Proxy
        </h3>
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-2 gap-1 text-xs"
          aria-label="Refresh exec proxy status"
          onClick={() => { void refetch() }}
          disabled={isFetching}
        >
          <ArrowsClockwise
            size={11}
            className={isFetching ? 'animate-spin' : undefined}
          />
        </Button>
      </div>

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3">
        {isLoading ? (
          <p className="text-sm text-[var(--color-muted)]">Checking...</p>
        ) : isError ? (
          <div className="flex items-center gap-2">
            <XCircle size={14} style={{ color: 'var(--color-error)' }} />
            <p className="text-sm text-[var(--color-error)]">
              Failed to reach backend
            </p>
          </div>
        ) : data && cfg ? (
          <>
            {/* Status row */}
            <div className="flex items-center gap-2">
              <span
                className="inline-block w-2 h-2 rounded-full flex-shrink-0"
                style={{ backgroundColor: cfg.dotColor }}
                aria-hidden="true"
              />
              <span className="text-sm font-medium" style={{ color: cfg.textColor }}>
                {cfg.label}
              </span>
              {data.running && data.address && (
                <span className="font-mono text-xs text-[var(--color-muted)] ml-1">
                  {data.address}
                </span>
              )}
            </div>

            {/* Contextual message */}
            {status === 'disabled' && (
              <p className="text-xs text-[var(--color-muted)]">
                Enable via{' '}
                <span className="font-mono">config.tools.exec.enable_proxy</span>
              </p>
            )}
            {status === 'stopped' && (
              <div className="flex items-start gap-2 rounded-md border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/8 p-2.5">
                <Warning
                  size={13}
                  weight="fill"
                  className="flex-shrink-0 mt-0.5"
                  style={{ color: 'var(--color-warning)' }}
                />
                <p className="text-xs leading-relaxed" style={{ color: 'var(--color-warning)' }}>
                  Proxy failed to start — exec commands are not SSRF-protected.
                </p>
              </div>
            )}
          </>
        ) : null}

        {/* Description */}
        <p className="text-[10px] text-[var(--color-muted)] leading-relaxed border-t border-[var(--color-border)] pt-2">
          Routes exec tool child process HTTP/HTTPS traffic through an SSRF-protected loopback proxy (SEC-28).
        </p>
      </div>
    </section>
  )
}
