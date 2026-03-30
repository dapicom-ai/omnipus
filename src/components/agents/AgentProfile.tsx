import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Robot, FloppyDisk } from '@phosphor-icons/react'
import { useNavigate } from '@tanstack/react-router'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { fetchAgent, updateAgent } from '@/lib/api'
import { useUiStore } from '@/store/ui'

const AVAILABLE_MODELS = [
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-haiku-4-5-20251001',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-1.5-pro',
]

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

  const [model, setModel] = useState('')
  const [useGlobalRateLimits, setUseGlobalRateLimits] = useState(true)

  // Initialize local state when data loads
  const [initialized, setInitialized] = useState(false)
  if (agent && !initialized) {
    setModel(agent.model ?? '')
    setUseGlobalRateLimits(agent.rate_limits?.use_global_defaults ?? true)
    setInitialized(true)
  }

  const { mutate: doUpdate, isPending: isSaving } = useMutation({
    mutationFn: (data: Parameters<typeof updateAgent>[1]) => updateAgent(agentId, data),
    onSuccess: () => {
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
          className="w-14 h-14 rounded-full flex items-center justify-center text-lg font-bold shrink-0"
          style={{ backgroundColor: agent.color ?? 'var(--color-surface-3)' }}
        >
          <Robot size={22} className="text-[var(--color-primary)]" />
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
            onClick={() => doUpdate({ model, rate_limits: { use_global_defaults: useGlobalRateLimits } })}
            disabled={isSaving}
          >
            <FloppyDisk size={14} weight="bold" />
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
        )}
      </div>

      <Separator />

      {/* Model section */}
      <section>
        <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">Model</h2>
        <Select
          value={model || '__default__'}
          onValueChange={(v) => setModel(v === '__default__' ? '' : v)}
          disabled={!canEdit}
        >
          <SelectTrigger>
            <SelectValue placeholder="Provider default" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__default__">Provider default</SelectItem>
            {AVAILABLE_MODELS.map((m) => (
              <SelectItem key={m} value={m}>{m}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </section>

      {/* Rate limits section (core/custom only) */}
      {agent.type !== 'system' && (
        <>
          <Separator />
          <section>
            <h2 className="font-headline font-bold text-sm text-[var(--color-secondary)] mb-3">Rate Limits</h2>
            <div className="flex items-center justify-between py-2">
              <div>
                <p className="text-sm text-[var(--color-secondary)]">Use global defaults</p>
                <p className="text-xs text-[var(--color-muted)]">Inherit rate limits from global settings</p>
              </div>
              <Switch
                checked={useGlobalRateLimits}
                onCheckedChange={setUseGlobalRateLimits}
              />
            </div>
            {!useGlobalRateLimits && (
              <p className="text-xs text-[var(--color-muted)] mt-2">
                Per-agent overrides are configurable when global defaults are disabled.
                {/* TODO: wire per-agent limit fields */}
              </p>
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
            <div className="grid grid-cols-3 gap-4">
              <StatCard label="Sessions" value={agent.stats.total_sessions.toString()} />
              <StatCard
                label="Total Tokens"
                value={
                  agent.stats.total_tokens >= 1000
                    ? `${(agent.stats.total_tokens / 1000).toFixed(1)}k`
                    : agent.stats.total_tokens.toString()
                }
              />
              <StatCard
                label="Total Cost"
                value={`$${agent.stats.total_cost.toFixed(4)}`}
              />
            </div>
          </section>
        </>
      )}

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
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-3 text-center">
      <div className="font-headline font-bold text-lg text-[var(--color-secondary)]">{value}</div>
      <div className="text-xs text-[var(--color-muted)] mt-0.5">{label}</div>
    </div>
  )
}
