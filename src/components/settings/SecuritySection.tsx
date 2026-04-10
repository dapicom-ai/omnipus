import { useState, useEffect, useRef } from 'react'
import { AuditLogViewer } from './AuditLogViewer'
import { ExecAllowlistSection } from './ExecAllowlistSection'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FloppyDisk, Plus, Trash, Key } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { SmartSelect } from '@/components/ui/smart-select'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import { fetchConfig, updateConfig, fetchGatewayStatus, fetchCredentials, addCredential, deleteCredential } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { DiagnosticsSection } from './DiagnosticsSection'

export function SecuritySection() {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data: config, isLoading, isError: configError } = useQuery({
    queryKey: ['config'],
    queryFn: fetchConfig,
  })

  const { data: gatewayStatus, isError: gatewayStatusError } = useQuery({
    queryKey: ['gateway-status'],
    queryFn: fetchGatewayStatus,
  })

  const { data: credentials = [], isError: credentialsError } = useQuery({
    queryKey: ['credentials'],
    queryFn: fetchCredentials,
    retry: false,
  })

  const isDirtyRef = useRef(false)
  const markDirty = () => { isDirtyRef.current = true }

  const [policyMode, setPolicyMode] = useState<'allow' | 'deny'>('deny')
  const [execApproval, setExecApproval] = useState<'auto' | 'ask' | 'deny'>('ask')
  const [injectionLevel, setInjectionLevel] = useState<'off' | 'low' | 'medium' | 'high'>('medium')
  const [dailyCostCap, setDailyCostCap] = useState('')
  const [agentLlmCallsPerHour, setAgentLlmCallsPerHour] = useState('')
  const [agentToolCallsPerMin, setAgentToolCallsPerMin] = useState('')
  const [execTimeoutSecs, setExecTimeoutSecs] = useState('')
  const [maxBackgroundSecs, setMaxBackgroundSecs] = useState('')
  const [enableDenyPatterns, setEnableDenyPatterns] = useState(false)

  // Audit log dialog state
  const [auditLogOpen, setAuditLogOpen] = useState(false)

  // Credential vault modal state
  const [credModalOpen, setCredModalOpen] = useState(false)
  const [credKey, setCredKey] = useState('')
  const [credValue, setCredValue] = useState('')
  const [deletingKey, setDeletingKey] = useState<string | null>(null)

  useEffect(() => {
    if (!config) return
    if (isDirtyRef.current) return
    setPolicyMode(config.security.policy_mode)
    setExecApproval(config.security.exec_approval)
    setInjectionLevel(config.security.prompt_injection_level)
    setDailyCostCap(config.security.daily_cost_cap?.toString() ?? '')
    setAgentLlmCallsPerHour(config.security.rate_limits.max_agent_llm_calls_per_hour?.toString() ?? '')
    setAgentToolCallsPerMin(config.security.rate_limits.max_agent_tool_calls_per_minute?.toString() ?? '')
    setExecTimeoutSecs(config.security.exec_timeout_seconds?.toString() ?? '')
    setMaxBackgroundSecs(config.security.max_background_seconds?.toString() ?? '')
    setEnableDenyPatterns(config.security.enable_deny_patterns ?? false)
  }, [config])

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: () =>
      updateConfig({
        security: {
          policy_mode: policyMode,
          exec_approval: execApproval,
          prompt_injection_level: injectionLevel,
          daily_cost_cap: dailyCostCap ? parseFloat(dailyCostCap) : undefined,
          exec_timeout_seconds: execTimeoutSecs ? parseInt(execTimeoutSecs, 10) : undefined,
          max_background_seconds: maxBackgroundSecs ? parseInt(maxBackgroundSecs, 10) : undefined,
          enable_deny_patterns: enableDenyPatterns,
          rate_limits: {
            ...config?.security.rate_limits,
            max_agent_llm_calls_per_hour: agentLlmCallsPerHour ? parseInt(agentLlmCallsPerHour, 10) : undefined,
            max_agent_tool_calls_per_minute: agentToolCallsPerMin ? parseInt(agentToolCallsPerMin, 10) : undefined,
          },
        },
      }),
    onSuccess: () => {
      isDirtyRef.current = false
      queryClient.invalidateQueries({ queryKey: ['config'] })
      addToast({ message: 'Security settings saved', variant: 'success' })
    },
    onError: (err: Error) => addToast({
      message: err.message.includes('501')
        ? 'Settings changes require editing config.json and restarting the gateway'
        : err.message,
      variant: 'error',
    }),
  })

  const { mutate: doAddCred, isPending: isAddingCred } = useMutation({
    mutationFn: () => addCredential(credKey.trim(), credValue),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['credentials'] })
      addToast({ message: `Credential "${credKey}" saved`, variant: 'success' })
      setCredModalOpen(false)
      setCredKey('')
      setCredValue('')
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const { mutate: doDeleteCred } = useMutation({
    mutationFn: (key: string) => deleteCredential(key),
    onSuccess: (_data, key) => {
      queryClient.invalidateQueries({ queryKey: ['credentials'] })
      addToast({ message: `Credential "${key}" removed`, variant: 'success' })
      setDeletingKey(null)
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  if (isLoading) {
    return <div className="text-sm text-[var(--color-muted)]">Loading...</div>
  }

  if (configError) {
    return <p className="text-sm text-red-400">Failed to load security settings. Please try again.</p>
  }

  const todaySpend = gatewayStatus?.daily_cost ?? 0
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
                onCheckedChange={(v) => { markDirty(); setPolicyMode(v ? 'allow' : 'deny') }}
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
            <SmartSelect
              value={execApproval}
              onValueChange={(v) => { markDirty(); setExecApproval(v as typeof execApproval) }}
              triggerClassName="w-[120px] h-8 text-xs"
              items={[
                { value: 'auto', label: 'Auto-allow' },
                { value: 'ask', label: 'Ask each time' },
                { value: 'deny', label: 'Always deny' },
              ]}
            />
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
            <SmartSelect
              value={injectionLevel}
              onValueChange={(v) => { markDirty(); setInjectionLevel(v as typeof injectionLevel) }}
              triggerClassName="w-[120px] h-8 text-xs"
              items={[
                { value: 'off', label: 'Off' },
                { value: 'low', label: 'Low' },
                { value: 'medium', label: 'Medium' },
                { value: 'high', label: 'High' },
              ]}
            />
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
                  onChange={(e) => { markDirty(); setDailyCostCap(e.target.value) }}
                  className="w-24 h-7 text-xs font-mono"
                  placeholder="10.00"
                />
              </div>
            </div>
            <div className="space-y-1">
              <div className="flex justify-between text-[10px] text-[var(--color-muted)]">
                <span>
                  {gatewayStatusError
                    ? 'Today\'s spend: unavailable'
                    : `Today's spend: $${todaySpend.toFixed(2)}`}
                </span>
                <span>Cap: ${capValue.toFixed(2)}</span>
              </div>
              <Progress value={spendPercent} className="h-1.5" />
            </div>
          </div>

          <Separator />

          {/* Per-agent default rate limits */}
          <div className="space-y-3">
            <p className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Per-Agent Defaults</p>
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-[var(--color-secondary)]">LLM calls / hour</p>
                <p className="text-xs text-[var(--color-muted)]">Default limit per agent</p>
              </div>
              <Input
                type="number"
                min="0"
                value={agentLlmCallsPerHour}
                onChange={(e) => { markDirty(); setAgentLlmCallsPerHour(e.target.value) }}
                className="w-24 h-7 text-xs font-mono"
                placeholder="Unlimited"
              />
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-[var(--color-secondary)]">Tool calls / minute</p>
                <p className="text-xs text-[var(--color-muted)]">Default limit per agent</p>
              </div>
              <Input
                type="number"
                min="0"
                value={agentToolCallsPerMin}
                onChange={(e) => { markDirty(); setAgentToolCallsPerMin(e.target.value) }}
                className="w-24 h-7 text-xs font-mono"
                placeholder="Unlimited"
              />
            </div>
          </div>
        </div>
      </section>

      {/* Command Execution */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Command Execution</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Exec timeout (seconds)</p>
              <p className="text-xs text-[var(--color-muted)]">Max time for a single command, 0 = no limit</p>
            </div>
            <Input
              type="number"
              min="0"
              value={execTimeoutSecs}
              onChange={(e) => { markDirty(); setExecTimeoutSecs(e.target.value) }}
              className="w-24 h-7 text-xs font-mono"
              placeholder="0"
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Background timeout (seconds)</p>
              <p className="text-xs text-[var(--color-muted)]">Max time for background processes, 0 = no limit</p>
            </div>
            <Input
              type="number"
              min="0"
              value={maxBackgroundSecs}
              onChange={(e) => { markDirty(); setMaxBackgroundSecs(e.target.value) }}
              className="w-24 h-7 text-xs font-mono"
              placeholder="0"
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Enable deny patterns</p>
              <p className="text-xs text-[var(--color-muted)]">Block commands matching configured deny patterns</p>
            </div>
            <Switch
              checked={enableDenyPatterns}
              onCheckedChange={(v) => { markDirty(); setEnableDenyPatterns(v) }}
            />
          </div>
        </div>
      </section>

      {/* Credential Vault */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Credential Vault</h3>
          <Button size="sm" variant="outline" className="h-7 px-2 gap-1 text-xs" onClick={() => setCredModalOpen(true)}>
            <Plus size={11} weight="bold" />
            Add key
          </Button>
        </div>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] divide-y divide-[var(--color-border)]">
          {credentialsError && (
            <div className="p-4 text-sm text-red-400">Failed to load credentials. Please try again.</div>
          )}
          {!credentialsError && credentials.length === 0 && (
            <div className="p-4 text-sm text-[var(--color-muted)] flex items-center gap-2">
              <Key size={14} />
              No credentials stored. Add your first key above.
            </div>
          )}
          {credentials.map((cred) => (
            <div key={cred.key} className="flex items-center justify-between px-4 py-2.5">
              <div>
                <p className="text-sm font-mono text-[var(--color-secondary)]">{cred.key}</p>
                <p className="text-[10px] text-[var(--color-muted)] font-mono">••••••••••••</p>
              </div>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 text-[var(--color-muted)] hover:text-[var(--color-error)]"
                onClick={() => setDeletingKey(cred.key)}
              >
                <Trash size={13} />
              </Button>
            </div>
          ))}
        </div>
      </section>

      {/* ── Exec Binary Allowlist ──────────────────────────── */}
      <Separator className="my-6" />
      <ExecAllowlistSection />

      {/* ── Audit Log ─────────────────────────────────────── */}
      <Separator className="my-6" />
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--color-secondary)]">Audit Log</h3>
          <p className="text-xs text-[var(--color-muted)] mt-1">
            Security events, policy decisions, and tool executions
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setAuditLogOpen(true)}>
          View Audit Log
        </Button>
      </div>
      <AuditLogViewer open={auditLogOpen} onOpenChange={setAuditLogOpen} />

      <Separator />

      {/* US-10: Doctor diagnostics panel */}
      <DiagnosticsSection />

      {/* Add credential modal */}
      <Dialog open={credModalOpen} onOpenChange={setCredModalOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="font-headline text-base">Add Credential</DialogTitle>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1">
              <p className="text-xs text-[var(--color-muted)]">Key name</p>
              <Input
                value={credKey}
                onChange={(e) => setCredKey(e.target.value)}
                placeholder="e.g. OPENAI_API_KEY"
                className="h-8 text-xs font-mono"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <p className="text-xs text-[var(--color-muted)]">Value</p>
              <Input
                type="password"
                value={credValue}
                onChange={(e) => setCredValue(e.target.value)}
                placeholder="sk-..."
                className="h-8 text-xs font-mono"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setCredModalOpen(false)}>Cancel</Button>
            <Button
              size="sm"
              onClick={() => doAddCred()}
              disabled={!credKey.trim() || !credValue || isAddingCred}
            >
              {isAddingCred ? 'Saving...' : 'Save'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation modal */}
      <Dialog open={!!deletingKey} onOpenChange={() => setDeletingKey(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle className="font-headline text-base">Remove credential?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-[var(--color-muted)] py-2">
            This will permanently remove <span className="font-mono text-[var(--color-secondary)]">{deletingKey}</span> from the vault. This cannot be undone.
          </p>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setDeletingKey(null)}>Cancel</Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => deletingKey && doDeleteCred(deletingKey)}
            >
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
