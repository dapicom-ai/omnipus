import { useState, useEffect, useRef, useCallback } from 'react'
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
  Trash,
  Plus,
  Warning,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  fetchSandboxStatus,
  fetchSandboxConfig,
  updateSandboxConfig,
} from '@/lib/api'
import type { SandboxStatus } from '@/lib/api'
import { useAuthStore } from '@/store/auth'

// ── Constants ─────────────────────────────────────────────────────────────────

const ABI4_BANNER_SESSION_KEY = 'sprint-k:abi4-banner-dismissed'

// SSRF preset definitions
const SSRF_PRESETS = [
  { label: 'Block all', list: [] as string[] },
  { label: 'Allow loopback only', list: ['127.0.0.1', '::1'] },
  {
    label: 'Allow RFC1918 + loopback',
    list: ['127.0.0.1', '::1', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7'],
  },
] as const

// SSRF entry validation — hostname/IP/CIDR check matching server-side rules.
function isValidSsrfEntry(entry: string): boolean {
  const trimmed = entry.trim()
  if (!trimmed) return false
  if (trimmed.includes('/')) {
    const slashIdx = trimmed.lastIndexOf('/')
    const ip = trimmed.slice(0, slashIdx)
    const prefixStr = trimmed.slice(slashIdx + 1)
    const prefixNum = parseInt(prefixStr, 10)
    if (isNaN(prefixNum) || prefixNum < 0 || prefixStr === '') return false
    const ipv4Re = /^(\d{1,3}\.){3}\d{1,3}$/
    if (ipv4Re.test(ip) && prefixNum <= 32) return true
    if (ip.includes(':') && prefixNum <= 128) return true
    return false
  }
  const ipv4Re = /^(\d{1,3}\.){3}\d{1,3}$/
  if (ipv4Re.test(trimmed)) return true
  if (trimmed.includes(':')) return true
  const hostnameRe = /^[A-Za-z0-9][A-Za-z0-9.-]*$/
  return hostnameRe.test(trimmed)
}

function isWildcardEntry(entry: string): boolean {
  return entry === '0.0.0.0/0' || entry === '::/0'
}

function listsMatch(a: string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false
  const sortedA = [...a].sort()
  const sortedB = [...b].sort()
  return sortedA.every((v, i) => v === sortedB[i])
}

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

function CapBadge({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-block rounded px-1.5 py-0.5 text-[10px] font-mono border border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-secondary)]">
      {children}
    </span>
  )
}

// Read-only badge with tooltip for allowed_paths rows
function ReadOnlyBadge() {
  const [tip, setTip] = useState(false)
  return (
    <span className="relative inline-block">
      <button
        type="button"
        className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-mono border border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-muted)] cursor-default"
        onMouseEnter={() => setTip(true)}
        onMouseLeave={() => setTip(false)}
        onFocus={() => setTip(true)}
        onBlur={() => setTip(false)}
        tabIndex={0}
        aria-describedby={tip ? 'ro-tip' : undefined}
      >
        read-only
      </button>
      {tip && (
        <span
          id="ro-tip"
          role="tooltip"
          className="absolute bottom-full left-0 mb-1 z-50 w-64 rounded border border-[var(--color-border)] bg-[var(--color-surface-1)] px-2 py-1.5 text-[10px] text-[var(--color-muted)] shadow-lg pointer-events-none"
        >
          AllowedPaths entries grant read-only access. Write access is never available via this editor.
        </span>
      )}
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

      {data.abi_version != null && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-[var(--color-muted)]">Landlock ABI version</span>
          <CapBadge>{data.abi_version}</CapBadge>
        </div>
      )}

      {hasFeatures && (
        <div className="space-y-1.5">
          <span className="text-xs text-[var(--color-muted)]">Landlock features</span>
          <div className="flex flex-wrap gap-1">
            {data.landlock_features!.map((f) => (
              <CapBadge key={f}>{f}</CapBadge>
            ))}
          </div>
        </div>
      )}

      <div className="flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">Seccomp-BPF</span>
        <span
          className="text-xs font-medium"
          style={{ color: data.seccomp_enabled ? 'var(--color-success)' : 'var(--color-muted)' }}
        >
          {data.seccomp_enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

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
              <CapBadge key={sc}>{sc}</CapBadge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// ── ABI v4 Banner (k25) ───────────────────────────────────────────────────────

function Abi4Banner({
  abiVersion,
  issueRef,
  onDismiss,
}: {
  abiVersion: number
  issueRef: string
  onDismiss: () => void
}) {
  return (
    <div
      role="alert"
      className="flex flex-col sm:flex-row sm:items-start gap-2 rounded-lg border border-yellow-500/40 bg-yellow-500/10 px-3 py-2.5"
    >
      <Warning size={14} className="mt-0.5 shrink-0 text-yellow-400" weight="fill" />
      <p className="flex-1 text-xs text-yellow-200 leading-relaxed">
        Your Linux kernel uses Landlock v{abiVersion}, which is not yet supported (issue {issueRef}).
        Enforce mode will exit with code 78 at boot. Use &lsquo;permissive&rsquo; or &lsquo;off&rsquo; until Landlock support is upgraded.
      </p>
      <button
        type="button"
        onClick={onDismiss}
        className="shrink-0 text-[10px] text-yellow-400 underline hover:text-yellow-300 focus:outline-none focus:ring-1 focus:ring-yellow-400 rounded"
      >
        Dismiss for session
      </button>
    </div>
  )
}

// ── Allowed Paths Editor (k23) ────────────────────────────────────────────────

interface AllowedPathsEditorProps {
  paths: string[]
  isEditing: boolean
  rowErrors: Record<number, string>
  restartedRows: Set<number>
  onDelete: (index: number) => void
  newPath: string
  onNewPathChange: (v: string) => void
  onAdd: () => void
  addError: string | null
}

function AllowedPathsEditor({
  paths,
  isEditing,
  rowErrors,
  restartedRows,
  onDelete,
  newPath,
  onNewPathChange,
  onAdd,
  addError,
}: AllowedPathsEditorProps) {
  return (
    <div className="space-y-2">
      <p className="text-xs font-semibold text-[var(--color-secondary)]">
        Filesystem paths the sandbox may read
      </p>

      {paths.length === 0 && (
        <p className="text-xs text-[var(--color-muted)] italic">No allowed paths configured.</p>
      )}

      <div className="space-y-1">
        {paths.map((p, i) => (
          <div key={i} className="flex flex-col gap-0.5">
            <div className="flex items-center gap-2 rounded border border-[var(--color-border)] bg-[var(--color-surface-2)] px-2 py-1.5">
              <span className="flex-1 text-xs font-mono text-[var(--color-secondary)] break-all">
                {p}
              </span>
              <ReadOnlyBadge />
              {restartedRows.has(i) && (
                <span className="inline-block rounded px-1.5 py-0.5 text-[10px] border border-yellow-500/40 bg-yellow-500/10 text-yellow-400">
                  restart required
                </span>
              )}
              {isEditing && (
                <button
                  type="button"
                  aria-label={`Delete path ${p}`}
                  className="text-[var(--color-muted)] hover:text-[var(--color-error)] transition-colors focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] rounded"
                  onClick={() => onDelete(i)}
                >
                  <Trash size={12} />
                </button>
              )}
            </div>
            {rowErrors[i] && (
              <p className="text-[10px] text-[var(--color-error)] pl-2">{rowErrors[i]}</p>
            )}
          </div>
        ))}
      </div>

      {isEditing && (
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <Input
              value={newPath}
              onChange={(e) => onNewPathChange(e.target.value)}
              placeholder="/var/data/shared"
              className="h-7 text-xs font-mono flex-1"
              aria-label="New allowed path"
              onKeyDown={(e) => {
                if (e.key === 'Enter') { e.preventDefault(); onAdd() }
              }}
            />
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-7 px-2 gap-1 text-xs shrink-0"
              onClick={onAdd}
              aria-label="Add path"
            >
              <Plus size={11} />
              Add
            </Button>
          </div>
          {addError && (
            <p className="text-[10px] text-[var(--color-error)]">{addError}</p>
          )}
        </div>
      )}
    </div>
  )
}

// ── SSRF Editor (k24) ─────────────────────────────────────────────────────────

interface SsrfEditorProps {
  list: string[]
  isEditing: boolean
  activePreset: number | null
  advancedOpen: boolean
  onAdvancedToggle: () => void
  onPresetClick: (idx: number) => void
  advancedErrors: Record<number, string>
  onDeleteAdvanced: (idx: number) => void
  newSsrfEntry: string
  onNewSsrfEntryChange: (v: string) => void
  onAddSsrfEntry: () => void
  ssrfAddError: string | null
}

function SsrfEditor({
  list,
  isEditing,
  activePreset,
  advancedOpen,
  onAdvancedToggle,
  onPresetClick,
  advancedErrors,
  onDeleteAdvanced,
  newSsrfEntry,
  onNewSsrfEntryChange,
  onAddSsrfEntry,
  ssrfAddError,
}: SsrfEditorProps) {
  return (
    <div className="space-y-2 border-t border-[var(--color-border)] pt-3">
      <p className="text-xs font-semibold text-[var(--color-secondary)]">
        SSRF internal-network policy
      </p>

      <div className="flex flex-wrap gap-2">
        {SSRF_PRESETS.map((preset, idx) => (
          <button
            key={preset.label}
            type="button"
            disabled={!isEditing}
            onClick={() => onPresetClick(idx)}
            className={[
              'rounded border px-3 py-1 text-xs transition-colors focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)]',
              activePreset === idx
                ? 'border-[var(--color-accent)] bg-[var(--color-accent)]/10 text-[var(--color-accent)]'
                : 'border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-muted)] hover:border-[var(--color-accent)]/50',
              !isEditing ? 'opacity-60 cursor-default' : 'cursor-pointer',
            ].join(' ')}
            aria-pressed={activePreset === idx}
          >
            {preset.label}
          </button>
        ))}
      </div>

      <button
        type="button"
        onClick={onAdvancedToggle}
        className="flex items-center gap-1 text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] transition-colors focus:outline-none"
        aria-expanded={advancedOpen}
      >
        {advancedOpen ? <CaretUp size={10} /> : <CaretDown size={10} />}
        Advanced (custom list)
      </button>

      {advancedOpen && (
        <div className="space-y-1 pl-3 border-l border-[var(--color-border)]">
          {list.length === 0 && (
            <p className="text-xs text-[var(--color-muted)] italic">Empty — all internal traffic blocked.</p>
          )}
          {list.map((entry, i) => (
            <div key={i} className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2 rounded border border-[var(--color-border)] bg-[var(--color-surface-2)] px-2 py-1">
                <span className="flex-1 text-xs font-mono text-[var(--color-secondary)] break-all">
                  {entry}
                </span>
                {isEditing && (
                  <button
                    type="button"
                    aria-label={`Delete SSRF entry ${entry}`}
                    className="text-[var(--color-muted)] hover:text-[var(--color-error)] transition-colors focus:outline-none focus:ring-1 focus:ring-[var(--color-accent)] rounded"
                    onClick={() => onDeleteAdvanced(i)}
                  >
                    <Trash size={12} />
                  </button>
                )}
              </div>
              {advancedErrors[i] && (
                <p className="text-[10px] text-[var(--color-error)] pl-2">{advancedErrors[i]}</p>
              )}
            </div>
          ))}

          {isEditing && (
            <div className="space-y-1 pt-1">
              <div className="flex items-center gap-2">
                <Input
                  value={newSsrfEntry}
                  onChange={(e) => onNewSsrfEntryChange(e.target.value)}
                  placeholder="10.0.0.0/8 or internal.corp"
                  className="h-7 text-xs font-mono flex-1"
                  aria-label="New SSRF allow entry"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') { e.preventDefault(); onAddSsrfEntry() }
                  }}
                />
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="h-7 px-2 gap-1 text-xs shrink-0"
                  onClick={onAddSsrfEntry}
                  aria-label="Add SSRF entry"
                >
                  <Plus size={11} />
                  Add
                </Button>
              </div>
              {ssrfAddError && (
                <p className="text-[10px] text-[var(--color-error)]">{ssrfAddError}</p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

export function SandboxSection(): React.ReactElement {
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'
  const queryClient = useQueryClient()

  // ── Status query ───────────────────────────────────────────────────────────
  const [statusExpanded, setStatusExpanded] = useState(false)
  const {
    data: statusData,
    isLoading: statusLoading,
    isError: statusIsError,
    error: statusError,
    refetch: statusRefetch,
    isFetching: statusFetching,
  } = useQuery({
    queryKey: ['sandbox-status'],
    queryFn: fetchSandboxStatus,
  })

  // ── Config query (k23/k24) ─────────────────────────────────────────────────
  const { data: configData, isLoading: configLoading } = useQuery({
    queryKey: ['sandbox-config'],
    queryFn: fetchSandboxConfig,
  })

  // ── Edit mode ──────────────────────────────────────────────────────────────
  const [isEditing, setIsEditing] = useState(false)

  // ── ABI v4 banner state (k25) ──────────────────────────────────────────────
  const [bannerDismissed, setBannerDismissed] = useState(() => {
    if (typeof sessionStorage === 'undefined') return false
    return sessionStorage.getItem(ABI4_BANNER_SESSION_KEY) === 'dismissed'
  })

  const handleBannerDismiss = useCallback(() => {
    sessionStorage.setItem(ABI4_BANNER_SESSION_KEY, 'dismissed')
    localStorage.setItem(ABI4_BANNER_SESSION_KEY, new Date().toISOString())
    setBannerDismissed(true)
  }, [])

  const showAbi4Banner =
    !bannerDismissed &&
    typeof statusData?.abi_version === 'number' &&
    statusData.abi_version >= 4 &&
    typeof statusData.issue_ref === 'string'

  // ── Allowed paths state (k23) ──────────────────────────────────────────────
  const [pathList, setPathList] = useState<string[]>([])
  const [newPath, setNewPath] = useState('')
  const [pathAddError, setPathAddError] = useState<string | null>(null)
  const [pathRowErrors, setPathRowErrors] = useState<Record<number, string>>({})
  const [pathRestartedRows, setPathRestartedRows] = useState<Set<number>>(new Set())

  // ── SSRF state (k24) ──────────────────────────────────────────────────────
  const [ssrfList, setSsrfList] = useState<string[]>([])
  const [ssrfActivePreset, setSsrfActivePreset] = useState<number | null>(null)
  const [ssrfAdvancedOpen, setSsrfAdvancedOpen] = useState(false)
  const [ssrfAdvancedErrors, setSsrfAdvancedErrors] = useState<Record<number, string>>({})
  const [newSsrfEntry, setNewSsrfEntry] = useState('')
  const [ssrfAddError, setSsrfAddError] = useState<string | null>(null)

  // ── Modal state (k24 wildcard + k25 enforce) ───────────────────────────────
  const [showWildcardModal, setShowWildcardModal] = useState(false)
  const [showEnforceModal, setShowEnforceModal] = useState(false)
  const pendingSaveRef = useRef<(() => void) | null>(null)

  // ── Sync from config query ─────────────────────────────────────────────────
  useEffect(() => {
    if (!configData) return
    const paths = configData.allowed_paths ?? []
    setPathList(paths)
    setPathRestartedRows(new Set())

    const allowInternal = configData.ssrf?.allow_internal ?? []
    setSsrfList(allowInternal)

    const matchedPreset = SSRF_PRESETS.findIndex((p) => listsMatch(allowInternal, p.list))
    if (matchedPreset >= 0) {
      setSsrfActivePreset(matchedPreset)
      setSsrfAdvancedOpen(false)
    } else {
      setSsrfActivePreset(null)
      setSsrfAdvancedOpen(true)
    }
  }, [configData])

  // ── Config update mutation ─────────────────────────────────────────────────
  const saveMutation = useMutation({
    mutationFn: (body: Parameters<typeof updateSandboxConfig>[0]) => updateSandboxConfig(body),
    onSuccess: (resp) => {
      void queryClient.invalidateQueries({ queryKey: ['sandbox-config'] })
      setIsEditing(false)
      setPathAddError(null)
      setSsrfAddError(null)
      setPathRowErrors({})
      setSsrfAdvancedErrors({})
      if (resp.requires_restart) {
        setPathRestartedRows(new Set(pathList.map((_, i) => i)))
      }
    },
    onError: (err: Error) => {
      const msg = err.message.replace(/^\d+:\s*/, '')
      const pathRowMatch = /allowed_paths\[(\d+)\]:\s*(.+)/.exec(msg)
      if (pathRowMatch) {
        const rowIdx = parseInt(pathRowMatch[1], 10)
        setPathRowErrors({ [rowIdx]: pathRowMatch[2] })
        return
      }
      const ssrfRowMatch = /ssrf\.allow_internal\[(\d+)\]:\s*(.+)/.exec(msg)
      if (ssrfRowMatch) {
        const rowIdx = parseInt(ssrfRowMatch[1], 10)
        setSsrfAdvancedErrors({ [rowIdx]: ssrfRowMatch[2] })
        return
      }
      setPathAddError(msg)
    },
  })

  // ── Allowed paths handlers ─────────────────────────────────────────────────
  function handleDeletePath(index: number) {
    setPathList((prev) => prev.filter((_, i) => i !== index))
    setPathRowErrors({})
  }

  function handleAddPath() {
    const trimmed = newPath.trim()
    if (!trimmed) {
      setPathAddError('Path cannot be empty.')
      return
    }
    setPathAddError(null)
    setPathList((prev) => [...prev, trimmed])
    setNewPath('')
  }

  // ── SSRF handlers ─────────────────────────────────────────────────────────
  function handlePresetClick(idx: number) {
    setSsrfActivePreset(idx)
    setSsrfList([...SSRF_PRESETS[idx].list])
    setSsrfAdvancedErrors({})
  }

  function handleDeleteSsrfEntry(idx: number) {
    setSsrfList((prev) => {
      const next = prev.filter((_, i) => i !== idx)
      const matchedPreset = SSRF_PRESETS.findIndex((p) => listsMatch(next, p.list))
      setSsrfActivePreset(matchedPreset >= 0 ? matchedPreset : null)
      return next
    })
    setSsrfAdvancedErrors({})
  }

  function handleAddSsrfEntry() {
    const trimmed = newSsrfEntry.trim()
    if (!trimmed) {
      setSsrfAddError('Entry cannot be empty.')
      return
    }
    if (!isValidSsrfEntry(trimmed)) {
      setSsrfAddError('invalid entry — expected hostname, IP, or CIDR')
      return
    }
    setSsrfAddError(null)
    setSsrfList((prev) => {
      const next = [...prev, trimmed]
      const matchedPreset = SSRF_PRESETS.findIndex((p) => listsMatch(next, p.list))
      setSsrfActivePreset(matchedPreset >= 0 ? matchedPreset : null)
      return next
    })
    setNewSsrfEntry('')
  }

  // ── Save orchestration ────────────────────────────────────────────────────

  function validateSsrfEntries(): boolean {
    const errors: Record<number, string> = {}
    ssrfList.forEach((entry, i) => {
      if (!isValidSsrfEntry(entry)) {
        errors[i] = 'invalid entry — expected hostname, IP, or CIDR'
      }
    })
    setSsrfAdvancedErrors(errors)
    return Object.keys(errors).length === 0
  }

  function executeSave() {
    saveMutation.mutate({
      allowed_paths: pathList,
      ssrf: { allow_internal: ssrfList },
    })
  }

  function handleSave() {
    setPathRowErrors({})
    setSsrfAdvancedErrors({})

    if (!validateSsrfEntries()) return

    const hasWildcard = ssrfList.some(isWildcardEntry)
    if (hasWildcard) {
      pendingSaveRef.current = executeSave
      setShowWildcardModal(true)
      return
    }

    executeSave()
  }

  function handleWildcardConfirm() {
    setShowWildcardModal(false)
    if (pendingSaveRef.current) {
      pendingSaveRef.current()
      pendingSaveRef.current = null
    }
  }

  function handleWildcardCancel() {
    setShowWildcardModal(false)
    pendingSaveRef.current = null
  }

  function handleEnforceModalConfirm() {
    setShowEnforceModal(false)
    if (pendingSaveRef.current) {
      pendingSaveRef.current()
      pendingSaveRef.current = null
    }
  }

  function handleEnforceModalCancel() {
    setShowEnforceModal(false)
    pendingSaveRef.current = null
  }

  function handleCancel() {
    if (configData) {
      const paths = configData.allowed_paths ?? []
      setPathList(paths)
      const allowInternal = configData.ssrf?.allow_internal ?? []
      setSsrfList(allowInternal)
      const matchedPreset = SSRF_PRESETS.findIndex((p) => listsMatch(allowInternal, p.list))
      setSsrfActivePreset(matchedPreset >= 0 ? matchedPreset : null)
      setSsrfAdvancedOpen(matchedPreset < 0)
    }
    setIsEditing(false)
    setPathAddError(null)
    setSsrfAddError(null)
    setPathRowErrors({})
    setSsrfAdvancedErrors({})
    setNewPath('')
    setNewSsrfEntry('')
  }

  // ── Status display helpers ────────────────────────────────────────────────

  function resolveDotVariant(): DotVariant {
    if (!statusData) return 'red'
    if (statusData.kernel_level) return 'green'
    if (statusData.available) return 'amber'
    return 'red'
  }

  function resolveBackendLabel(): string {
    if (!statusData) return 'Unknown'
    return statusData.kernel_level ? statusData.backend : 'Application fallback'
  }

  function resolveHeaderIcon(): typeof Shield {
    if (statusData?.kernel_level) return ShieldCheck
    if (statusData?.available) return ShieldWarning
    return Shield
  }

  function resolveDescription(): string | null {
    if (!statusData) return null
    if (statusData.kernel_level) {
      return 'Child processes are restricted at the kernel level using Linux Landlock and seccomp-BPF. This provides strong isolation.'
    }
    return 'Kernel-level sandboxing is unavailable on this platform. Falling back to cooperative environment-variable enforcement — uncooperative binaries are NOT contained.'
  }

  const dotVariant = resolveDotVariant()
  const backendLabel = resolveBackendLabel()
  const HeaderIcon = resolveHeaderIcon()
  const description = resolveDescription()
  const backendColor = statusData?.kernel_level ? 'var(--color-accent)' : 'var(--color-muted)'

  const hasCapabilities = !!(
    statusData &&
    (statusData.abi_version != null ||
      (statusData.landlock_features && statusData.landlock_features.length > 0) ||
      (statusData.blocked_syscalls && statusData.blocked_syscalls.length > 0) ||
      statusData.seccomp_enabled)
  )

  const hasSsrfErrors = Object.keys(ssrfAdvancedErrors).length > 0
  const saveDisabled = saveMutation.isPending || hasSsrfErrors

  function renderStatusBody(): React.ReactNode {
    if (statusLoading) return <SandboxSkeleton />

    if (statusIsError) {
      const errorDetail = statusError instanceof Error ? statusError.message : undefined
      return (
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 flex items-start gap-2">
          <XCircle size={14} style={{ color: 'var(--color-error)' }} className="mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-sm text-[var(--color-error)]">Failed to load sandbox status</p>
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
            onClick={() => { void statusRefetch() }}
            disabled={statusFetching}
          >
            Retry
          </Button>
        </div>
      )
    }

    if (!statusData) return null

    return (
      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4">
        <div className="flex items-center gap-2 mb-3">
          <StatusDot variant={dotVariant} />
          <HeaderIcon
            size={14}
            style={{ color: statusData.kernel_level ? 'var(--color-accent)' : 'var(--color-muted)' }}
            weight="duotone"
          />
          <span className="text-sm font-semibold font-mono" style={{ color: backendColor }}>
            {backendLabel}
          </span>
        </div>

        {description && (
          <p className="text-xs text-[var(--color-muted)] leading-relaxed">{description}</p>
        )}

        {statusData.notes && statusData.notes.length > 0 && (
          <div className="mt-2 rounded-md border border-yellow-500/30 bg-yellow-500/5 p-2 space-y-1">
            {statusData.notes.map((note, i) => (
              <p key={i} className="text-[10px] text-yellow-400 leading-relaxed">
                <span className="font-semibold">Note:</span> {note}
              </p>
            ))}
          </div>
        )}

        {hasCapabilities && (
          <>
            <button
              type="button"
              onClick={() => setStatusExpanded((e) => !e)}
              className="mt-3 flex items-center gap-1 text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] transition-colors"
              aria-expanded={statusExpanded}
            >
              {statusExpanded ? <CaretUp size={10} /> : <CaretDown size={10} />}
              {statusExpanded ? 'Hide capabilities' : 'Show capabilities'}
            </button>
            {statusExpanded && <CapabilitiesPanel data={statusData} />}
          </>
        )}
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
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-2 gap-1 text-xs"
          aria-label="Refresh sandbox status"
          onClick={() => { void statusRefetch() }}
          disabled={statusFetching}
        >
          <ArrowsClockwise size={11} className={statusFetching ? 'animate-spin' : undefined} />
        </Button>
      </div>

      {/* ABI v4 banner (k25) */}
      {showAbi4Banner && (
        <Abi4Banner
          abiVersion={statusData!.abi_version!}
          issueRef={statusData!.issue_ref!}
          onDismiss={handleBannerDismiss}
        />
      )}

      {/* Status display */}
      {renderStatusBody()}

      {/* Config editor — only shown when status loaded successfully */}
      {!statusLoading && !statusIsError && (
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <p className="text-xs font-semibold text-[var(--color-secondary)]">Sandbox configuration</p>
            {isAdmin && !isEditing && (
              <Button
                size="sm"
                variant="outline"
                className="h-7 px-2 text-xs"
                onClick={() => setIsEditing(true)}
                disabled={configLoading}
              >
                Edit
              </Button>
            )}
          </div>

          {configLoading ? (
            <div className="space-y-2 animate-pulse">
              <div className="h-3 w-3/4 rounded bg-[var(--color-border)]" />
              <div className="h-3 w-1/2 rounded bg-[var(--color-border)]" />
            </div>
          ) : (
            <>
              <AllowedPathsEditor
                paths={pathList}
                isEditing={isEditing}
                rowErrors={pathRowErrors}
                restartedRows={pathRestartedRows}
                onDelete={handleDeletePath}
                newPath={newPath}
                onNewPathChange={(v) => { setNewPath(v); setPathAddError(null) }}
                onAdd={handleAddPath}
                addError={pathAddError}
              />

              <SsrfEditor
                list={ssrfList}
                isEditing={isEditing}
                activePreset={ssrfActivePreset}
                advancedOpen={ssrfAdvancedOpen}
                onAdvancedToggle={() => setSsrfAdvancedOpen((v) => !v)}
                onPresetClick={handlePresetClick}
                advancedErrors={ssrfAdvancedErrors}
                onDeleteAdvanced={handleDeleteSsrfEntry}
                newSsrfEntry={newSsrfEntry}
                onNewSsrfEntryChange={(v) => { setNewSsrfEntry(v); setSsrfAddError(null) }}
                onAddSsrfEntry={handleAddSsrfEntry}
                ssrfAddError={ssrfAddError}
              />

              {isAdmin && isEditing && (
                <div className="flex items-center gap-2 pt-2 border-t border-[var(--color-border)]">
                  <Button
                    type="button"
                    size="sm"
                    className="h-7 px-3 text-xs"
                    onClick={handleSave}
                    disabled={saveDisabled}
                  >
                    {saveMutation.isPending ? 'Saving\u2026' : 'Save'}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="h-7 px-3 text-xs"
                    onClick={handleCancel}
                    disabled={saveMutation.isPending}
                  >
                    Cancel
                  </Button>
                  {saveMutation.isError && (
                    <p className="text-xs text-[var(--color-error)] flex-1">
                      {saveMutation.error instanceof Error
                        ? saveMutation.error.message.replace(/^\d+:\s*/, '')
                        : 'Save failed'}
                    </p>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* Wildcard SSRF confirmation modal (k24) */}
      <Dialog
        open={showWildcardModal}
        onOpenChange={(open) => { if (!open) handleWildcardCancel() }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Disable SSRF protection?</DialogTitle>
            <DialogDescription>
              This would disable SSRF protection entirely — continue?
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={handleWildcardCancel}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleWildcardConfirm}
              style={{ background: 'var(--color-error)', color: '#fff' }}
            >
              Save anyway
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ABI v4 enforce-mode confirmation modal (k25) */}
      <Dialog
        open={showEnforceModal}
        onOpenChange={(open) => { if (!open) handleEnforceModalCancel() }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Kernel incompatibility warning</DialogTitle>
            <DialogDescription>
              {statusData && typeof statusData.abi_version === 'number' && statusData.abi_version >= 4
                ? `Your kernel reports Landlock ABI v${statusData.abi_version} (issue ${statusData.issue_ref ?? ''}). Enforce mode will cause the gateway to exit with code 78 at next boot. Save anyway?`
                : 'Enforce mode may cause issues with your current kernel configuration. Save anyway?'}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={handleEnforceModalCancel}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleEnforceModalConfirm}
              style={{ background: 'var(--color-error)', color: '#fff' }}
            >
              Save anyway
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
