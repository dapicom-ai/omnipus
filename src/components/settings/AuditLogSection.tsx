import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { BookOpen } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { fetchAuditLogToggle, updateAuditLog } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'

// ── Skeleton ──────────────────────────────────────────────────────────────────

function Skeleton() {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3 animate-pulse">
      <div className="h-4 w-40 rounded bg-[var(--color-border)]" />
      <div className="h-3 w-64 rounded bg-[var(--color-border)]" />
    </div>
  )
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AuditLogSection(): React.ReactElement {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['audit-log-toggle'],
    queryFn: fetchAuditLogToggle,
  })

  const [enabled, setEnabled] = useState(false)
  const [isDirty, setIsDirty] = useState(false)
  const [restartRequired, setRestartRequired] = useState(false)

  useEffect(() => {
    if (!data || isDirty) return
    setEnabled(data.enabled)
    // AuditLogToggle does not carry requires_restart — it comes from the PUT response only.
  }, [data, isDirty])

  const { mutate: save, isPending } = useMutation({
    mutationFn: (val: boolean) => updateAuditLog(val),
    onSuccess: (resp) => {
      setIsDirty(false)
      if (resp.requires_restart) setRestartRequired(true)
      queryClient.setQueryData(['audit-log-toggle'], { enabled: resp.applied_enabled, requires_restart: resp.requires_restart })
      addToast({ message: 'Audit log setting saved', variant: 'success' })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  if (isLoading) return <Skeleton />

  if (isError) {
    return (
      <p className="text-sm" style={{ color: 'var(--color-error)' }}>
        Failed to load audit log settings: {error instanceof Error ? error.message : 'Unknown error'}
      </p>
    )
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
          <BookOpen size={14} className="text-[var(--color-muted)]" />
          Audit Log
          {restartRequired && (
            <span className="ml-2 text-[10px] uppercase tracking-wider text-[var(--color-warning)] border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 rounded px-1.5 py-0.5">
              Restart required
            </span>
          )}
        </h3>
      </div>

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
        <p className="text-xs text-[var(--color-muted)] leading-relaxed">
          When enabled, all security-relevant events are written to the audit log file in{' '}
          <span className="font-mono">~/.omnipus/audit.log</span>.
        </p>

        <label className="flex items-center gap-3 cursor-pointer select-none">
          <input
            type="checkbox"
            role="switch"
            aria-label="Enable audit log"
            checked={enabled}
            disabled={!isAdmin}
            onChange={(e) => {
              setEnabled(e.target.checked)
              setIsDirty(true)
            }}
            className="w-4 h-4 accent-[var(--color-accent)] cursor-pointer disabled:cursor-not-allowed"
          />
          <span className="text-sm text-[var(--color-secondary)]">
            {enabled ? 'Enabled' : 'Disabled'}
          </span>
        </label>

        {isAdmin && (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="default"
              disabled={!isDirty || isPending}
              onClick={() => save(enabled)}
            >
              {isPending ? 'Saving...' : 'Save'}
            </Button>
          </div>
        )}
      </div>
    </section>
  )
}
