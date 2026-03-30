import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { StatusBar } from '@/components/command-center/StatusBar'
import { AttentionSection } from '@/components/command-center/AttentionSection'
import { TaskList } from '@/components/command-center/TaskList'
import { TaskDetailPanel } from '@/components/command-center/TaskDetailPanel'
import { AgentSummarySection } from '@/components/command-center/AgentSummarySection'
import { ActivityFeed } from '@/components/command-center/ActivityFeed'
import { fetchTasks } from '@/lib/api'
import type { Task } from '@/lib/api'
import { cn } from '@/lib/utils'

type StatusFilter = 'all' | Task['status']

const FILTER_TABS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'inbox', label: 'Inbox' },
  { value: 'next', label: 'Next' },
  { value: 'active', label: 'Active' },
  { value: 'waiting', label: 'Waiting' },
  { value: 'done', label: 'Done' },
]

export function CommandCenterScreen() {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)

  const { data: tasks = [] } = useQuery({
    queryKey: ['tasks'],
    queryFn: fetchTasks,
    refetchInterval: 10_000,
  })

  const countFor = (filter: StatusFilter) =>
    filter === 'all' ? tasks.length : tasks.filter((t) => t.status === filter).length

  return (
    <div className="flex flex-col h-full">
      {/* 1. Status bar */}
      <StatusBar />

      {/* 2. Attention section */}
      <AttentionSection />

      {/* 3. Filter tabs */}
      <div className="flex items-center gap-0.5 px-4 py-2 border-b border-[var(--color-border)] bg-[var(--color-surface-1)] overflow-x-auto shrink-0">
        {FILTER_TABS.map((tab) => {
          const count = countFor(tab.value)
          const active = statusFilter === tab.value
          return (
            <button
              key={tab.value}
              type="button"
              onClick={() => setStatusFilter(tab.value)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium transition-colors whitespace-nowrap',
                active
                  ? 'bg-[var(--color-accent)] text-[var(--color-primary)]'
                  : 'text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)]',
              )}
            >
              {tab.label}
              {count > 0 && (
                <span
                  className={cn(
                    'rounded-full px-1.5 py-0.5 text-[10px] leading-none font-semibold',
                    active
                      ? 'bg-[var(--color-primary)]/30 text-[var(--color-primary)]'
                      : 'bg-[var(--color-surface-2)] text-[var(--color-muted)]',
                  )}
                >
                  {count}
                </span>
              )}
            </button>
          )
        })}
      </div>

      {/* 4. Task list (scrollable middle) */}
      <div className="flex-1 min-h-0 flex flex-col overflow-hidden">
        <TaskList
          statusFilter={statusFilter}
          onTaskSelect={setSelectedTask}
        />

        {/* 5. Agent summary + 6. Activity feed (pinned below tasks) */}
        <div className="shrink-0 border-t border-[var(--color-border)] overflow-y-auto max-h-72">
          <AgentSummarySection />
          <ActivityFeed />
        </div>
      </div>

      {/* Task detail slide-over */}
      <TaskDetailPanel
        task={selectedTask}
        onClose={() => setSelectedTask(null)}
      />
    </div>
  )
}

export const Route = createFileRoute('/_app/command-center')({
  component: CommandCenterScreen,
})
