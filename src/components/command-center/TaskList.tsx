import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Plus,
  Rows,
  SquaresFour,
  Robot,
  Play,
} from '@phosphor-icons/react'
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  useDroppable,
  useDraggable,
} from '@dnd-kit/core'
import type { DragEndEvent, DragStartEvent } from '@dnd-kit/core'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { SmartSelect } from '@/components/ui/smart-select'
import { fetchTasks, fetchAgents, createTask, updateTask, startTask } from '@/lib/api'
import { isApiError } from '@/lib/api-error'
import type { Task } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { cn } from '@/lib/utils'

// ── Status config ──────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; color: string; bg: string }> = {
  queued:    { label: 'Queued',    color: 'text-blue-400',   bg: 'bg-blue-400/10' },
  assigned:  { label: 'Assigned',  color: 'text-purple-400', bg: 'bg-purple-400/10' },
  running:   { label: 'Running',   color: 'text-yellow-400', bg: 'bg-yellow-400/10' },
  completed: { label: 'Completed', color: 'text-green-400',  bg: 'bg-green-400/10' },
  failed:    { label: 'Failed',    color: 'text-red-400',    bg: 'bg-red-400/10' },
}

const BOARD_COLUMNS: Task['status'][] = ['queued', 'running', 'completed', 'failed']

// ── Priority badge ─────────────────────────────────────────────────────────────

const PRIORITY_CONFIG: Record<number, { label: string; color: string }> = {
  1: { label: 'P1', color: 'text-red-400 bg-red-400/10' },
  2: { label: 'P2', color: 'text-orange-400 bg-orange-400/10' },
  3: { label: 'P3', color: 'text-yellow-400 bg-yellow-400/10' },
  4: { label: 'P4', color: 'text-blue-400 bg-blue-400/10' },
  5: { label: 'P5', color: 'text-[var(--color-muted)] bg-[var(--color-surface-2)]' },
}

function PriorityBadge({ priority }: { priority: number }) {
  const cfg = PRIORITY_CONFIG[priority] ?? PRIORITY_CONFIG[3]
  return (
    <span className={cn('text-[9px] font-bold px-1 py-0.5 rounded', cfg.color)}>
      {cfg.label}
    </span>
  )
}

// ── Draggable card (board view) ────────────────────────────────────────────────

function DraggableCard({ task, onSelect }: { task: Task; onSelect: (t: Task) => void }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: task.id })
  const resultPreview = task.result?.split('\n')[0]?.slice(0, 60)
  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      onClick={() => onSelect(task)}
      className={cn(
        'mx-1 p-2.5 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)]',
        'text-xs hover:border-[var(--color-accent)]/30 transition-colors cursor-pointer',
        isDragging && 'opacity-40',
      )}
    >
      <div className="flex items-start justify-between gap-1 mb-0.5">
        <p className="text-[var(--color-secondary)] font-medium line-clamp-2">{task.title}</p>
        <PriorityBadge priority={task.priority} />
      </div>
      {(task.status === 'completed' || task.status === 'failed') && resultPreview && (
        <p className="text-[var(--color-muted)] mt-0.5 line-clamp-1">{resultPreview}</p>
      )}
      {task.agent_name && (
        <p className="text-[var(--color-muted)] mt-1 flex items-center gap-1">
          <Robot size={10} /> {task.agent_name}
        </p>
      )}
    </div>
  )
}

// ── Droppable column (board view) ──────────────────────────────────────────────

function BoardColumn({
  status,
  tasks,
  onSelect,
}: {
  status: Task['status']
  tasks: Task[]
  onSelect: (t: Task) => void
}) {
  const cfg = STATUS_CONFIG[status]
  const { setNodeRef, isOver } = useDroppable({ id: status })
  return (
    <div className="flex-1 min-w-[160px]">
      <div className={cn('text-xs font-semibold px-3 py-2 rounded-t-lg border-b border-[var(--color-border)]', cfg.bg)}>
        <span className={cfg.color}>{cfg.label}</span>
        <span className="ml-2 text-[var(--color-muted)]">({tasks.length})</span>
      </div>
      <div
        ref={setNodeRef}
        className={cn(
          'space-y-2 pt-2 min-h-[120px] rounded-b-lg transition-colors',
          isOver && 'bg-[var(--color-accent)]/5',
        )}
      >
        {tasks.map((task) => (
          <DraggableCard key={task.id} task={task} onSelect={onSelect} />
        ))}
      </div>
    </div>
  )
}

// ── List row ───────────────────────────────────────────────────────────────────

function TaskRow({ task, onSelect }: { task: Task; onSelect: (t: Task) => void }) {
  const cfg = STATUS_CONFIG[task.status] ?? STATUS_CONFIG.queued
  const resultPreview = (task.status === 'completed' || task.status === 'failed')
    ? task.result?.split('\n')[0]?.slice(0, 60)
    : undefined
  return (
    <div
      onClick={() => onSelect(task)}
      className="flex items-center gap-3 px-4 py-3 border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-surface-2)] transition-colors group cursor-pointer"
    >
      <PriorityBadge priority={task.priority} />
      <div className="flex-1 min-w-0">
        <p className="text-sm text-[var(--color-secondary)] truncate">{task.title}</p>
        {resultPreview ? (
          <p className="text-xs text-[var(--color-muted)] truncate mt-0.5">{resultPreview}</p>
        ) : task.prompt ? (
          <p className="text-xs text-[var(--color-muted)] truncate mt-0.5">{task.prompt}</p>
        ) : null}
      </div>
      {task.agent_name && (
        <Badge variant="outline" className="text-[10px] shrink-0 gap-1 text-[var(--color-muted)]">
          <Robot size={10} weight="fill" />
          <span className="hidden sm:inline truncate max-w-[80px]">{task.agent_name}</span>
        </Badge>
      )}
      <Badge
        className={cn('text-[10px] shrink-0', cfg.color, cfg.bg)}
        variant="outline"
      >
        {cfg.label}
      </Badge>
    </div>
  )
}

// ── Main component ─────────────────────────────────────────────────────────────

interface TaskListProps {
  statusFilter?: Task['status'] | 'all'
  onTaskSelect?: (task: Task) => void
}

export function TaskList({ statusFilter = 'all', onTaskSelect }: TaskListProps) {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const [view, setView] = useState<'list' | 'board'>('list')
  const [showCreate, setShowCreate] = useState(false)
  const [newTitle, setNewTitle] = useState('')
  const [newPrompt, setNewPrompt] = useState('')
  const [newPriority, setNewPriority] = useState('3')
  const [newAgentId, setNewAgentId] = useState('__none__')
  const [startImmediately, setStartImmediately] = useState(false)
  const [draggingTask, setDraggingTask] = useState<Task | null>(null)

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  const { data: allTasks = [], isLoading, isFetching, isError: tasksError } = useQuery({
    queryKey: ['tasks'],
    queryFn: () => fetchTasks(),
    staleTime: 30_000,
    refetchInterval: 10_000,
  })
  // Show skeleton on initial load (no data yet, still fetching)
  const showSkeleton = isLoading || (isFetching && allTasks.length === 0)

  const { data: agents = [] } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
  })

  const { mutate: doCreate, isPending: isCreating } = useMutation({
    mutationFn: async () => {
      const newTask = await createTask({
        title: newTitle.trim(),
        prompt: newPrompt.trim(),
        priority: parseInt(newPriority, 10),
        agent_id: newAgentId !== '__none__' ? newAgentId : undefined,
      })
      if (startImmediately) {
        await startTask(newTask.id)
      }
      return newTask
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      setNewTitle('')
      setNewPrompt('')
      setNewPriority('3')
      setNewAgentId('__none__')
      setStartImmediately(false)
      setShowCreate(false)
    },
    onError: (err: unknown) => addToast({ message: isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to create task', variant: 'error' }),
  })

  const { mutate: doUpdateStatus } = useMutation({
    mutationFn: ({ id, status }: { id: string; status: Task['status'] }) =>
      updateTask(id, { status }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
    onError: (err: unknown) => addToast({ message: isApiError(err) ? err.userMessage : err instanceof Error ? err.message : 'Failed to update task', variant: 'error' }),
  })

  const tasks = statusFilter === 'all'
    ? allTasks
    : allTasks.filter((t) => t.status === statusFilter)

  const tasksByStatus = BOARD_COLUMNS.reduce<Record<string, Task[]>>((acc, s) => {
    acc[s] = allTasks.filter((t) => t.status === s)
    return acc
  }, {})

  function handleDragStart(event: DragStartEvent) {
    const task = allTasks.find((t) => t.id === event.active.id)
    setDraggingTask(task ?? null)
  }

  function handleDragEnd(event: DragEndEvent) {
    setDraggingTask(null)
    const { active, over } = event
    if (!over || active.id === over.id) return
    const newStatus = over.id as Task['status']
    if (!BOARD_COLUMNS.includes(newStatus)) return
    const task = allTasks.find((t) => t.id === active.id)
    if (!task || task.status === newStatus) return
    doUpdateStatus({ id: task.id, status: newStatus })
  }

  const handleSelect = (task: Task) => onTaskSelect?.(task)

  return (
    <div className="flex flex-col">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-[var(--color-border)]">
        <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">Tasks</h2>
        <span className="text-xs text-[var(--color-muted)]">({tasks.length})</span>
        <div className="ml-auto flex items-center gap-2">
          {/* View toggle */}
          <div className="flex rounded-md border border-[var(--color-border)] overflow-hidden">
            <button
              type="button"
              onClick={() => setView('list')}
              className={cn(
                'px-2 py-1.5 transition-colors',
                view === 'list'
                  ? 'bg-[var(--color-surface-2)] text-[var(--color-secondary)]'
                  : 'text-[var(--color-muted)] hover:text-[var(--color-secondary)]',
              )}
              aria-label="List view"
            >
              <Rows size={13} />
            </button>
            <button
              type="button"
              onClick={() => setView('board')}
              className={cn(
                'px-2 py-1.5 border-l border-[var(--color-border)] transition-colors',
                view === 'board'
                  ? 'bg-[var(--color-surface-2)] text-[var(--color-secondary)]'
                  : 'text-[var(--color-muted)] hover:text-[var(--color-secondary)]',
              )}
              aria-label="Board view"
            >
              <SquaresFour size={13} />
            </button>
          </div>

          <Button
            size="sm"
            onClick={() => setShowCreate((v) => !v)}
            className="h-7 px-2 gap-1 text-xs"
          >
            <Plus size={12} weight="bold" /> Task
          </Button>
        </div>
      </div>

      {/* Create task inline */}
      {showCreate && (
        <div className="flex flex-col gap-2 px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface-2)]">
          <Input
            value={newTitle}
            onChange={(e) => setNewTitle(e.target.value)}
            placeholder="Title..."
            className="h-7 text-xs"
            autoFocus
            onKeyDown={(e) => {
              if (e.key === 'Escape') setShowCreate(false)
            }}
          />
          <Textarea
            value={newPrompt}
            onChange={(e) => setNewPrompt(e.target.value)}
            placeholder="Prompt / Instructions..."
            className="text-xs min-h-[60px]"
          />
          <div className="flex items-center gap-2">
            {/* Priority */}
            <SmartSelect
              value={newPriority}
              onValueChange={setNewPriority}
              placeholder="Priority"
              triggerClassName="h-7 text-xs w-[90px] shrink-0"
              items={[1, 2, 3, 4, 5].map((p) => ({ value: String(p), label: `P${p}` }))}
            />
            {/* Agent */}
            <SmartSelect
              value={newAgentId}
              onValueChange={setNewAgentId}
              placeholder="Assign agent..."
              triggerClassName="h-7 text-xs flex-1"
              items={[
                { value: '__none__', label: 'Unassigned' },
                ...agents.map((a) => ({ value: a.id, label: a.name })),
              ]}
            />
          </div>
          {/* Start immediately checkbox */}
          <label className="flex items-center gap-2 text-xs text-[var(--color-muted)] cursor-pointer select-none">
            <input
              type="checkbox"
              checked={startImmediately}
              onChange={(e) => setStartImmediately(e.target.checked)}
              className="accent-[var(--color-accent)] w-3.5 h-3.5"
            />
            <Play size={11} />
            Start immediately
          </label>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              className="h-7 px-3 text-xs"
              onClick={() => doCreate()}
              disabled={!newTitle.trim() || !newPrompt.trim() || isCreating}
            >
              {isCreating ? 'Adding...' : 'Add'}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-xs"
              onClick={() => setShowCreate(false)}
            >
              Cancel
            </Button>
          </div>
        </div>
      )}

      {/* Content */}
      <div>
        {tasksError ? (
          <div className="flex flex-col items-center justify-center py-12 text-center gap-2">
            <p className="text-sm text-[var(--color-error)]">Could not load tasks.</p>
            <p className="text-xs text-[var(--color-muted)]">Check your connection and try refreshing.</p>
          </div>
        ) : showSkeleton ? (
          <div className="space-y-1 p-2">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-12 rounded border border-[var(--color-border)] bg-[var(--color-surface-1)] animate-pulse" />
            ))}
          </div>
        ) : tasks.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-center gap-2">
            <p className="text-sm text-[var(--color-muted)]">No tasks yet.</p>
            <Button variant="ghost" size="sm" className="text-xs" onClick={() => setShowCreate(true)}>
              <Plus size={11} /> Create the first task
            </Button>
          </div>
        ) : view === 'list' ? (
          <div className="rounded-lg border border-[var(--color-border)] overflow-hidden mx-4 my-3">
            {tasks.map((task) => (
              <TaskRow key={task.id} task={task} onSelect={handleSelect} />
            ))}
          </div>
        ) : (
          <DndContext sensors={sensors} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
            <div className="flex gap-3 px-4 py-3 overflow-x-auto min-h-[300px]">
              {BOARD_COLUMNS.map((status) => (
                <BoardColumn
                  key={status}
                  status={status}
                  tasks={tasksByStatus[status] ?? []}
                  onSelect={handleSelect}
                />
              ))}
            </div>
            <DragOverlay>
              {draggingTask ? (
                <div className="mx-1 p-2.5 rounded-lg border border-[var(--color-accent)]/50 bg-[var(--color-surface-2)] text-xs shadow-lg rotate-1">
                  <p className="text-[var(--color-secondary)] font-medium">{draggingTask.title}</p>
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>
        )}
      </div>
    </div>
  )
}
