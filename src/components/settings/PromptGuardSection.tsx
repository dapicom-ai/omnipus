import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Shield, FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { fetchPromptGuard, updatePromptGuard } from '@/lib/api'
import type { PromptGuardStrictness } from '@/lib/api'
import { useUiStore } from '@/store/ui'

// ── Level metadata ────────────────────────────────────────────────────────────

const LEVELS: {
  value: PromptGuardStrictness
  label: string
  description: string
}[] = [
  {
    value: 'low',
    label: 'Low',
    description:
      'Wraps untrusted tool output in [UNTRUSTED_CONTENT] delimiters. Fewest false positives, lowest protection.',
  },
  {
    value: 'medium',
    label: 'Medium',
    description:
      '(Default) Wraps untrusted content AND escapes known injection phrases like "ignore previous instructions" using zero-width characters.',
  },
  {
    value: 'high',
    label: 'High',
    description:
      'Replaces all untrusted content with a placeholder. Highest protection but may lose context — consider using a summarization step before passing to the main agent.',
  },
]

// ── Component ─────────────────────────────────────────────────────────────────

export function PromptGuardSection(): React.ReactElement {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['prompt-guard'],
    queryFn: fetchPromptGuard,
  })

  const [selected, setSelected] = useState<PromptGuardStrictness>('medium')
  const [isDirty, setIsDirty] = useState(false)
  // restartRequired is "sticky": once a save returns restart_required=true, it
  // stays true until the page is reloaded. The GET endpoint currently returns
  // restart_required=false on every read, so we MUST NOT let the useEffect
  // clobber a true value — that would make the badge flash and disappear on
  // every refetch. The badge represents "pending restart for unsaved-to-runtime
  // state", not a server-side property.
  const [restartRequired, setRestartRequired] = useState(false)

  useEffect(() => {
    if (!data || isDirty) return
    setSelected(data.strictness)
    // Only UPGRADE restartRequired based on GET — never downgrade.
    if (data.restart_required) {
      setRestartRequired(true)
    }
  }, [data, isDirty])

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: () => updatePromptGuard(selected),
    onSuccess: (serverResp) => {
      // Trust the server response — it is the source of truth for strictness.
      setSelected(serverResp.strictness)
      setIsDirty(false)
      if (serverResp.restart_required) {
        setRestartRequired(true)
      }
      // Update the query cache directly with the mutation response instead of
      // invalidating: the GET endpoint returns restart_required=false, which
      // would otherwise race with our local state and clear the badge.
      queryClient.setQueryData(['prompt-guard'], serverResp)
      addToast({
        message: serverResp.restart_required
          ? 'Prompt guard level saved — restart Omnipus for changes to take effect'
          : 'Prompt injection defense level saved',
        variant: 'success',
      })
    },
    onError: (err: Error) =>
      addToast({ message: err.message, variant: 'error' }),
  })

  if (isLoading) {
    return (
      <div className="text-sm text-[var(--color-muted)] py-2">
        Loading prompt guard settings...
      </div>
    )
  }

  if (isError) {
    return (
      <p className="text-sm text-red-400">
        Failed to load prompt injection settings. Please try again.
      </p>
    )
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
            <Shield size={14} className="text-[var(--color-muted)]" />
            Prompt Injection Defense
            {restartRequired && (
              <span className="ml-2 text-[10px] uppercase tracking-wider text-[var(--color-warning)] border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 rounded px-1.5 py-0.5">
                Restart required
              </span>
            )}
          </h3>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Controls how untrusted tool output is sanitised before passing to the agent.
          </p>
        </div>
        {isDirty && (
          <Button
            size="sm"
            onClick={() => doSave()}
            disabled={isSaving}
            className="gap-1.5 shrink-0"
          >
            <FloppyDisk size={13} weight="bold" />
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
        )}
      </div>

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3">
        {/* Level selector */}
        <div className="space-y-2" role="radiogroup" aria-label="Prompt injection defense level">
          {LEVELS.map((level) => {
            const isActive = selected === level.value
            return (
              <button
                key={level.value}
                type="button"
                role="radio"
                aria-checked={isActive}
                onClick={() => {
                  if (selected !== level.value) {
                    setSelected(level.value)
                    setIsDirty(true)
                  }
                }}
                className={[
                  'w-full text-left rounded-md border p-3 transition-colors',
                  isActive
                    ? 'border-[var(--color-accent)]/60 bg-[var(--color-accent)]/8'
                    : 'border-[var(--color-border)] bg-[var(--color-surface-2)] hover:border-[var(--color-border-hover)]',
                ].join(' ')}
              >
                <div className="flex items-center gap-2">
                  {/* Radio indicator */}
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
                      isActive
                        ? 'text-[var(--color-secondary)]'
                        : 'text-[var(--color-muted)]',
                    ].join(' ')}
                  >
                    {level.label}
                  </span>
                </div>
                {isActive && (
                  <p className="text-xs text-[var(--color-muted)] mt-1.5 ml-5 leading-relaxed">
                    {level.description}
                  </p>
                )}
              </button>
            )
          })}
        </div>

        {/* Applies-to note */}
        <p className="text-[10px] text-[var(--color-muted)] pt-1 leading-relaxed">
          Applies to results from:{' '}
          <span className="font-mono">web_search</span>,{' '}
          <span className="font-mono">web_fetch</span>,{' '}
          <span className="font-mono">browser_*</span>,{' '}
          <span className="font-mono">read_file</span>
        </p>
      </div>
    </section>
  )
}
