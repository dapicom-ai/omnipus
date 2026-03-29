import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import { fetchConfig, updateConfig } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { DiagnosticsSection } from './DiagnosticsSection'

export function SecuritySection() {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data: config, isLoading } = useQuery({
    queryKey: ['config'],
    queryFn: fetchConfig,
  })

  const [policyMode, setPolicyMode] = useState<'allow' | 'deny'>('deny')
  const [execApproval, setExecApproval] = useState<'auto' | 'ask' | 'deny'>('ask')
  const [injectionLevel, setInjectionLevel] = useState<'off' | 'low' | 'medium' | 'high'>('medium')
  const [dailyCostCap, setDailyCostCap] = useState('')

  const [initialized, setInitialized] = useState(false)
  if (config && !initialized) {
    setPolicyMode(config.security.policy_mode)
    setExecApproval(config.security.exec_approval)
    setInjectionLevel(config.security.prompt_injection_level)
    setDailyCostCap(config.security.daily_cost_cap?.toString() ?? '')
    setInitialized(true)
  }

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: () =>
      updateConfig({
        security: {
          policy_mode: policyMode,
          exec_approval: execApproval,
          prompt_injection_level: injectionLevel,
          daily_cost_cap: dailyCostCap ? parseFloat(dailyCostCap) : undefined,
          rate_limits: config?.security.rate_limits ?? {},
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      addToast({ message: 'Security settings saved', variant: 'success' })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  if (isLoading) {
    return <div className="text-sm text-[var(--color-muted)]">Loading...</div>
  }

  // Mock today's spend for progress bar (TODO: wire from real spend endpoint)
  const todaySpend = 0.42
  const capValue = parseFloat(dailyCostCap) || 10
  const spendPercent = Math.min((todaySpend / capValue) * 100, 100)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">Security & Policy</h2>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Control agent behavior boundaries and resource limits.
          </p>
        </div>
        <Button size="sm" onClick={() => doSave()} disabled={isSaving} className="gap-1.5">
          <FloppyDisk size={13} weight="bold" />
          {isSaving ? 'Saving...' : 'Save'}
        </Button>
      </div>

      {/* Policy */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Policy</h3>

        <div className="space-y-3 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Default policy mode</p>
              <p className="text-xs text-[var(--color-muted)]">Whether agents are allowed or denied by default</p>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-[var(--color-muted)]">Deny</span>
              <Switch
                checked={policyMode === 'allow'}
                onCheckedChange={(v) => setPolicyMode(v ? 'allow' : 'deny')}
              />
              <span className="text-xs text-[var(--color-secondary)]">Allow</span>
            </div>
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Exec approval</p>
              <p className="text-xs text-[var(--color-muted)]">How shell command execution is handled</p>
            </div>
            <Select value={execApproval} onValueChange={(v) => setExecApproval(v as typeof execApproval)}>
              <SelectTrigger className="w-[120px] h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">Auto-allow</SelectItem>
                <SelectItem value="ask">Ask each time</SelectItem>
                <SelectItem value="deny">Always deny</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      {/* Prompt injection */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Prompt Injection Defense</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Detection level</p>
              <p className="text-xs text-[var(--color-muted)]">Sensitivity of prompt injection detection</p>
            </div>
            <Select value={injectionLevel} onValueChange={(v) => setInjectionLevel(v as typeof injectionLevel)}>
              <SelectTrigger className="w-[120px] h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="off">Off</SelectItem>
                <SelectItem value="low">Low</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="high">High</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      {/* Rate limits & cost */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Rate Limits & Cost Control</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-sm text-[var(--color-secondary)]">Daily cost cap</p>
              <div className="flex items-center gap-2">
                <span className="text-xs text-[var(--color-muted)]">$</span>
                <Input
                  type="number"
                  min="0"
                  step="0.5"
                  value={dailyCostCap}
                  onChange={(e) => setDailyCostCap(e.target.value)}
                  className="w-24 h-7 text-xs font-mono"
                  placeholder="10.00"
                />
              </div>
            </div>
            <div className="space-y-1">
              <div className="flex justify-between text-[10px] text-[var(--color-muted)]">
                <span>Today's spend: ${todaySpend.toFixed(2)}</span>
                <span>Cap: ${capValue.toFixed(2)}</span>
              </div>
              <Progress value={spendPercent} className="h-1.5" />
            </div>
          </div>
        </div>
      </section>

      <Separator />

      {/* US-10: Doctor diagnostics panel */}
      <DiagnosticsSection />
    </div>
  )
}
