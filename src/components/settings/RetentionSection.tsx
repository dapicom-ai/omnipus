import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Clock, Broom, Warning } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import {
  fetchRetention,
  updateRetention,
  triggerRetentionSweep,
  retentionMode,
  isApiError,
} from '@/lib/api'
import type { RetentionUpdateBody, RetentionMode } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { SaveStatus, useSaveStatus } from './SaveStatus'

// ── Mode helpers ───────────────────────────────────────────────────────────────

function buildBody(mode: RetentionMode, customDays: number): RetentionUpdateBody {
  if (mode === 'default') return { session_days: 0, disabled: false }
  if (mode === 'custom') return { session_days: customDays, disabled: false }
  return { session_days: 0, disabled: true }
}

// ── Confirmation modal ────────────────────────────────────────────────────────

interface ConfirmModalProps {
  onConfirm: () => void
  onCancel: () => void
}

function ConfirmModal({ onConfirm, onCancel }: ConfirmModalProps) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Confirm disable retention"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
    >
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-5 max-w-sm w-full mx-4 space-y-4">
        <div className="flex items-start gap-2">
          <Warning size={18} weight="fill" className="mt-0.5 shrink-0" style={{ color: 'var(--color-warning)' }} />
          <div>
            <p className="text-sm font-medium text-[var(--color-secondary)]">
              Disable retention?
            </p>
            <p className="text-xs text-[var(--color-muted)] mt-1 leading-relaxed">
              This will let sessions accumulate indefinitely. Continue?
            </p>
          </div>
        </div>
        <div className="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button size="sm" variant="default" onClick={onConfirm}>
            Continue
          </Button>
        </div>
      </div>
    </div>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

function Skeleton() {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3 animate-pulse">
      <div className="h-4 w-40 rounded bg-[var(--color-border)]" />
      <div className="h-3 w-full rounded bg-[var(--color-border)]" />
      <div className="h-3 w-2/3 rounded bg-[var(--color-border)]" />
    </div>
  )
}

// ── Component ─────────────────────────────────────────────────────────────────

export function RetentionSection(): React.ReactElement {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['retention'],
    queryFn: fetchRetention,
  })

  const [mode, setMode] = useState<RetentionMode>('default')
  const [customDays, setCustomDays] = useState(30)
  const [showConfirm, setShowConfirm] = useState(false)
  const [isDisabledOnServer, setIsDisabledOnServer] = useState(false)
  // Track server mode for revert on cancel
  const [serverMode, setServerMode] = useState<RetentionMode>('default')

  const { state: saveState, setState: setSaveState, errorMessage, setErrorMessage } = useSaveStatus()

  useEffect(() => {
    if (!data) return
    const m = retentionMode(data)
    setMode(m)
    setServerMode(m)
    setIsDisabledOnServer(!!data.disabled)
    if (m === 'custom' && data.session_days && data.session_days > 0) {
      setCustomDays(data.session_days)
    }
  }, [data])

  const { mutate: save, isPending: isSaving } = useMutation({
    mutationFn: (body: RetentionUpdateBody) => updateRetention(body),
    onMutate: () => setSaveState('saving'),
    onSuccess: (resp) => {
      setSaveState('saved')
      setIsDisabledOnServer(!!resp.disabled)
      const m = retentionMode(resp)
      setServerMode(m)
      queryClient.setQueryData(['retention'], {
        session_days: resp.session_days,
        disabled: resp.disabled,
      })
    },
    onError: (err: Error) => {
      setSaveState('error')
      setErrorMessage(err.message)
      addToast({ message: err.message, variant: 'error' })
    },
  })

  const { mutate: sweep, isPending: isSweeping } = useMutation({
    mutationFn: triggerRetentionSweep,
    onSuccess: (resp) => {
      addToast({ message: `Sweep complete — ${resp.removed} session(s) removed`, variant: 'success' })
    },
    onError: (err: Error) => {
      // Defensively match both the typed status (409 from MaxBytes / conflict)
      // and the legacy "sweep in progress" body substring so the toast still
      // works against pre-ApiError servers.
      if (isApiError(err)) {
        if (err.status === 409 || err.userMessage.toLowerCase().includes('sweep in progress')) {
          addToast({ message: 'A sweep is already running — try again in a moment.', variant: 'error' })
        } else {
          addToast({ message: err.userMessage, variant: 'error' })
        }
      } else {
        addToast({ message: err.message, variant: 'error' })
      }
    },
  })

  function handleModeChange(m: RetentionMode) {
    if (m === mode) return
    setMode(m)
    if (m === 'forever') {
      // Intercept — show confirmation before saving
      setShowConfirm(true)
      return
    }
    save(buildBody(m, customDays))
  }

  function confirmDisable() {
    setShowConfirm(false)
    save(buildBody('forever', customDays))
  }

  function cancelDisable() {
    // Revert mode back to server value
    setMode(serverMode)
    setShowConfirm(false)
  }

  function handleCustomDaysBlur() {
    if (mode === 'custom') {
      save(buildBody('custom', customDays))
    }
  }

  if (isLoading) return <Skeleton />

  if (isError) {
    return (
      <p className="text-sm" style={{ color: 'var(--color-error)' }}>
        Failed to load retention settings:{' '}
        {error instanceof Error ? error.message : 'Unknown error'}
      </p>
    )
  }

  return (
    <>
      {showConfirm && (
        <ConfirmModal
          onConfirm={confirmDisable}
          onCancel={cancelDisable}
        />
      )}

      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
            <Clock size={14} className="text-[var(--color-muted)]" />
            Session Retention
          </h3>
          <SaveStatus state={saveState} errorMessage={errorMessage} />
        </div>

        {/* Persistent warning when retention is disabled on server */}
        {isDisabledOnServer && (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-md border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/8 p-3"
          >
            <Warning size={14} weight="fill" className="mt-0.5 shrink-0" style={{ color: 'var(--color-warning)' }} />
            <p className="text-xs leading-relaxed" style={{ color: 'var(--color-warning)' }}>
              Retention disabled — sessions will accumulate indefinitely.
            </p>
          </div>
        )}

        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <p className="text-xs text-[var(--color-muted)] leading-relaxed">
            Controls how long session transcripts are kept before the nightly sweep removes them.
          </p>

          {/* Mode selector */}
          <div className="space-y-2" role="radiogroup" aria-label="Retention mode">
            {(['default', 'custom', 'forever'] as const).map((m) => {
              const isActive = mode === m
              const labels: Record<RetentionMode, string> = {
                default: 'Default (90 days)',
                custom: 'Custom',
                forever: 'Disabled (keep forever)',
              }
              return (
                <button
                  key={m}
                  type="button"
                  role="radio"
                  aria-checked={isActive}
                  disabled={!isAdmin}
                  onClick={() => handleModeChange(m)}
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
                      {labels[m]}
                    </span>
                  </div>
                </button>
              )
            })}
          </div>

          {/* Custom days input */}
          {mode === 'custom' && (
            <div className="space-y-2 ml-1">
              <label className="text-xs font-medium text-[var(--color-secondary)]">
                Retention period (days)
              </label>
              <div className="flex items-center gap-3">
                <input
                  type="range"
                  min={1}
                  max={365}
                  value={customDays}
                  disabled={!isAdmin}
                  onChange={(e) => setCustomDays(Number(e.target.value))}
                  onMouseUp={handleCustomDaysBlur}
                  onTouchEnd={handleCustomDaysBlur}
                  className="flex-1 accent-[var(--color-accent)] disabled:opacity-60"
                  aria-label="Retention days slider"
                />
                <input
                  type="number"
                  min={1}
                  max={365}
                  value={customDays}
                  disabled={!isAdmin}
                  onChange={(e) => {
                    const v = Math.max(1, Math.min(365, Number(e.target.value)))
                    setCustomDays(v)
                  }}
                  onBlur={handleCustomDaysBlur}
                  className="w-16 rounded border border-[var(--color-border)] bg-[var(--color-surface-2)] px-2 py-1 text-sm text-[var(--color-secondary)] text-center disabled:opacity-60 disabled:cursor-not-allowed"
                  aria-label="Retention days number"
                />
                <span className="text-xs text-[var(--color-muted)] shrink-0">days</span>
              </div>
            </div>
          )}

          {/* Sweep button — admin only, hidden when disabled */}
          {isAdmin && !isDisabledOnServer && (
            <div className="pt-1">
              <Button
                size="sm"
                variant="ghost"
                disabled={isSweeping || isSaving}
                onClick={() => sweep()}
              >
                <Broom size={13} className="mr-1.5" />
                {isSweeping ? 'Running sweep...' : 'Run sweep now'}
              </Button>
            </div>
          )}
        </div>
      </section>
    </>
  )
}
