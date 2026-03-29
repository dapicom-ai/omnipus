import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Robot } from '@phosphor-icons/react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useUiStore } from '@/store/ui'
import { createAgent } from '@/lib/api'
import type { Agent } from '@/lib/api'

const AVAILABLE_MODELS = [
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5-20251001',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-1.5-pro',
]

const AVATAR_COLORS = [
  '#D4AF37', '#10B981', '#3B82F6', '#8B5CF6',
  '#EF4444', '#F97316', '#EC4899', '#06B6D4',
]

interface CreateAgentModalProps {
  /** Override modal open state (optional — defaults to Zustand store) */
  open?: boolean
  /** Override close handler (optional — defaults to Zustand store) */
  onClose?: () => void
  /** Override create handler (optional — defaults to REST API) */
  onCreate?: (data: Partial<Agent>) => Promise<void>
}

export function CreateAgentModal({ open: openProp, onClose: onCloseProp, onCreate: onCreateProp }: CreateAgentModalProps = {}) {
  const { createAgentModalOpen, closeCreateAgentModal } = useUiStore()
  const queryClient = useQueryClient()

  const isOpen = openProp !== undefined ? openProp : createAgentModalOpen
  const handleClose = onCloseProp ?? closeCreateAgentModal

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [model, setModel] = useState('')
  const [color, setColor] = useState(AVATAR_COLORS[0])
  const [nameError, setNameError] = useState('')

  const { mutate: doCreate, isPending } = useMutation({
    mutationFn: async (data: Partial<Agent>) => {
      if (onCreateProp) {
        await onCreateProp(data)
        return data as Agent
      }
      return createAgent(data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      handleClose()
      resetForm()
    },
    onError: (err: Error) => {
      useUiStore.getState().addToast({ message: err.message, variant: 'error' })
    },
  })

  const resetForm = () => {
    setName('')
    setDescription('')
    setModel('')
    setColor(AVATAR_COLORS[0])
    setNameError('')
  }

  const handleCreate = () => {
    if (!name.trim()) {
      setNameError('Name is required')
      return
    }
    doCreate({ name: name.trim(), description, model: model || undefined, color, type: 'custom' })
  }

  return (
    <DialogPrimitive.Root open={isOpen} onOpenChange={(open) => !open && handleClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content className="fixed left-[50%] top-[50%] z-50 w-full sm:max-w-lg translate-x-[-50%] translate-y-[-50%] border border-[var(--color-border)] bg-[var(--color-surface-1)] rounded-xl p-6 shadow-xl data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95">
          <DialogPrimitive.Title className="font-headline text-lg font-bold text-[var(--color-secondary)] mb-1">
            Create Agent
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="text-sm text-[var(--color-muted)] mb-5">
            Configure a new custom agent with a persona, model, and tools.
          </DialogPrimitive.Description>

          <div className="space-y-4">
            {/* Avatar color picker */}
            <div>
              <label className="text-xs font-medium text-[var(--color-muted)] mb-2 block">
                Avatar Color
              </label>
              <div className="flex items-center gap-3">
                <div
                  className="w-10 h-10 rounded-full flex items-center justify-center shrink-0 font-bold text-sm"
                  style={{ backgroundColor: color }}
                >
                  {name ? (
                    <span className="text-[var(--color-primary)]">{name.charAt(0).toUpperCase()}</span>
                  ) : (
                    <Robot size={18} className="text-[var(--color-primary)]" />
                  )}
                </div>
                <div className="flex gap-2 flex-wrap">
                  {AVATAR_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      onClick={() => setColor(c)}
                      className={`w-6 h-6 rounded-full transition-transform ${color === c ? 'ring-2 ring-[var(--color-secondary)] ring-offset-2 ring-offset-[var(--color-surface-1)] scale-110' : 'hover:scale-110'}`}
                      style={{ backgroundColor: c }}
                      aria-label={`Select color ${c}`}
                    />
                  ))}
                </div>
              </div>
            </div>

            {/* Name */}
            <div>
              <label htmlFor="agent-name" className="text-xs font-medium text-[var(--color-muted)] mb-1.5 block">
                Name <span className="text-[var(--color-error)]">*</span>
              </label>
              <Input
                id="agent-name"
                value={name}
                onChange={(e) => {
                  setName(e.target.value)
                  if (e.target.value.trim()) setNameError('')
                }}
                placeholder="e.g. Research Assistant"
                className={nameError ? 'border-[var(--color-error)]' : ''}
                autoFocus
              />
              {nameError && (
                <p className="mt-1 text-xs text-[var(--color-error)]">{nameError}</p>
              )}
            </div>

            {/* Description */}
            <div>
              <label htmlFor="agent-description" className="text-xs font-medium text-[var(--color-muted)] mb-1.5 block">
                Description
              </label>
              <Textarea
                id="agent-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What does this agent do?"
                rows={2}
              />
            </div>

            {/* Model */}
            <div>
              <label className="text-xs font-medium text-[var(--color-muted)] mb-1.5 block">
                Model
              </label>
              <Select value={model || '__default__'} onValueChange={(v) => setModel(v === '__default__' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue placeholder="Use provider default" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__default__">Use provider default</SelectItem>
                  {AVAILABLE_MODELS.map((m) => (
                    <SelectItem key={m} value={m}>{m}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 mt-6">
            <Button
              variant="outline"
              onClick={() => { handleClose(); resetForm() }}
              disabled={isPending}
            >
              Cancel
            </Button>
            <Button onClick={handleCreate} disabled={isPending}>
              {isPending ? 'Creating...' : 'Create Agent'}
            </Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
