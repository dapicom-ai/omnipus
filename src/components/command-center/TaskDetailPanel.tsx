import { useState, useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchAgents, updateTask } from '@/lib/api'
import type { Task } from '@/lib/api'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { useUiStore } from '@/store/ui'

interface TaskDetailPanelProps {
  task: Task | null
  onClose: () => void
}

const STATUS_OPTIONS: { value: Task['status']; label: string }[] = [
  { value: 'inbox', label: 'Inbox' },
  { value: 'next', label: 'Next' },
  { value: 'active', label: 'Active' },
  { value: 'waiting', label: 'Waiting' },
  { value: 'done', label: 'Done' },
]

export function TaskDetailPanel({ task, onClose }: TaskDetailPanelProps) {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const [descDraft, setDescDraft] = useState('')

  useEffect(() => {
    setDescDraft(task?.description ?? '')
  }, [task?.id, task?.description])

  const { data: agents = [] } = useQuery({ queryKey: ['agents'], queryFn: fetchAgents })

  const { mutate: doUpdate } = useMutation({
    mutationFn: (data: Partial<Task>) => {
      if (!task) return Promise.reject(new Error('No task selected'))
      return updateTask(task.id, data)
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['tasks'] }),
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const formatDate = (iso?: string) => {
    if (!iso) return '—'
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(iso))
  }

  return (
    <Sheet open={task != null} onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent side="right" className="w-[360px] sm:w-[420px] overflow-y-auto">
        <SheetHeader className="mb-5">
          <SheetTitle className="pr-6 leading-snug">{task?.name ?? ''}</SheetTitle>
        </SheetHeader>

        {task && (
          <div className="space-y-5">
            {/* Description */}
            <Field label="Description">
              <Textarea
                value={descDraft}
                onChange={(e) => setDescDraft(e.target.value)}
                onBlur={() => {
                  const trimmed = descDraft.trim()
                  if (trimmed !== (task.description ?? '').trim()) {
                    doUpdate({ description: trimmed || undefined })
                  }
                }}
                placeholder="Add instructions or context..."
                className="text-xs min-h-[80px]"
              />
            </Field>

            {/* Status */}
            <Field label="Status">
              <Select
                value={task.status}
                onValueChange={(val) => doUpdate({ status: val as Task['status'] })}
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {STATUS_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value} className="text-xs">
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {/* Agent */}
            <Field label="Agent">
              <Select
                value={task.agent_id ?? '__none__'}
                onValueChange={(val) =>
                  doUpdate({ agent_id: val === '__none__' ? undefined : val })
                }
              >
                <SelectTrigger className="h-8 text-xs">
                  <SelectValue placeholder="Unassigned" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__" className="text-xs">Unassigned</SelectItem>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.id} className="text-xs">
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {/* Dates */}
            <Field label="Created">
              <span className="text-xs text-[var(--color-muted)]">{formatDate(task.created_at)}</span>
            </Field>
            <Field label="Updated">
              <span className="text-xs text-[var(--color-muted)]">{formatDate(task.updated_at)}</span>
            </Field>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <p className="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted)]">{label}</p>
      {children}
    </div>
  )
}
