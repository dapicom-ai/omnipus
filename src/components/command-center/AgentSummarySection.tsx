import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Robot, CaretDown, CaretUp } from '@phosphor-icons/react'
import { Badge } from '@/components/ui/badge'
import { fetchAgents, fetchSessions } from '@/lib/api'
import { cn } from '@/lib/utils'

const STATUS_STYLE: Record<string, string> = {
  active: 'text-[var(--color-success)] bg-[var(--color-success)]/10',
  idle: 'text-[var(--color-muted)] bg-[var(--color-surface-2)]',
  error: 'text-[var(--color-error)] bg-[var(--color-error)]/10',
}

export function AgentSummarySection() {
  const [open, setOpen] = useState(true)

  const { data: agents = [], isLoading } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    refetchInterval: 30_000,
  })

  const { data: sessions = [] } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => fetchSessions(),
    refetchInterval: 30_000,
  })

  const sessionCountByAgent = sessions.reduce<Record<string, number>>((acc, s) => {
    acc[s.agent_id] = (acc[s.agent_id] ?? 0) + 1
    return acc
  }, {})

  return (
    <div className="border-b border-[var(--color-border)]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center justify-between w-full px-4 py-2.5 hover:bg-[var(--color-surface-2)] transition-colors"
      >
        <div className="flex items-center gap-2">
          <Robot size={13} className="text-[var(--color-muted)]" />
          <span className="text-xs font-semibold text-[var(--color-secondary)]">Agents</span>
          <span className="text-[10px] text-[var(--color-muted)]">({agents.length})</span>
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
              {[1, 2].map((i) => (
                <div key={i} className="h-9 rounded bg-[var(--color-surface-1)] animate-pulse" />
              ))}
            </div>
          ) : agents.length === 0 ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-muted)]">No agents configured.</p>
          ) : (
            <div className="space-y-px px-2">
              {agents.map((agent) => (
                <div
                  key={agent.id}
                  className="flex items-center gap-3 px-2 py-2 rounded-md hover:bg-[var(--color-surface-2)] transition-colors"
                >
                  {/* Avatar */}
                  <div className="w-7 h-7 rounded-full bg-[var(--color-surface-2)] border border-[var(--color-border)] flex items-center justify-center shrink-0">
                    <Robot size={13} className="text-[var(--color-muted)]" />
                  </div>

                  {/* Name + model */}
                  <div className="flex-1 min-w-0">
                    <p className="text-xs font-medium text-[var(--color-secondary)] truncate">{agent.name}</p>
                    <p className="text-[10px] text-[var(--color-muted)] truncate font-mono">{agent.model}</p>
                  </div>

                  {/* Sessions */}
                  <span className="text-[10px] text-[var(--color-muted)] shrink-0">
                    {sessionCountByAgent[agent.id] ?? 0} sessions
                  </span>

                  {/* Status */}
                  <Badge
                    variant="outline"
                    className={cn('text-[10px] shrink-0 capitalize', STATUS_STYLE[agent.status] ?? STATUS_STYLE.idle)}
                  >
                    {agent.status}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
