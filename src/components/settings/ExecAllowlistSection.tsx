import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Terminal, Plus, Trash, FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { fetchExecAllowlist, updateExecAllowlist } from '@/lib/api'
import { useUiStore } from '@/store/ui'

export function ExecAllowlistSection(): React.ReactElement {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['exec-allowlist'],
    queryFn: fetchExecAllowlist,
  })

  const [patterns, setPatterns] = useState<string[]>([])
  const [isDirty, setIsDirty] = useState(false)
  const [newPattern, setNewPattern] = useState('')
  const [addError, setAddError] = useState('')
  const [restartRequired, setRestartRequired] = useState(false)

  useEffect(() => {
    if (!data || isDirty) return
    // Pull from the server response — the source of truth for what's on disk.
    setPatterns(data.allowed_binaries ?? [])
    // Reset input/error state when the query refreshes externally so the
    // component doesn't carry stale add-errors across refetches.
    setNewPattern('')
    setAddError('')
  }, [data, isDirty])

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: () => updateExecAllowlist(patterns),
    onSuccess: (serverResp) => {
      // Trust the server response — it may have normalised (trimmed, deduped)
      // the patterns we submitted. Using local state would cause UI drift.
      setPatterns(serverResp.allowed_binaries ?? [])
      setIsDirty(false)
      setNewPattern('')
      setAddError('')
      // restartRequired is sticky: only upgrade, never downgrade. The GET
      // endpoint returns restart_required=false which would otherwise
      // clobber the badge on the next refetch.
      if (serverResp.restart_required) {
        setRestartRequired(true)
      }
      // Seed the cache with the mutation response rather than invalidating;
      // invalidation triggers a GET that returns restart_required=false and
      // races with our local state.
      queryClient.setQueryData(['exec-allowlist'], serverResp)
      addToast({
        message: serverResp.restart_required
          ? 'Allowlist saved — restart Omnipus for changes to take effect'
          : 'Binary allowlist saved',
        variant: 'success',
      })
    },
    onError: (err: Error) =>
      addToast({ message: err.message, variant: 'error' }),
  })

  function handleAdd() {
    const trimmed = newPattern.trim()
    if (!trimmed) {
      setAddError('Pattern cannot be empty.')
      return
    }
    if (patterns.includes(trimmed)) {
      setAddError('Pattern already exists.')
      return
    }
    setPatterns((prev) => [...prev, trimmed])
    setNewPattern('')
    setAddError('')
    setIsDirty(true)
  }

  function handleRemove(pattern: string) {
    setPatterns((prev) => prev.filter((p) => p !== pattern))
    setIsDirty(true)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleAdd()
    }
  }

  if (isLoading) {
    return (
      <div className="text-sm text-[var(--color-muted)] py-2">
        Loading allowlist...
      </div>
    )
  }

  if (isError) {
    return (
      <p className="text-sm text-red-400">
        Failed to load exec allowlist. Please try again.
      </p>
    )
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
            <Terminal size={14} className="text-[var(--color-muted)]" />
            Command Binary Allowlist
            {restartRequired && (
              <span className="ml-2 text-[10px] uppercase tracking-wider text-[var(--color-warning)] border border-[var(--color-warning)]/40 bg-[var(--color-warning)]/10 rounded px-1.5 py-0.5">
                Restart required
              </span>
            )}
          </h3>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Glob patterns for binaries that exec may run.
            E.g. <span className="font-mono">git *</span>,{' '}
            <span className="font-mono">npm run *</span>. When non-empty, exec
            denies any command that does not match a pattern.
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
        {patterns.length === 0 ? (
          <p className="text-xs text-[var(--color-muted)] italic">
            No patterns configured. Exec runs without the binary allowlist restriction
            (existing deny-pattern safety checks still apply).
          </p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {patterns.map((pattern) => (
              <div
                key={pattern}
                className="flex items-center gap-1.5 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-2)] px-2.5 py-1"
              >
                <Badge
                  variant="secondary"
                  className="font-mono text-xs px-0 py-0 bg-transparent border-0 text-[var(--color-secondary)]"
                >
                  {pattern}
                </Badge>
                <button
                  type="button"
                  aria-label={`Remove pattern ${pattern}`}
                  onClick={() => handleRemove(pattern)}
                  className="text-[var(--color-muted)] hover:text-[var(--color-error)] transition-colors"
                >
                  <Trash size={12} />
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-center gap-2 pt-1">
          <Input
            value={newPattern}
            onChange={(e) => {
              setNewPattern(e.target.value)
              setAddError('')
            }}
            onKeyDown={handleKeyDown}
            placeholder="e.g. git or npm run *"
            className="h-7 text-xs font-mono flex-1"
            aria-label="New binary pattern"
          />
          <Button
            size="sm"
            variant="outline"
            onClick={handleAdd}
            className="h-7 px-2 gap-1 text-xs shrink-0"
          >
            <Plus size={11} weight="bold" />
            Add
          </Button>
        </div>
        {addError && (
          <p className="text-xs text-[var(--color-error)]">{addError}</p>
        )}
      </div>
    </section>
  )
}
