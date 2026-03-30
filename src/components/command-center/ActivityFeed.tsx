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
  const [open, setOpen] = useState(true)

  const { data: events = [], isLoading } = useQuery({
    queryKey: ['activity'],
    queryFn: fetchActivity,
    refetchInterval: 30_000,
  })

  // Reverse-chronological
  const sorted = [...events].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  )

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
        </div>
        {open ? (
          <CaretUp size={12} className="text-[var(--color-muted)]" />
        ) : (
          <CaretDown size={12} className="text-[var(--color-muted)]" />
        )}
      </button>

      {open && (
        <div className="pb-2">
          {isLoading ? (
            <div className="space-y-1 px-4 py-1">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-8 rounded bg-[var(--color-surface-1)] animate-pulse" />
              ))}
            </div>
          ) : sorted.length === 0 ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-muted)]">No recent activity.</p>
          ) : (
            <div>
              {sorted.map((event) => (
                <EventRow key={event.id} event={event} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
