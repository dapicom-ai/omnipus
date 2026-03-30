import { useState, useEffect, useRef, KeyboardEvent } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
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
  FloppyDisk,
  X,
  CaretDown,
  CaretUp,
  Scroll,
  NotePencil,
} from '@phosphor-icons/react'
import { useNavigate } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
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
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import {
  fetchAgent,
  updateAgent,
  fetchProviders,
  fetchAgentSessions,
  fetchActivity,
  type AgentSession,
  type ActivityEvent,
} from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { AVATAR_COLORS } from '@/lib/constants'

// Fallback model list when no providers are connected
const FALLBACK_MODELS = [
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5-20251001',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-1.5-pro',
]

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

function getIconComponent(name: string | undefined) {
  const match = ICON_OPTIONS.find((o) => o.name === name)
  return match?.component ?? Robot
}

interface AgentProfileProps {
  agentId: string
}

export function AgentProfile({ agentId }: AgentProfileProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { addToast } = useUiStore()

  const { data: agent, isLoading, isError } = useQuery({
    queryKey: ['agent', agentId],
    queryFn: () => fetchAgent(agentId),
  })

  const { data: providers = [], isError: providersError } = useQuery({
    queryKey: ['providers'],
    queryFn: fetchProviders,
  })

  const { data: agentSessions = [], isError: sessionsError } = useQuery({
    queryKey: ['agent-sessions', agentId],
    queryFn: () => fetchAgentSessions(agentId),
  })

  const { data: allActivity = [], isError: activityError } = useQuery({
    queryKey: ['activity'],
    queryFn: fetchActivity,
    staleTime: 30_000,
  })

  const recentActivity = allActivity
    .filter((e) => e.agent_id === agentId)
    .slice(0, 5)

  const connectedModels = providers.filter((p) => p.status === 'connected').flatMap((p) => p.models ?? [])
  const usingFallbackModels = connectedModels.length === 0
  const availableModels = usingFallbackModels ? FALLBACK_MODELS : connectedModels

  const isDirtyRef = useRef(false)
  const markDirty = () => { isDirtyRef.current = true }

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [model, setModel] = useState('')
  const [selectedColor, setSelectedColor] = useState<string | undefined>(undefined)
  const [selectedIcon, setSelectedIcon] = useState<IconName>('Robot')
  const [fallbackModels, setFallbackModels] = useState<string[]>([])
  const [fallbackInput, setFallbackInput] = useState('')
  const [temperature, setTemperature] = useState(1.0)
  const [maxTokens, setMaxTokens] = useState(4096)
  const [topP, setTopP] = useState(1.0)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [useGlobalRateLimits, setUseGlobalRateLimits] = useState(true)
  const [maxLlmCallsPerHour, setMaxLlmCallsPerHour] = useState<number | ''>('')
  const [maxToolCallsPerMinute, setMaxToolCallsPerMinute] = useState<number | ''>('')
  const [maxCostPerDay, setMaxCostPerDay] = useState<number | ''>('')
  const [soul, setSoul] = useState('')
  const [instructions, setInstructions] = useState('')
  const [heartbeat, setHeartbeat] = useState('')

  useEffect(() => {
    if (!agent) return
    // Do not overwrite unsaved user edits on background refetch
    if (isDirtyRef.current) return
    setName(agent.name ?? '')
    setDescription(agent.description ?? '')
    setModel(agent.model ?? '')
    setSelectedColor(agent.color)
    setSelectedIcon((agent.icon as IconName) ?? 'Robot')
    setFallbackModels(agent.fallback_models ?? [])
    setTemperature(agent.model_params?.temperature ?? 1.0)
    setMaxTokens(agent.model_params?.max_tokens ?? 4096)
    setTopP(agent.model_params?.top_p ?? 1.0)
    setUseGlobalRateLimits(agent.rate_limits?.use_global_defaults ?? true)
    setMaxLlmCallsPerHour(agent.rate_limits?.max_llm_calls_per_hour ?? '')
    setMaxToolCallsPerMinute(agent.rate_limits?.max_tool_calls_per_minute ?? '')
    setMaxCostPerDay(agent.rate_limits?.max_cost_per_day ?? '')
    setSoul(agent.soul ?? '')
    setInstructions(agent.instructions ?? '')
    setHeartbeat(agent.heartbeat ?? '')
  }, [agent])

  const { mutate: doUpdate, isPending: isSaving } = useMutation({
    mutationFn: (data: Parameters<typeof updateAgent>[1]) => updateAgent(agentId, data),
    onSuccess: () => {
      isDirtyRef.current = false
      queryClient.invalidateQueries({ queryKey: ['agent', agentId] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      addToast({ message: 'Agent saved', variant: 'success' })
    },
    onError: (err: Error) => addToast({
      message: err.message.includes('501')
        ? 'Agent changes require editing config.json and restarting the gateway'
        : err.message,
      variant: 'error',
    }),
  })

  function handleSave() {
    doUpdate({
      name,
      description,
      model,
      color: selectedColor,
      icon: selectedIcon,
      fallback_models: fallbackModels.length > 0 ? fallbackModels : undefined,
      model_params: { temperature, max_tokens: maxTokens, top_p: topP },
      rate_limits: {
        use_global_defaults: useGlobalRateLimits,
        max_llm_calls_per_hour: maxLlmCallsPerHour !== '' ? maxLlmCallsPerHour : undefined,
        max_tool_calls_per_minute: maxToolCallsPerMinute !== '' ? maxToolCallsPerMinute : undefined,
        max_cost_per_day: maxCostPerDay !== '' ? maxCostPerDay : undefined,
      },
      soul,
      instructions,
    })
  }

  function addFallbackModel() {
    const trimmed = fallbackInput.trim()
    if (!trimmed || fallbackModels.includes(trimmed)) return
    setFallbackModels((prev) => [...prev, trimmed])
    setFallbackInput('')
  }

  function handleFallbackKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addFallbackModel()
    } else if (e.key === 'Backspace' && fallbackInput === '' && fallbackModels.length > 0) {
      setFallbackModels((prev) => prev.slice(0, -1))
    }
  }

  const recentSessions = agentSessions.slice(0, 10)
  const AvatarIcon = getIconComponent(selectedIcon)

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full text-[var(--color-muted)] text-sm">
        Loading agent...
      </div>
    )
  }

  if (isError || !agent) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-4">
        <p className="text-[var(--color-muted)] text-sm">Could not load agent.</p>
        <Button variant="outline" size="sm" onClick={() => navigate({ to: '/agents' })}>
          Back to Agents
        </Button>
      </div>
    )
  }

  const canEdit = agent.type !== 'system'

  return (
    <div className="max-w-2xl mx-auto px-4 py-6 space-y-6">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate({ to: '/agents' })}
          className="gap-1 text-[var(--color-muted)]"
        >
          <ArrowLeft size={14} /> Agents
        </Button>
      </div>

      <div className="flex items-center gap-4">
        <div
          className="w-14 h-14 rounded-full flex items-center justify-center shrink-0"
          style={{ backgroundColor: selectedColor ?? 'var(--color-surface-3)' }}
        >
          <AvatarIcon size={22} className="text-[var(--color-primary)]" />
        </div>
        <div className="min-w-0">
          <h1 className="font-headline text-xl font-bold text-[var(--color-secondary)]">{agent.name}</h1>
          <div className="flex items-center gap-2 mt-1">
            <Badge variant={agent.type === 'system' ? 'warning' : agent.type === 'core' ? 'secondary' : 'outline'}>
              {agent.type}
            </Badge>
            <span className="text-xs text-[var(--color-muted)]">{agent.description}</span>
          </div>
        </div>
        {canEdit && (
          <Button
            className="ml-auto gap-2"
            onClick={handleSave}
            disabled={isSaving}
          >
            <FloppyDisk size={14} weight="bold" />
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
        )}
      </div>

      <Separator />

      {/* Identity section */}
      {canEdit && (
        <>
          <section className="space-y-3">
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">Identity</h2>
            <div className="space-y-2">
              <Input
                value={name}
                onChange={(e) => { markDirty(); setName(e.target.value) }}
                placeholder="Agent name"
                className="text-sm"
              />
              <Textarea
                value={description}
                onChange={(e) => { markDirty(); setDescription(e.target.value) }}
                placeholder="Short description of this agent's purpose"
                rows={2}
                className="text-sm resize-none"
              />
            </div>

            {/* Color picker */}
            <div className="space-y-1.5">
              <p className="text-xs text-[var(--color-muted)]">Avatar color</p>
              <div className="flex gap-2">
                {AVATAR_COLORS.map((color) => (
                  <button
                    key={color}
                    type="button"
                    onClick={() => { markDirty(); setSelectedColor(color) }}
                    className="w-7 h-7 rounded-full transition-transform hover:scale-110 focus:outline-none focus:ring-2 focus:ring-[var(--color-accent)] focus:ring-offset-1 focus:ring-offset-[var(--color-primary)]"
                    style={{
                      backgroundColor: color,
                      boxShadow: selectedColor === color ? `0 0 0 2px var(--color-primary), 0 0 0 4px ${color}` : undefined,
                    }}
                    aria-label={color}
                  />
                ))}
              </div>
            </div>

            {/* Icon picker */}
            <div className="space-y-1.5">
              <p className="text-xs text-[var(--color-muted)]">Avatar icon</p>
              <Select
                value={selectedIcon}
                onValueChange={(v) => { markDirty(); setSelectedIcon(v as IconName) }}
              >
                <SelectTrigger className="w-48">
                  <div className="flex items-center gap-2">
                    <AvatarIcon size={14} />
                    <SelectValue />
                  </div>
                </SelectTrigger>
                <SelectContent>
                  {ICON_OPTIONS.map(({ name: iconName, component: IconComp }) => (
                    <SelectItem key={iconName} value={iconName}>
                      <div className="flex items-center gap-2">
                        <IconComp size={14} />
                        <span>{iconName}</span>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </section>
          <Separator />
        </>
      )}

      {/* Model section */}
      <section className="space-y-3">
        <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">Model</h2>
        {(usingFallbackModels || providersError) && (
          <p className="text-xs text-[var(--color-warning)]">
            {providersError
              ? 'Could not load providers — showing default model list.'
              : 'No providers connected — showing default model list. Connect a provider in Settings for accurate options.'}
          </p>
        )}
        <Select
          value={model || '__default__'}
          onValueChange={(v) => { markDirty(); setModel(v === '__default__' ? '' : v) }}
          disabled={!canEdit}
        >
          <SelectTrigger>
            <SelectValue placeholder="Provider default" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__default__">Provider default</SelectItem>
            {availableModels.map((m) => (
              <SelectItem key={m} value={m}>{m}</SelectItem>
            ))}
          </SelectContent>
        </Select>

        {/* Fallback models */}
        {canEdit && (
          <div className="space-y-1.5">
            <p className="text-xs text-[var(--color-muted)]">Fallback models (tried in order if primary fails)</p>
            <div className="flex flex-wrap gap-1.5 p-2 rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] min-h-[36px]">
              {fallbackModels.map((m) => (
                <span
                  key={m}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-mono bg-[var(--color-surface-2)] text-[var(--color-secondary)] border border-[var(--color-border)]"
                >
                  {m}
                  <button
                    type="button"
                    onClick={() => setFallbackModels((prev) => prev.filter((x) => x !== m))}
                    className="text-[var(--color-muted)] hover:text-[var(--color-error)] transition-colors"
                  >
                    <X size={10} />
                  </button>
                </span>
              ))}
              <input
                value={fallbackInput}
                onChange={(e) => { markDirty(); setFallbackInput(e.target.value) }}
                onKeyDown={handleFallbackKeyDown}
                onBlur={addFallbackModel}
                placeholder={fallbackModels.length === 0 ? 'Type a model name, press Enter' : ''}
                className="flex-1 min-w-[160px] bg-transparent text-xs text-[var(--color-secondary)] outline-none placeholder:text-[var(--color-muted)]"
              />
            </div>
          </div>
        )}

        {/* Advanced model params */}
        {canEdit && (
          <div className="rounded-md border border-[var(--color-border)] bg-[var(--color-surface-1)] overflow-hidden">
            <button
              type="button"
              onClick={() => setAdvancedOpen((o) => !o)}
              className="flex items-center justify-between w-full px-3 py-2.5 text-sm font-medium text-[var(--color-secondary)] hover:text-[var(--color-accent)] transition-colors"
            >
              <span>Advanced parameters</span>
              {advancedOpen ? <CaretUp size={13} /> : <CaretDown size={13} />}
            </button>
            {advancedOpen && (
              <div className="px-3 pb-3 space-y-4 border-t border-[var(--color-border)]">
                <RangeField
                  label="Temperature"
                  value={temperature}
                  min={0}
                  max={2}
                  step={0.05}
                  onChange={(v) => { markDirty(); setTemperature(v) }}
                  format={(v) => v.toFixed(2)}
                />
                <RangeField
                  label="Max tokens"
                  value={maxTokens}
                  min={256}
                  max={32768}
                  step={256}
                  onChange={(v) => { markDirty(); setMaxTokens(v) }}
                  format={(v) => v.toLocaleString()}
                />
                <RangeField
                  label="Top P"
                  value={topP}
                  min={0}
                  max={1}
                  step={0.01}
                  onChange={(v) => { markDirty(); setTopP(v) }}
                  format={(v) => v.toFixed(2)}
                />
              </div>
            )}
          </div>
        )}
      </section>

      {/* SOUL.md editor */}
      {canEdit && (
        <>
          <Separator />
          <section className="space-y-3">
            <div className="flex items-center gap-2">
              <Scroll size={14} className="text-[var(--color-accent)]" />
              <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">SOUL.md</h2>
            </div>
            <p className="text-xs text-[var(--color-muted)]">
              The agent's personality and system prompt — defines who the agent is and how it behaves.
            </p>
            <Textarea
              value={soul}
              onChange={(e) => { markDirty(); setSoul(e.target.value) }}
              placeholder={"# Soul\n\nDefine this agent's personality, expertise, and behavioral guidelines..."}
              rows={8}
              className="text-xs font-mono resize-none"
            />
            <Button
              size="sm"
              variant="outline"
              disabled={isSaving}
              onClick={() => doUpdate({ soul })}
            >
              <FloppyDisk size={13} weight="bold" className="mr-1.5" />
              Save SOUL.md
            </Button>
          </section>
        </>
      )}

      {/* Additional Instructions editor */}
      {canEdit && (
        <>
          <Separator />
          <section className="space-y-3">
            <div className="flex items-center gap-2">
              <NotePencil size={14} className="text-[var(--color-accent)]" />
              <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">Additional Instructions</h2>
            </div>
            <p className="text-xs text-[var(--color-muted)]">
              Extra instructions appended to the agent's context — task-specific guidance, constraints, or domain knowledge.
            </p>
            <Textarea
              value={instructions}
              onChange={(e) => { markDirty(); setInstructions(e.target.value) }}
              placeholder="Add specific instructions, constraints, or domain knowledge..."
              rows={6}
              className="text-xs font-mono resize-none"
            />
            <Button
              size="sm"
              variant="outline"
              disabled={isSaving}
              onClick={() => doUpdate({ instructions })}
            >
              <FloppyDisk size={13} weight="bold" className="mr-1.5" />
              Save Instructions
            </Button>
          </section>
        </>
      )}

      {/* HEARTBEAT.md editor */}
      {canEdit && (
        <>
          <Separator />
          <section className="space-y-3">
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">HEARTBEAT.md</h2>
            <p className="text-xs text-[var(--color-muted)]">
              The agent's persistent context — goals, preferences, and working memory.
            </p>
            <Textarea
              value={heartbeat}
              onChange={(e) => { markDirty(); setHeartbeat(e.target.value) }}
              placeholder="# Heartbeat&#10;&#10;Write persistent context for this agent..."
              rows={6}
              className="text-xs font-mono resize-none"
            />
            <Button
              size="sm"
              variant="outline"
              disabled={isSaving}
              onClick={() => doUpdate({ heartbeat })}
            >
              <FloppyDisk size={13} weight="bold" className="mr-1.5" />
              Save HEARTBEAT.md
            </Button>
          </section>
        </>
      )}

      {/* Rate limits section */}
      {agent.type !== 'system' && (
        <>
          <Separator />
          <section className="space-y-3">
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)]">Rate Limits</h2>
            <div className="flex items-center justify-between py-1">
              <div>
                <p className="text-sm text-[var(--color-secondary)]">Use global defaults</p>
                <p className="text-xs text-[var(--color-muted)]">Inherit rate limits from global settings</p>
              </div>
              <Switch
                checked={useGlobalRateLimits}
                onCheckedChange={(v) => { markDirty(); setUseGlobalRateLimits(v) }}
                disabled={!canEdit}
              />
            </div>
            {!useGlobalRateLimits && (
              <div className="space-y-2">
                <div className="flex items-center gap-3">
                  <label className="text-xs text-[var(--color-muted)] w-44 shrink-0">LLM calls / hour</label>
                  <Input
                    type="number"
                    min={0}
                    value={maxLlmCallsPerHour}
                    onChange={(e) => { markDirty(); setMaxLlmCallsPerHour(e.target.value === '' ? '' : Number(e.target.value)) }}
                    placeholder="Unlimited"
                    className="text-xs h-8"
                    disabled={!canEdit}
                  />
                </div>
                <div className="flex items-center gap-3">
                  <label className="text-xs text-[var(--color-muted)] w-44 shrink-0">Tool calls / minute</label>
                  <Input
                    type="number"
                    min={0}
                    value={maxToolCallsPerMinute}
                    onChange={(e) => { markDirty(); setMaxToolCallsPerMinute(e.target.value === '' ? '' : Number(e.target.value)) }}
                    placeholder="Unlimited"
                    className="text-xs h-8"
                    disabled={!canEdit}
                  />
                </div>
                <div className="flex items-center gap-3">
                  <label className="text-xs text-[var(--color-muted)] w-44 shrink-0">Max cost / day ($)</label>
                  <Input
                    type="number"
                    min={0}
                    step={0.01}
                    value={maxCostPerDay}
                    onChange={(e) => { markDirty(); setMaxCostPerDay(e.target.value === '' ? '' : Number(e.target.value)) }}
                    placeholder="Unlimited"
                    className="text-xs h-8"
                    disabled={!canEdit}
                  />
                </div>
              </div>
            )}
          </section>
        </>
      )}

      {/* Stats */}
      {agent.stats && (
        <>
          <Separator />
          <section>
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">Stats</h2>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <StatCard label="Sessions" value={agent.stats.total_sessions.toString()} />
              <StatCard
                label="Tokens"
                value={
                  agent.stats.total_tokens >= 1000
                    ? `${(agent.stats.total_tokens / 1000).toFixed(1)}k`
                    : agent.stats.total_tokens.toString()
                }
              />
              <StatCard label="Cost" value={`$${agent.stats.total_cost.toFixed(4)}`} />
              <StatCard
                label="Last active"
                value={
                  agent.stats.last_active
                    ? new Date(agent.stats.last_active).toLocaleDateString()
                    : '—'
                }
              />
            </div>
          </section>
        </>
      )}

      {/* Recent sessions */}
      <>
        <Separator />
        <section>
          <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">Recent Sessions</h2>
          {sessionsError ? (
            <p className="text-sm text-red-400">Failed to load sessions</p>
          ) : recentSessions.length > 0 ? (
            <div className="space-y-1">
              {recentSessions.map((s) => (
                <SessionRow key={s.id} session={s} />
              ))}
            </div>
          ) : (
            <p className="text-xs text-[var(--color-muted)]">No sessions yet.</p>
          )}
        </section>
      </>

      {/* Tools */}
      {agent.tools && agent.tools.length > 0 && (
        <>
          <Separator />
          <section>
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">
              Tools & Skills
            </h2>
            <div className="flex flex-wrap gap-2">
              {agent.tools.map((tool) => (
                <Badge key={tool} variant="outline" className="font-mono text-[10px]">
                  {tool}
                </Badge>
              ))}
            </div>
          </section>
        </>
      )}

      {/* Recent activity */}
      <Separator />
      <section>
        <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">Recent Activity</h2>
        {activityError ? (
          <p className="text-sm text-red-400">Failed to load activity</p>
        ) : recentActivity.length === 0 ? (
          <p className="text-xs text-[var(--color-muted)]">No recent activity for this agent.</p>
        ) : (
          <div className="space-y-1">
            {recentActivity.map((event) => (
              <ActivityRow key={event.id} event={event} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-3 text-center">
      <div className="font-headline font-bold text-base text-[var(--color-secondary)]">{value}</div>
      <div className="text-xs text-[var(--color-muted)] mt-0.5">{label}</div>
    </div>
  )
}

function SessionRow({ session }: { session: AgentSession }) {
  const date = new Date(session.updated_at ?? session.created_at)
  return (
    <div className="flex items-center justify-between px-3 py-2 rounded-md hover:bg-[var(--color-surface-1)] transition-colors">
      <span className="text-xs text-[var(--color-secondary)] truncate flex-1 min-w-0 mr-3">
        {session.title || 'Untitled session'}
      </span>
      <span className="text-[10px] text-[var(--color-muted)] shrink-0">
        {date.toLocaleDateString()}
      </span>
    </div>
  )
}

interface RangeFieldProps {
  label: string
  value: number
  min: number
  max: number
  step: number
  onChange: (v: number) => void
  format: (v: number) => string
}

function RangeField({ label, value, min, max, step, onChange, format }: RangeFieldProps) {
  return (
    <div className="space-y-1 pt-3">
      <div className="flex items-center justify-between">
        <span className="text-xs text-[var(--color-muted)]">{label}</span>
        <span className="text-xs font-mono text-[var(--color-secondary)]">{format(value)}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
        style={{
          background: `linear-gradient(to right, var(--color-accent) 0%, var(--color-accent) ${((value - min) / (max - min)) * 100}%, var(--color-border) ${((value - min) / (max - min)) * 100}%, var(--color-border) 100%)`,
        }}
      />
    </div>
  )
}

function ActivityRow({ event }: { event: ActivityEvent }) {
  const date = new Date(event.timestamp)
  return (
    <div className="flex items-start gap-3 px-3 py-2 rounded-md hover:bg-[var(--color-surface-1)] transition-colors">
      <span className="text-xs text-[var(--color-secondary)] flex-1 min-w-0 truncate">
        {event.summary}
      </span>
      <span className="text-[10px] text-[var(--color-muted)] shrink-0 mt-0.5">
        {date.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
      </span>
    </div>
  )
}
