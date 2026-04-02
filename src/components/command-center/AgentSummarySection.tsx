import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Robot, CaretDown, CaretUp, ArrowRight } from '@phosphor-icons/react'
import { fetchAgents, fetchTasks } from '@/lib/api'
import { useChatStore } from '@/store/chat'
import { useNavigate } from '@tanstack/react-router'
import { cn } from '@/lib/utils'

const STATUS_DOT: Record<string, string> = {
  active: 'bg-[var(--color-success)]',
  idle: 'bg-[var(--color-muted)]',
  error: 'bg-[var(--color-error)]',
  draft: 'bg-transparent border border-dashed border-[var(--color-warning)]',
}

const STATUS_DOT_COLOR: Record<string, string> = {
  active: 'bg-green-500',
  idle: 'bg-[var(--color-muted)]',
  error: 'bg-red-500',
  draft: '',
}

export function AgentSummarySection() {
  const [open, setOpen] = useState(true)
  const [showDrafts, setShowDrafts] = useState(false)
  const navigate = useNavigate()
  const setActiveSession = useChatStore((s) => s.setActiveSession)

  const { data: agents = [], isLoading, isError: agentsError } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
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

  const draftAgents = agents.filter((a) => a.status === 'draft')
  const nonDraftAgents = agents.filter((a) => a.status !== 'draft')
  const visibleAgents = showDrafts ? agents : nonDraftAgents

  return (
    <div className="border-b border-[var(--color-border)]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center justify-between w-full px-4 py-2 hover:bg-[var(--color-surface-2)] transition-colors"
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
        <div className="px-3 pb-2.5">
          {agentsError ? (
            <p className="py-1 text-xs text-[var(--color-error)]">Could not load agents.</p>
          ) : isLoading ? (
            <div className="flex gap-2 py-1">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-7 w-28 rounded-full bg-[var(--color-surface-1)] animate-pulse" />
              ))}
            </div>
          ) : agents.length === 0 ? (
            <p className="py-1 text-xs text-[var(--color-muted)]">No agents configured.</p>
          ) : (
            <>
              {tasksError && (
                <p className="pb-1 text-[10px] text-[var(--color-muted)]">Could not load task data.</p>
              )}
              <div className="flex flex-wrap gap-2 py-1">
                {visibleAgents.map((agent) => {
                  const taskCount = taskCountByAgent[agent.id] ?? 0
                  const dotClass = agent.status === 'draft'
                    ? STATUS_DOT.draft
                    : (STATUS_DOT_COLOR[agent.status] ?? STATUS_DOT_COLOR.idle)

                  return (
                    <div
                      key={agent.id}
                      className="group relative flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-[var(--color-surface-1)] border border-[var(--color-border)] hover:border-[var(--color-accent)]/40 hover:bg-[var(--color-surface-2)] transition-colors cursor-default"
                    >
                      {/* Status dot */}
                      <span
                        className={cn('w-1.5 h-1.5 rounded-full shrink-0', dotClass)}
                        aria-label={agent.status}
                      />

                      {/* Name */}
                      <span className="text-xs font-medium text-[var(--color-secondary)] whitespace-nowrap max-w-[120px] truncate">
                        {agent.name}
                      </span>

                      {/* Task count badge */}
                      {taskCount > 0 && (
                        <span className="text-[10px] text-[var(--color-muted)] leading-none">
                          {taskCount}
                        </span>
                      )}

                      {/* Chat hover action */}
                      <button
                        type="button"
                        onClick={() => {
                          setActiveSession(null, agent.id, agent.type)
                          void navigate({ to: '/' })
                        }}
                        className="ml-0.5 flex items-center gap-0.5 text-[10px] text-[var(--color-accent)] hover:text-[var(--color-accent-hover)] font-medium transition-colors opacity-0 group-hover:opacity-100 whitespace-nowrap"
                        aria-label={`Chat with ${agent.name}`}
                      >
                        Chat <ArrowRight size={9} />
                      </button>
                    </div>
                  )
                })}

                {/* Show drafts toggle */}
                {draftAgents.length > 0 && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation()
                      setShowDrafts((v) => !v)
                    }}
                    className="flex items-center px-2.5 py-1 rounded-full border border-dashed border-[var(--color-border)] text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:border-[var(--color-muted)] transition-colors whitespace-nowrap"
                  >
                    {showDrafts
                      ? `Hide drafts`
                      : `Show drafts (${draftAgents.length})`}
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
