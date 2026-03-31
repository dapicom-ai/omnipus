import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Robot,
  Brain,
  Lightbulb,
  MagnifyingGlass,
  PencilSimple,
  Code,
  Chat,
  Gear,
  Shield,
  Rocket,
  CaretDown,
  CaretUp,
} from '@phosphor-icons/react'
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
import { useQuery } from '@tanstack/react-query'
import { useUiStore } from '@/store/ui'
import { createAgent, fetchProviders } from '@/lib/api'
import type { Agent } from '@/lib/api'
import { AVATAR_COLORS } from '@/lib/constants'

const ICON_OPTIONS = [
  { name: 'Robot', component: Robot },
  { name: 'Brain', component: Brain },
  { name: 'Lightbulb', component: Lightbulb },
  { name: 'MagnifyingGlass', component: MagnifyingGlass },
  { name: 'PencilSimple', component: PencilSimple },
  { name: 'Code', component: Code },
  { name: 'Chat', component: Chat },
  { name: 'Gear', component: Gear },
  { name: 'Shield', component: Shield },
  { name: 'Rocket', component: Rocket },
] as const

type IconName = typeof ICON_OPTIONS[number]['name']

function getIconComponent(name: IconName) {
  return ICON_OPTIONS.find((o) => o.name === name)?.component ?? Robot
}

// Fallback model list when no providers are connected
const FALLBACK_MODELS = [
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5-20251001',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-1.5-pro',
]

interface CreateAgentModalProps {
  /** Override modal open state (optional — defaults to Zustand store) */
  open?: boolean
  /** Override close handler (optional — defaults to Zustand store) */
  onClose?: () => void
  /** Override create handler (optional — defaults to REST API) */
  onCreate?: (data: Partial<Agent>) => Promise<void>
}

export function CreateAgentModal({ open: openProp, onClose: onCloseProp, onCreate: onCreateProp }: CreateAgentModalProps) {
  const { createAgentModalOpen, closeCreateAgentModal } = useUiStore()
  const queryClient = useQueryClient()

  const isOpen = openProp !== undefined ? openProp : createAgentModalOpen
  const handleClose = onCloseProp ?? closeCreateAgentModal

  const { data: providers = [], isError: providersError } = useQuery({
    queryKey: ['providers'],
    queryFn: fetchProviders,
    enabled: isOpen,
  })
  const connectedModels = providers.filter((p) => p.status === 'connected').flatMap((p) => p.models ?? [])
  const availableModels = connectedModels.length > 0 ? connectedModels : FALLBACK_MODELS

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [model, setModel] = useState('')
  const [color, setColor] = useState(AVATAR_COLORS[0])
  const [icon, setIcon] = useState<IconName>('Robot')
  const [temperature, setTemperature] = useState(1.0)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [nameError, setNameError] = useState('')

  const resetForm = () => {
    setName('')
    setDescription('')
    setModel('')
    setColor(AVATAR_COLORS[0])
    setIcon('Robot')
    setTemperature(1.0)
    setAdvancedOpen(false)
    setNameError('')
  }

  // Reset form state whenever the modal opens so stale values are not shown
  useEffect(() => {
    if (isOpen) {
      resetForm()
    }
    // resetForm references stable setState callbacks — isOpen is the only meaningful dep
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen])

  const AvatarIcon = getIconComponent(icon)

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

  const handleCreate = () => {
    if (!name.trim()) {
      setNameError('Name is required')
      return
    }
    doCreate({
      name: name.trim(),
      description,
      model: model || undefined,
      color,
      icon,
      type: 'custom',
      model_params: { temperature },
    })
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
            {/* Avatar preview + color + icon */}
            <div>
              <label className="text-xs font-medium text-[var(--color-muted)] mb-2 block">
                Avatar
              </label>
              <div className="flex items-start gap-4">
                {/* Preview */}
                <div
                  className="w-12 h-12 rounded-full flex items-center justify-center shrink-0"
                  style={{ backgroundColor: color }}
                >
                  <AvatarIcon size={20} className="text-[var(--color-primary)]" />
                </div>

                <div className="flex-1 space-y-3">
                  {/* Color palette */}
                  <div>
                    <p className="text-[10px] text-[var(--color-muted)] mb-1.5">Color</p>
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

                  {/* Icon grid */}
                  <div>
                    <p className="text-[10px] text-[var(--color-muted)] mb-1.5">Icon</p>
                    <div className="grid grid-cols-5 gap-1.5">
                      {ICON_OPTIONS.map(({ name: iconName, component: IconComp }) => (
                        <button
                          key={iconName}
                          type="button"
                          onClick={() => setIcon(iconName)}
                          title={iconName}
                          className={`flex items-center justify-center w-8 h-8 rounded-md transition-colors ${
                            icon === iconName
                              ? 'bg-[var(--color-accent)] text-[var(--color-primary)]'
                              : 'bg-[var(--color-surface-2)] text-[var(--color-muted)] hover:text-[var(--color-secondary)]'
                          }`}
                        >
                          <IconComp size={16} />
                        </button>
                      ))}
                    </div>
                  </div>
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
              {providersError && (
                <div className="mb-2 rounded-md border border-[var(--color-error)]/40 bg-[var(--color-error)]/10 px-3 py-2">
                  <p className="text-xs text-[var(--color-error)] font-medium">Provider list unavailable</p>
                  <p className="text-xs text-[var(--color-error)]/80 mt-0.5">
                    Could not load connected providers. The model list below may not reflect what is actually configured.
                    Verify your provider settings before creating this agent.
                  </p>
                </div>
              )}
              <Select value={model || '__default__'} onValueChange={(v) => setModel(v === '__default__' ? '' : v)}>
                <SelectTrigger>
                  <SelectValue placeholder="Use provider default" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__default__">Use provider default</SelectItem>
                  {availableModels.map((m) => (
                    <SelectItem key={m} value={m}>{m}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Advanced model params */}
            <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] overflow-hidden">
              <button
                type="button"
                onClick={() => setAdvancedOpen((o) => !o)}
                className="flex items-center justify-between w-full px-3 py-2.5 text-sm font-medium text-[var(--color-secondary)] hover:text-[var(--color-accent)] transition-colors"
              >
                <span>Advanced</span>
                {advancedOpen ? <CaretUp size={13} /> : <CaretDown size={13} />}
              </button>
              {advancedOpen && (
                <div className="px-3 pb-3 border-t border-[var(--color-border)] pt-3 space-y-1">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-[var(--color-muted)]">Temperature</span>
                    <span className="text-xs font-mono text-[var(--color-secondary)]">{temperature.toFixed(2)}</span>
                  </div>
                  <input
                    type="range"
                    min={0}
                    max={2}
                    step={0.05}
                    value={temperature}
                    onChange={(e) => setTemperature(Number(e.target.value))}
                    className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                    style={{
                      background: `linear-gradient(to right, var(--color-accent) 0%, var(--color-accent) ${(temperature / 2) * 100}%, var(--color-border) ${(temperature / 2) * 100}%, var(--color-border) 100%)`,
                    }}
                  />
                </div>
              )}
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
