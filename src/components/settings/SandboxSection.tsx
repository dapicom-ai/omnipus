import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ShieldCheck,
  ShieldWarning,
  Shield,
  ArrowsClockwise,
  CaretDown,
  CaretUp,
  XCircle,
  Cpu,
  PencilSimple,
  CheckCircle,
  ArrowCounterClockwise,
  Warning,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import {
  fetchSandboxStatus,
  fetchSandboxConfig,
  updateSandboxConfig,
} from '@/lib/api'
import type { SandboxStatus, SandboxConfig, SandboxConfigUpdate } from '@/lib/api'
import { useAuthStore } from '@/store/auth'
import { useUiStore } from '@/store/ui'

// ── Status dot ────────────────────────────────────────────────────────────────

type DotVariant = 'green' | 'amber' | 'red'

function StatusDot({ variant }: { variant: DotVariant }) {
  const colors: Record<DotVariant, string> = {
    green: 'var(--color-success)',
    amber: 'var(--color-warning)',
    red: 'var(--color-error)',
  }
  return (
    <span
      className="inline-block w-2 h-2 rounded-full flex-shrink-0"
      style={{ backgroundColor: colors[variant] }}
      aria-hidden="true"
    />
  )
}

// ── Capability badge ──────────────────────────────────────────────────────────

function Badge({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-mono border border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-secondary)]">
      {children}
    </span>
  )
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

function SandboxSkeleton() {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3 animate-pulse">
      <div className="flex items-center gap-2">
        <div className="w-2 h-2 rounded-full bg-[var(--color-border)]" />
        <div className="h-4 w-32 rounded bg-[var(--color-border)]" />
      </div>
      <div className="h-3 w-full rounded bg-[var(--color-border)]" />
      <div className="h-3 w-2/3 rounded bg-[var(--color-border)]" />
    </div>
  )
}

// ── Capabilities detail ───────────────────────────────────────────────────────

function CapabilitiesPanel({ data }: { data: SandboxStatus }) {
  const hasFeatures = data.landlock_features && data.landlock_features.length > 0
  const hasSyscalls = data.blocked_syscalls && data.blocked_syscalls.length > 0

  return (
    <div className="border-t border-[var(--color-border)] pt-3 space-y-3 mt-3">
      <p className="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted)]">
        Capabilities
      </p>

      {/* ABI version */}
      {data.abi_version != null && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-[var(--color-muted)]">Landlock ABI version</span>
          <Badge>{data.abi_version}</Badge>
        </div>
      )}

      {/* Landlock features */}
      {hasFeatures && (
        <div className="space-y-1.5">
          <span className="text-xs text-[var(--color-muted)]">Landlock features</span>
          <div className="flex flex-wrap gap-1">
            {data.landlock_features!.map((f) => (
              <Badge key={f}>{f}</Badge>
            ))}
          </div>
        </div>
      )}

      {/* Seccomp */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">Seccomp-BPF</span>
        <span
          className="text-xs font-medium"
          style={{ color: data.seccomp_enabled ? 'var(--color-success)' : 'var(--color-muted)' }}
        >
          {data.seccomp_enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      {/* Blocked syscalls */}
      {hasSyscalls && (
        <div className="space-y-1.5">
          <span className="text-xs text-[var(--color-muted)]">
            Blocked syscalls ({data.blocked_syscalls!.length})
          </span>
          <div
            className="flex flex-wrap gap-1 max-h-28 overflow-y-auto pr-1"
            style={{ scrollbarWidth: 'thin' }}
          >
            {data.blocked_syscalls!.map((sc) => (
              <Badge key={sc}>{sc}</Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export function SandboxSection(): React.ReactElement {
  const [expanded, setExpanded] = useState(false)
  const [editing, setEditing] = useState(false)
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['sandbox-status'],
    queryFn: fetchSandboxStatus,
  })

  // Editable sandbox config — only fetched when the operator is an admin
  // AND has opened the editor. Keeps the read-only path unaffected.
  const {
    data: config,
    isLoading: configLoading,
    refetch: refetchConfig,
  } = useQuery<SandboxConfig>({
    queryKey: ['sandbox-config'],
    queryFn: fetchSandboxConfig,
    enabled: isAdmin && editing,
  })

  // Local draft — diverges from `config` once the operator touches a
  // control. On Save we send only the changed fields (pointer-null
  // semantics on the server side).
  const [draftMode, setDraftMode] = useState<SandboxConfigUpdate['mode']>()

  const { mutate: doSave, isPending: saving } = useMutation({
    mutationFn: updateSandboxConfig,
    onSuccess: (saved) => {
      queryClient.setQueryData(['sandbox-config'], saved)
      addToast({
        message: saved.requires_restart
          ? 'Sandbox config saved — restart the gateway to apply.'
          : 'Sandbox config saved.',
        variant: saved.requires_restart ? 'default' : 'success',
      })
      setDraftMode(undefined)
    },
    onError: (err: Error) =>
      addToast({ message: err.message, variant: 'error' }),
  })

  const savedMode = config?.mode
  const effectiveDraftMode = draftMode ?? savedMode
  const restartPending = !!(
    config &&
    config.applied_mode !== '' &&
    config.mode !== config.applied_mode
  )

  // Derived display values — computed with explicit branches rather than
  // nested ternaries for readability.
  function resolveDotVariant(): DotVariant {
    if (!data) return 'red'
    if (data.kernel_level) return 'green'
    if (data.available) return 'amber'
    return 'red'
  }

  function resolveBackendLabel(): string {
    if (!data) return 'Unknown'
    return data.kernel_level ? data.backend : 'Application fallback'
  }

  function resolveHeaderIcon(): typeof Shield {
    if (data?.kernel_level) return ShieldCheck
    if (data?.available) return ShieldWarning
    return Shield
  }

  function resolveDescription(): string | null {
    if (!data) return null
    if (data.kernel_level) {
      return 'Child processes are restricted at the kernel level using Linux Landlock and seccomp-BPF. This provides strong isolation.'
    }
    return 'Kernel-level sandboxing is unavailable on this platform. Falling back to cooperative environment-variable enforcement — uncooperative binaries are NOT contained.'
  }

  const dotVariant = resolveDotVariant()
  const backendLabel = resolveBackendLabel()
  const HeaderIcon = resolveHeaderIcon()
  const description = resolveDescription()
  const backendColor = data?.kernel_level ? 'var(--color-accent)' : 'var(--color-muted)'

  // Capabilities panel is only meaningful when we have data and it has detail
  const hasCapabilities = !!(
    data &&
    (data.abi_version != null ||
      (data.landlock_features && data.landlock_features.length > 0) ||
      (data.blocked_syscalls && data.blocked_syscalls.length > 0) ||
      data.seccomp_enabled)
  )

  // Render the body in one of four states: loading, error, loaded, or empty.
  // Extracted to avoid nested ternaries in JSX.
  function renderBody(): React.ReactNode {
    if (isLoading) return <SandboxSkeleton />

    if (isError) {
      const errorDetail = error instanceof Error ? error.message : undefined
      return (
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 flex items-start gap-2">
          <XCircle size={14} style={{ color: 'var(--color-error)' }} className="mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-sm text-[var(--color-error)]">
              Failed to load sandbox status
            </p>
            {errorDetail && (
              <p className="mt-0.5 text-xs font-mono text-[var(--color-muted)] break-words">
                {errorDetail}
              </p>
            )}
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-xs shrink-0"
            onClick={() => { void refetch() }}
            disabled={isFetching}
          >
            Retry
          </Button>
        </div>
      )
    }

    if (!data) return null

    return (
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4">
        {/* Backend indicator */}
        <div className="flex items-center gap-2 mb-3">
          <StatusDot variant={dotVariant} />
          <HeaderIcon
            size={14}
            style={{ color: data.kernel_level ? 'var(--color-accent)' : 'var(--color-muted)' }}
            weight="duotone"
          />
          <span
            className="text-sm font-semibold font-mono"
            style={{ color: backendColor }}
          >
            {backendLabel}
          </span>
        </div>

        {/* Description */}
        {description && (
          <p className="text-xs text-[var(--color-muted)] leading-relaxed">
            {description}
          </p>
        )}

        {/* Status notes — surfaces mismatches between capability and
            enforcement (e.g. Landlock-capable but Apply() never called). */}
        {data.notes && data.notes.length > 0 && (
          <div className="mt-2 rounded-md border border-[var(--color-warning)]/30 bg-[var(--color-warning)]/5 p-2 space-y-1">
            {data.notes.map((note, i) => (
              <p key={i} className="text-[10px] text-[var(--color-warning)] leading-relaxed">
                <span className="font-semibold">Note:</span> {note}
              </p>
            ))}
          </div>
        )}

        {/* Collapsible capabilities */}
        {hasCapabilities && (
          <>
            <button
              type="button"
              onClick={() => setExpanded((e) => !e)}
              className="mt-3 flex items-center gap-1 text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] transition-colors"
              aria-expanded={expanded}
            >
              {expanded ? <CaretUp size={10} /> : <CaretDown size={10} />}
              {expanded ? 'Hide capabilities' : 'Show capabilities'}
            </button>
            {expanded && <CapabilitiesPanel data={data} />}
          </>
        )}
      </div>
    )
  }

  // ── Editor body ─────────────────────────────────────────────────────
  function renderEditor(): React.ReactNode {
    if (configLoading || !config) {
      return <SandboxSkeleton />
    }
    const modes: Array<{ value: 'enforce' | 'permissive' | 'off'; label: string; desc: string }> = [
      { value: 'enforce', label: 'Enforce', desc: 'Kernel-level Landlock + seccomp denies violating syscalls.' },
      { value: 'permissive', label: 'Permissive', desc: 'Policy computed and logged; violations not blocked (audit-only).' },
      { value: 'off', label: 'Off', desc: 'Sandbox disabled. Development only; production banner will fire.' },
    ]
    return (
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
        {/* Restart banner — fires when a saved change hasn't taken effect yet. */}
        {restartPending && (
          <div
            className="flex items-start gap-2 rounded-md border p-2.5"
            style={{
              borderColor: 'rgba(234,179,8,0.35)',
              backgroundColor: 'rgba(234,179,8,0.08)',
            }}
            role="status"
          >
            <Warning size={14} weight="fill" style={{ color: 'var(--color-warning)' }} className="mt-0.5 shrink-0" />
            <p className="text-xs leading-relaxed text-[var(--color-secondary)]">
              <span className="font-semibold" style={{ color: 'var(--color-warning)' }}>
                Restart required.
              </span>{' '}
              Saved mode is <code className="font-mono">{config.mode}</code> but the
              gateway is currently running with{' '}
              <code className="font-mono">{config.applied_mode || 'none'}</code>.
              Restart the gateway for the change to take effect.
            </p>
          </div>
        )}

        {/* Mode radio group */}
        <fieldset className="space-y-2">
          <legend className="text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
            Sandbox mode
          </legend>
          {modes.map((m) => (
            <label
              key={m.value}
              className={`flex items-start gap-2 p-2 rounded-md border cursor-pointer transition-colors ${
                effectiveDraftMode === m.value
                  ? 'border-[var(--color-accent)]/50 bg-[var(--color-accent)]/5'
                  : 'border-[var(--color-border)] hover:bg-[var(--color-surface-2)]'
              }`}
            >
              <input
                type="radio"
                name="sandbox-mode"
                value={m.value}
                checked={effectiveDraftMode === m.value}
                onChange={() => setDraftMode(m.value)}
                className="mt-0.5 accent-[var(--color-accent)]"
                aria-label={`Sandbox mode: ${m.label}`}
              />
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium text-[var(--color-secondary)]">{m.label}</p>
                <p className="text-xs text-[var(--color-muted)] leading-snug">{m.desc}</p>
              </div>
            </label>
          ))}
        </fieldset>

        {/* Save / Cancel */}
        <div className="flex items-center justify-end gap-2">
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-3 text-xs"
            onClick={() => {
              setDraftMode(undefined)
              setEditing(false)
            }}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            className="h-7 px-3 text-xs gap-1"
            disabled={saving || !draftMode || draftMode === savedMode}
            onClick={() => {
              if (draftMode) doSave({ mode: draftMode })
            }}
          >
            {saving ? (
              <>
                <ArrowCounterClockwise size={11} className="animate-spin" />
                Saving
              </>
            ) : (
              <>
                <CheckCircle size={11} />
                Save
              </>
            )}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-[var(--color-secondary)] flex items-center gap-1.5">
          <Cpu size={14} className="text-[var(--color-muted)]" />
          Process Sandbox
        </h3>
        <div className="flex items-center gap-2">
          {/* Edit button — admin-only. Opens the editor beneath the status card. */}
          {isAdmin && !editing && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 px-2 gap-1 text-xs"
              aria-label="Edit sandbox configuration"
              onClick={() => setEditing(true)}
            >
              <PencilSimple size={11} />
              Edit
            </Button>
          )}
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 gap-1 text-xs"
            aria-label="Refresh sandbox status"
            onClick={() => {
              void refetch()
              if (editing) void refetchConfig()
            }}
            disabled={isFetching}
          >
            <ArrowsClockwise
              size={11}
              className={isFetching ? 'animate-spin' : undefined}
            />
          </Button>
        </div>
      </div>

      {renderBody()}
      {isAdmin && editing && renderEditor()}
    </section>
  )
}
