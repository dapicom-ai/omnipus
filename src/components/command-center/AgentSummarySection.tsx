import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Robot, CaretDown, CaretUp, ArrowRight } from '@phosphor-icons/react'
import { Badge } from '@/components/ui/badge'
import { fetchAgents, fetchSessions, fetchTasks } from '@/lib/api'
import { useChatStore } from '@/store/chat'
import { useNavigate } from '@tanstack/react-router'
import { cn } from '@/lib/utils'

const STATUS_STYLE: Record<string, string> = {
  active: 'text-[var(--color-success)] bg-[var(--color-success)]/10',
  idle: 'text-[var(--color-muted)] bg-[var(--color-surface-2)]',
  error: 'text-[var(--color-error)] bg-[var(--color-error)]/10',
}

export function AgentSummarySection() {
  const [open, setOpen] = useState(true)
  const navigate = useNavigate()
  const setActiveSession = useChatStore((s) => s.setActiveSession)

  const { data: agents = [], isLoading, isError: agentsError } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    refetchInterval: 30_000,
  })

  const { data: sessions = [], isError: sessionsError } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => fetchSessions(),
    refetchInterval: 30_000,
  })

  const { data: tasks = [], isError: tasksError } = useQuery({
    queryKey: ['tasks'],
    queryFn: fetchTasks,
    refetchInterval: 30_000,
  })

  const taskCountByAgent = tasks.reduce<Record<string, number>>((acc, t) => {
    if (t.agent_id) acc[t.agent_id] = (acc[t.agent_id] ?? 0) + 1
    return acc
  }, {})

  const lastActiveByAgent = sessions.reduce<Record<string, string>>((acc, s) => {
    const prev = acc[s.agent_id]
    if (!prev || s.updated_at > prev) acc[s.agent_id] = s.updated_at
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
          {agentsError ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-error)]">Could not load agents.</p>
          ) : isLoading ? (
            <div className="space-y-1 px-4 py-1">
              {[1, 2].map((i) => (
                <div key={i} className="h-9 rounded bg-[var(--color-surface-1)] animate-pulse" />
              ))}
            </div>
          ) : agents.length === 0 ? (
            <p className="px-4 pb-2 text-xs text-[var(--color-muted)]">No agents configured.</p>
          ) : (
            <div className="space-y-px px-2">
              {(sessionsError || tasksError) && (
                <p className="px-2 pb-1 text-[10px] text-[var(--color-muted)]">
                  {sessionsError && tasksError
                    ? 'Could not load session and task data.'
                    : sessionsError
                      ? 'Could not load session data.'
                      : 'Could not load task data.'}
                </p>
              )}
              {agents.map((agent) => {
                const lastActive = lastActiveByAgent[agent.id]
                const taskCount = taskCountByAgent[agent.id] ?? 0
                return (
                  <div
                    key={agent.id}
                    className="flex items-center gap-3 px-2 py-2 rounded-md hover:bg-[var(--color-surface-2)] transition-colors group"
                  >
                    {/* Avatar */}
                    <div className="w-7 h-7 rounded-full bg-[var(--color-surface-2)] border border-[var(--color-border)] flex items-center justify-center shrink-0">
                      <Robot size={13} className="text-[var(--color-muted)]" />
                    </div>

                    {/* Name + model + last active */}
                    <div className="flex-1 min-w-0">
                      <p className="text-xs font-medium text-[var(--color-secondary)] truncate">{agent.name}</p>
                      <p className="text-[10px] text-[var(--color-muted)] truncate font-mono">
                        {lastActive
                          ? new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(lastActive))
                          : agent.model}
                      </p>
                    </div>

                    {/* Task count */}
                    {taskCount > 0 && (
                      <span className="text-[10px] text-[var(--color-muted)] shrink-0">
                        {taskCount} task{taskCount !== 1 ? 's' : ''}
                      </span>
                    )}

                    {/* Status */}
                    <Badge
                      variant="outline"
                      className={cn('text-[10px] shrink-0 capitalize', STATUS_STYLE[agent.status] ?? STATUS_STYLE.idle)}
                    >
                      {agent.status}
                    </Badge>

                    {/* Chat button */}
                    <button
                      type="button"
                      onClick={() => {
                        setActiveSession(null, agent.id)
                        void navigate({ to: '/' })
                      }}
                      className="shrink-0 flex items-center gap-0.5 text-[10px] text-[var(--color-accent)] hover:text-[var(--color-accent-hover)] transition-colors opacity-0 group-hover:opacity-100 font-medium"
                      aria-label={`Chat with ${agent.name}`}
                    >
                      Chat <ArrowRight size={10} />
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
