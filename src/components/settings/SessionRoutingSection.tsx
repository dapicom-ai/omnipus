import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowsSplit } from '@phosphor-icons/react'
import { fetchSessionScope, updateSessionScope } from '@/lib/api'
import type { DMScope } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { SaveStatus, useSaveStatus } from './SaveStatus'

// ── Scope metadata ────────────────────────────────────────────────────────────

const SCOPES: { value: DMScope; label: string; subtitle: string }[] = [
  {
    value: 'main',
    label: 'Main',
    subtitle: 'One session per agent across all DMs',
  },
  {
    value: 'per-peer',
    label: 'Per peer',
    subtitle: 'Separate session per sender identity',
  },
  {
    value: 'per-channel-peer',
    label: 'Per channel + peer',
    subtitle: 'Separate session per (channel, sender). Default.',
  },
  {
    value: 'per-account-channel-peer',
    label: 'Per account + channel + peer',
    subtitle:
      'Separate session per (bot account, channel, sender). Use when one bot serves multiple tenants.',
  },
]

// ── Skeleton ──────────────────────────────────────────────────────────────────

function Skeleton() {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3 animate-pulse">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="h-14 rounded bg-[var(--color-border)]" />
      ))}
    </div>
  )
}

// ── Component ─────────────────────────────────────────────────────────────────

export function SessionRoutingSection(): React.ReactElement {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['session-scope'],
    queryFn: fetchSessionScope,
  })

  const [selected, setSelected] = useState<DMScope>('per-channel-peer')
  const [restartRequired, setRestartRequired] = useState(false)

  const { state: saveState, setState: setSaveState, errorMessage, setErrorMessage } = useSaveStatus()

  useEffect(() => {
    if (!data) return
    setSelected(data.dm_scope)
  }, [data])

  const { mutate: save } = useMutation({
    mutationFn: (scope: DMScope) => updateSessionScope(scope),
    onMutate: () => setSaveState('saving'),
    onSuccess: (resp) => {
      setSaveState('saved')
      if (resp.requires_restart) setRestartRequired(true)
      queryClient.setQueryData(['session-scope'], { dm_scope: resp.applied_dm_scope })
    },
    onError: (err: Error) => {
      setSaveState('error')
      setErrorMessage(err.message)
      addToast({ message: err.message, variant: 'error' })
    },
  })

  function handleChange(scope: DMScope) {
    setSelected(scope)
    save(scope)
  }

  if (isLoading) return <Skeleton />

  if (isError) {
    return (
      <p className="text-sm" style={{ color: 'var(--color-error)' }}>
        Failed to load session routing settings:{' '}
        {error instanceof Error ? error.message : 'Unknown error'}
      </p>
    )
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
          <ArrowsSplit size={14} className="text-[var(--color-muted)]" />
          Session Routing
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
          Controls how DM conversation context is isolated. Changing this setting requires a
          gateway restart.
        </p>

        <div className="space-y-2" role="radiogroup" aria-label="Session routing scope">
          {SCOPES.map((sc) => {
            const isActive = selected === sc.value
            return (
              <button
                key={sc.value}
                type="button"
                role="radio"
                aria-checked={isActive}
                disabled={!isAdmin}
                onClick={() => {
                  if (selected !== sc.value) handleChange(sc.value)
                }}
                className={[
                  'w-full text-left rounded-md border p-3 transition-colors disabled:opacity-60 disabled:cursor-not-allowed',
                  isActive
                    ? 'border-[var(--color-accent)]/60 bg-[var(--color-accent)]/8'
                    : 'border-[var(--color-border)] bg-[var(--color-surface-2)] hover:border-[var(--color-border-hover)]',
                ].join(' ')}
              >
                <div className="flex items-center gap-2">
                  <span
                    className={[
                      'flex-shrink-0 inline-block w-3.5 h-3.5 rounded-full border-2 transition-colors',
                      isActive
                        ? 'border-[var(--color-accent)] bg-[var(--color-accent)]'
                        : 'border-[var(--color-border)]',
                    ].join(' ')}
                    aria-hidden="true"
                  />
                  <span
                    className={[
                      'text-sm font-medium',
                      isActive ? 'text-[var(--color-secondary)]' : 'text-[var(--color-muted)]',
                    ].join(' ')}
                  >
                    {sc.label}
                  </span>
                </div>
                <p className="text-xs text-[var(--color-muted)] mt-1 ml-5 leading-relaxed">
                  {sc.subtitle}
                </p>
              </button>
            )
          })}
        </div>
      </div>
    </section>
  )
}
