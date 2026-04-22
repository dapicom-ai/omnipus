import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { BookOpen } from '@phosphor-icons/react'
import { fetchAuditLogToggle, updateAuditLog } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { SaveStatus, useSaveStatus } from './SaveStatus'

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
  const [restartRequired, setRestartRequired] = useState(false)

  const { state: saveState, setState: setSaveState, errorMessage, setErrorMessage } = useSaveStatus()

  useEffect(() => {
    if (!data) return
    setEnabled(data.enabled)
  }, [data])

  const { mutate: save } = useMutation({
    mutationFn: (val: boolean) => updateAuditLog(val),
    onMutate: () => setSaveState('saving'),
    onSuccess: (resp) => {
      setSaveState('saved')
      if (resp.requires_restart) setRestartRequired(true)
      queryClient.setQueryData(['audit-log-toggle'], { enabled: resp.applied_enabled, requires_restart: resp.requires_restart })
    },
    onError: (err: Error) => {
      setSaveState('error')
      setErrorMessage(err.message)
      addToast({ message: err.message, variant: 'error' })
    },
  })

  function handleChange(checked: boolean) {
    setEnabled(checked)
    save(checked)
  }

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
        <SaveStatus state={saveState} errorMessage={errorMessage} />
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
            onChange={(e) => handleChange(e.target.checked)}
            className="w-4 h-4 accent-[var(--color-accent)] cursor-pointer disabled:cursor-not-allowed"
          />
          <span className="text-sm text-[var(--color-secondary)]">
            {enabled ? 'Enabled' : 'Disabled'}
          </span>
        </label>
      </div>
    </section>
  )
}
