import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Plus,
  Rows,
  SquaresFour,
  Robot,
  CurrencyDollar,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { fetchTasks, fetchAgents, createTask, updateTask } from '@/lib/api'
import type { Task } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { cn } from '@/lib/utils'

const STATUS_CONFIG = {
  inbox: { label: 'Inbox', color: 'text-[var(--color-muted)]', bg: 'bg-[var(--color-surface-2)]' },
  next: { label: 'Next', color: 'text-[var(--color-info)]', bg: 'bg-[var(--color-info)]/10' },
  active: { label: 'Active', color: 'text-[var(--color-success)]', bg: 'bg-[var(--color-success)]/10' },
  waiting: { label: 'Waiting', color: 'text-[var(--color-warning)]', bg: 'bg-[var(--color-warning)]/10' },
  done: { label: 'Done', color: 'text-[var(--color-muted)]', bg: 'bg-[var(--color-surface-1)]' },
} as const

// ── Draggable card (board view) ────────────────────────────────────────────────

function DraggableCard({ task, onSelect }: { task: Task; onSelect: (t: Task) => void }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: task.id })
  const descFirstLine = task.description?.split('\n')[0]
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
      <p className="text-[var(--color-secondary)] font-medium line-clamp-2">{task.name}</p>
      {descFirstLine && (
        <p className="text-[var(--color-muted)] mt-0.5 line-clamp-1">{descFirstLine}</p>
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
  const cfg = STATUS_CONFIG[task.status]
  return (
    <div
      onClick={() => onSelect(task)}
      className="flex items-center gap-3 px-4 py-3 border-b border-[var(--color-border)] last:border-0 hover:bg-[var(--color-surface-2)] transition-colors group cursor-pointer"
    >
      <div className="flex-1 min-w-0">
        <p className="text-sm text-[var(--color-secondary)] truncate">{task.name}</p>
        {task.description && (
          <p className="text-xs text-[var(--color-muted)] truncate mt-0.5">{task.description}</p>
        )}
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
      {task.cost != null && (
        <div className="flex items-center gap-0.5 text-[10px] text-[var(--color-muted)] shrink-0">
          <CurrencyDollar size={10} />
          {task.cost.toFixed(4)}
        </div>
      )}
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
  const [newTaskName, setNewTaskName] = useState('')
  const [newTaskDesc, setNewTaskDesc] = useState('')
  const [newTaskAgentId, setNewTaskAgentId] = useState('__none__')
  const [draggingTask, setDraggingTask] = useState<Task | null>(null)

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }))

  const { data: allTasks = [], isLoading, isError: tasksError } = useQuery({
    queryKey: ['tasks'],
    queryFn: fetchTasks,
    refetchInterval: 10_000,
  })

  const { data: agents = [] } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
  })

  const { mutate: doCreate, isPending: isCreating } = useMutation({
    mutationFn: () => createTask({
      name: newTaskName,
      description: newTaskDesc.trim() || undefined,
      agent_id: newTaskAgentId !== '__none__' ? newTaskAgentId : undefined,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      setNewTaskName('')
      setNewTaskDesc('')
      setNewTaskAgentId('__none__')
      setShowCreate(false)
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const { mutate: doUpdateStatus } = useMutation({
    mutationFn: ({ id, status }: { id: string; status: Task['status'] }) =>
      updateTask(id, { status }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const tasks = statusFilter === 'all'
    ? allTasks
    : allTasks.filter((t) => t.status === statusFilter)

  const statuses: Task['status'][] = ['inbox', 'next', 'active', 'waiting', 'done']
  const tasksByStatus = statuses.reduce<Record<string, Task[]>>((acc, s) => {
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
    if (!statuses.includes(newStatus)) return
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
            value={newTaskName}
            onChange={(e) => setNewTaskName(e.target.value)}
            placeholder="Task name..."
            className="h-7 text-xs"
            autoFocus
            onKeyDown={(e) => {
              if (e.key === 'Escape') setShowCreate(false)
            }}
          />
          <Textarea
            value={newTaskDesc}
            onChange={(e) => setNewTaskDesc(e.target.value)}
            placeholder="Description / instructions (optional)..."
            className="text-xs min-h-[60px]"
          />
          <div className="flex items-center gap-2">
            <Select value={newTaskAgentId} onValueChange={setNewTaskAgentId}>
              <SelectTrigger className="h-7 text-xs flex-1">
                <SelectValue placeholder="Assign agent..." />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__" className="text-xs">Unassigned</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={a.id} className="text-xs">{a.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              size="sm"
              className="h-7 px-3 text-xs"
              onClick={() => doCreate()}
              disabled={!newTaskName.trim() || isCreating}
            >
              Add
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
        ) : isLoading ? (
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
              {statuses.map((status) => (
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
                  <p className="text-[var(--color-secondary)] font-medium">{draggingTask.name}</p>
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>
        )}
      </div>
    </div>
  )
}
