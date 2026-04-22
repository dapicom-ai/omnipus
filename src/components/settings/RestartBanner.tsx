import { useQueryClient } from '@tanstack/react-query'
import { ArrowsClockwise } from '@phosphor-icons/react'
import { usePendingRestart, PENDING_RESTART_QUERY_KEY } from '@/store/restart'
import { useAuthStore } from '@/store/auth'
import type { PendingRestartEntry } from '@/lib/api'

// formatValue produces a human-readable transition string for a config value.
// Objects and arrays are represented as "(modified)" to avoid unreadable JSON blobs.
function formatValue(value: unknown): string {
  if (value === null || value === undefined) return 'null'
  if (typeof value === 'object') return '(modified)'
  return String(value)
}

function EntryRow({ entry }: { entry: PendingRestartEntry }) {
  const from = formatValue(entry.applied_value)
  const to = formatValue(entry.persisted_value)
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs font-mono">
      <span className="text-[var(--color-secondary)] font-semibold">{entry.key}:</span>
      <span className="text-[var(--color-muted)]">{from}</span>
      <span className="text-[var(--color-muted)]">→</span>
      <span className="text-[var(--color-secondary)]">{to}</span>
    </div>
  )
}

// RestartBannerInner is only mounted when the user is admin — this avoids
// making a /pending-restart request for non-admin users at all.
function RestartBannerInner() {
  const queryClient = useQueryClient()
  const { entries, isLoading, isError } = usePendingRestart()

  // Loading first fetch: render nothing to avoid flicker.
  if (isLoading && entries.length === 0) return null

  // Error (includes 403 from server, 503 for dev_mode_bypass):
  // treat as "not available" and hide.
  if (isError) return null

  // Empty diff: no changes pending, hide banner.
  if (entries.length === 0) return null

  function handleManualRefetch() {
    void queryClient.invalidateQueries({ queryKey: [...PENDING_RESTART_QUERY_KEY] })
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 mb-6"
    >
      {/* Header row */}
      <div className="flex items-start justify-between gap-2 flex-wrap">
        <div>
          <p className="text-sm font-semibold text-amber-300">Changes pending restart</p>
          <p className="text-xs text-amber-300/70 mt-0.5">
            Restart the gateway process for these changes to take effect.
          </p>
        </div>
        <button
          type="button"
          onClick={handleManualRefetch}
          className="shrink-0 text-amber-300/60 hover:text-amber-300 transition-colors"
          aria-label="Refresh pending restart status"
        >
          <ArrowsClockwise size={14} />
        </button>
      </div>

      {/* Per-key diff rows */}
      <div className="mt-2 space-y-1">
        {entries.map((entry) => (
          <EntryRow key={entry.key} entry={entry} />
        ))}
      </div>

      {/* Helper text */}
      <p className="mt-2 text-[11px] text-amber-300/50 leading-relaxed">
        To apply these changes, restart via your process supervisor (systemd / docker / launchd / etc.).
        This banner will clear automatically after restart.
      </p>
    </div>
  )
}

// RestartBanner renders a persistent amber banner at the top of the Settings
// screen whenever the gateway has config changes that require a restart.
//
// Visibility conditions (all must hold):
//   - User role is "admin"
//   - /api/v1/config/pending-restart returned a non-empty array (no error)
//
// The banner is purely data-driven: it auto-hides when the diff empties
// (set-then-revert or post-restart). There is no dismiss button.
export function RestartBanner() {
  const role = useAuthStore((s) => s.role)

  // Gate on admin role before mounting the inner component so we never
  // issue a /pending-restart request for non-admin users.
  if (role !== 'admin') return null

  return <RestartBannerInner />
}
