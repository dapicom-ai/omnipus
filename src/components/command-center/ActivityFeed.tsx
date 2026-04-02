import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Pulse,
  CaretDown,
  CaretUp,
  Plus,
  ArrowsClockwise,
  Robot,
  Terminal,
  Warning,
  CheckCircle,
  Clock,
} from '@phosphor-icons/react'
import { fetchActivity } from '@/lib/api'
import type { ActivityEvent } from '@/lib/api'
import { cn } from '@/lib/utils'

// Event types shown in the activity feed — chat messages are intentionally excluded
const ALLOWED_TYPES = new Set([
  'task_created',
  'task_updated',
  'session_started',
  'session_ended',
  'agent_error',
  'tool_called',
  'approval_requested',
])

type FilterPill = 'all' | 'errors' | 'tasks' | 'sessions'

const FILTER_PILLS: { value: FilterPill; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'errors', label: 'Errors' },
  { value: 'tasks', label: 'Tasks' },
  { value: 'sessions', label: 'Sessions' },
]

const PILL_TYPES: Record<FilterPill, Set<string>> = {
  all: ALLOWED_TYPES,
  errors: new Set(['agent_error']),
  tasks: new Set(['task_created', 'task_updated', 'approval_requested']),
  sessions: new Set(['session_started', 'session_ended']),
}

const PAGE_SIZE = 20

function eventIcon(type: string) {
  switch (type) {
    case 'task_created': return <Plus size={12} />
    case 'task_updated': return <ArrowsClockwise size={12} />
    case 'session_started': return <Robot size={12} />
    case 'session_ended': return <CheckCircle size={12} />
    case 'agent_error': return <Warning size={12} />
    case 'tool_called': return <Terminal size={12} />
    case 'approval_requested': return <Warning size={12} />
    default: return <Pulse size={12} />
  }
}

function eventColor(type: string): string {
  switch (type) {
    case 'agent_error': return 'text-[var(--color-error)]'
    case 'approval_requested': return 'text-[var(--color-warning)]'
    case 'session_started': return 'text-[var(--color-success)]'
    case 'task_created': return 'text-[var(--color-accent)]'
    default: return 'text-[var(--color-muted)]'
  }
}

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short' }).format(new Date(iso))
}

function EventRow({ event }: { event: ActivityEvent }) {
  const color = eventColor(event.type)
  return (
    <div className="flex items-start gap-2.5 px-4 py-2 hover:bg-[var(--color-surface-2)] transition-colors">
      <div className={cn('mt-0.5 shrink-0', color)}>{eventIcon(event.type)}</div>
      <div className="flex-1 min-w-0">
        <p className="text-xs text-[var(--color-secondary)] leading-snug">{event.summary}</p>
        {event.agent_name && (
          <p className="text-[10px] text-[var(--color-muted)] mt-0.5">{event.agent_name}</p>
        )}
      </div>
      <div className="flex items-center gap-1 shrink-0 text-[10px] text-[var(--color-muted)]">
        <Clock size={10} />
        <span>{relativeTime(event.timestamp)}</span>
      </div>
    </div>
  )
}

export function ActivityFeed() {
  const [open, setOpen] = useState(false)
  const [activeFilter, setActiveFilter] = useState<FilterPill>('all')
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE)

  const { data: events = [], isLoading, isError: activityError } = useQuery({
    queryKey: ['activity'],
    queryFn: fetchActivity,
    refetchInterval: 30_000,
  })

  // Filter to meaningful events only (no chat message echoes)
  const meaningful = events.filter((e) => ALLOWED_TYPES.has(e.type))

  // Apply pill filter
  const allowedForPill = PILL_TYPES[activeFilter]
  const filtered = meaningful.filter((e) => allowedForPill.has(e.type))

  // Reverse-chronological
  const sorted = [...filtered].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  )

  const visible = sorted.slice(0, visibleCount)
  const hasMore = sorted.length > visibleCount

  function handleFilterChange(pill: FilterPill) {
    setActiveFilter(pill)
    setVisibleCount(PAGE_SIZE)
  }

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center justify-between w-full px-4 py-2.5 hover:bg-[var(--color-surface-2)] transition-colors"
      >
        <div className="flex items-center gap-2">
          <Pulse size={13} className="text-[var(--color-muted)]" />
          <span className="text-xs font-semibold text-[var(--color-secondary)]">Activity</span>
          {meaningful.length > 0 && (
            <span className="text-[10px] text-[var(--color-muted)]">({meaningful.length})</span>
          )}
        </div>
        {open ? (
          <CaretUp size={12} className="text-[var(--color-muted)]" />
        ) : (
          <CaretDown size={12} className="text-[var(--color-muted)]" />
        )}
      </button>

      {open && (
        <div className="pb-2">
          {/* Filter pills */}
          <div className="flex items-center gap-1.5 px-4 pb-2">
            {FILTER_PILLS.map((pill) => (
              <button
                key={pill.value}
                type="button"
                onClick={() => handleFilterChange(pill.value)}
                className={cn(
                  'px-2.5 py-0.5 rounded-full text-[10px] font-medium transition-colors whitespace-nowrap',
                  activeFilter === pill.value
                    ? 'bg-[var(--color-accent)] text-[var(--color-primary)]'
                    : 'bg-[var(--color-surface-1)] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)]',
                )}
              >
                {pill.label}
              </button>
            ))}
          </div>

          {activityError ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-error)]">Could not load activity feed.</p>
          ) : isLoading ? (
            <div className="space-y-1 px-4 py-1">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-8 rounded bg-[var(--color-surface-1)] animate-pulse" />
              ))}
            </div>
          ) : visible.length === 0 ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-muted)]">No recent activity.</p>
          ) : (
            <div>
              {visible.map((event) => (
                <EventRow key={event.id} event={event} />
              ))}
              {hasMore && (
                <button
                  type="button"
                  onClick={() => setVisibleCount((n) => n + PAGE_SIZE)}
                  className="w-full px-4 py-2 text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors text-center"
                >
                  Show more ({sorted.length - visibleCount} remaining)
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
