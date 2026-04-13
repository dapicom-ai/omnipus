import { useState, useEffect, useRef, useMemo } from 'react'
import { AuditLogViewer } from './AuditLogViewer'
import { ExecAllowlistSection } from './ExecAllowlistSection'
import { PromptGuardSection } from './PromptGuardSection'
import { ExecProxyStatusCard } from './ExecProxyStatusCard'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash, Key, ShieldCheck, ShieldWarning, Prohibit } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAutoSave } from '@/hooks/useAutoSave'
import { AutoSaveIndicator } from '@/components/ui/AutoSaveIndicator'
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
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import {
  fetchConfig,
  updateConfig,
  fetchGatewayStatus,
  fetchCredentials,
  addCredential,
  deleteCredential,
  fetchBuiltinTools,
  fetchGlobalToolPolicies,
  updateGlobalToolPolicies,
  type BuiltinTool,
} from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { DiagnosticsSection } from './DiagnosticsSection'
import { SandboxSection } from './SandboxSection'

// ── Tool Access — Global Policies ─────────────────────────────────────────────

type ToolPolicy = 'allow' | 'ask' | 'deny'

const CATEGORY_LABELS: Record<string, string> = {
  file: 'File & Code',
  code: 'Code Execution',
  web: 'Web & Search',
  browser: 'Browser Automation',
  communication: 'Communication',
  task: 'Task Management',
  automation: 'Automation',
  search: 'Search & Discovery',
  skills: 'Skills',
  hardware: 'Hardware (IoT)',
}

function PolicyBadge({
  policy,
  onClick,
  active,
  disabled,
}: {
  policy: ToolPolicy
  onClick: () => void
  active: boolean
  disabled?: boolean
}) {
  const configs: Record<ToolPolicy, { icon: typeof ShieldCheck; label: string; color: string; activeColor: string }> = {
    allow: { icon: ShieldCheck, label: 'Allow', color: 'text-[var(--color-muted)]', activeColor: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/40' },
    ask: { icon: ShieldWarning, label: 'Ask', color: 'text-[var(--color-muted)]', activeColor: 'bg-amber-500/20 text-amber-400 border-amber-500/40' },
    deny: { icon: Prohibit, label: 'Deny', color: 'text-[var(--color-muted)]', activeColor: 'bg-red-500/20 text-red-400 border-red-500/40' },
  }
  const cfg = configs[policy]
  const Icon = cfg.icon
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium border transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
        active ? cfg.activeColor : `border-transparent ${cfg.color} hover:bg-[var(--color-surface-2)]`
      }`}
    >
      <Icon size={11} weight="bold" />
      {cfg.label}
    </button>
  )
}

function groupByCategory(tools: BuiltinTool[]): Record<string, BuiltinTool[]> {
  const groups: Record<string, BuiltinTool[]> = {}
  for (const t of tools) {
    const cat = t.category || 'other'
    if (!groups[cat]) groups[cat] = []
    groups[cat].push(t)
  }
  return groups
}

function GlobalToolPoliciesSection() {
  const queryClient = useQueryClient()

  const { data: builtinTools = [], isLoading: toolsLoading, isError: toolsError } = useQuery({
    queryKey: ['tools-builtin'],
    queryFn: fetchBuiltinTools,
  })

  const { data: globalPolicies, isLoading: policiesLoading, isError: policiesError } = useQuery({
    queryKey: ['global-tool-policies'],
    queryFn: fetchGlobalToolPolicies,
  })

  // Local draft state — initialised from server data
  const [defaultPolicy, setDefaultPolicy] = useState<ToolPolicy>('ask')
  const [perToolPolicies, setPerToolPolicies] = useState<Record<string, ToolPolicy>>({})
  const [isDraftReady, setIsDraftReady] = useState(false)

  useEffect(() => {
    if (!globalPolicies || isDraftReady) return
    setDefaultPolicy(globalPolicies.default_policy)
    setPerToolPolicies(globalPolicies.policies ?? {})
    setIsDraftReady(true)
  }, [globalPolicies, isDraftReady])

  const policiesData = useMemo(
    () => ({ default_policy: defaultPolicy, policies: perToolPolicies }),
    [defaultPolicy, perToolPolicies],
  )

  const { status: saveStatus, error: saveError } = useAutoSave(
    policiesData,
    async (cfg) => {
      await updateGlobalToolPolicies(cfg)
      queryClient.invalidateQueries({ queryKey: ['global-tool-policies'] })
    },
    { disabled: !isDraftReady },
  )

  function handleSetDefaultPolicy(p: ToolPolicy) {
    setDefaultPolicy(p)
  }

  function handleSetToolPolicy(toolName: string, p: ToolPolicy) {
    setPerToolPolicies((prev) => {
      const next = { ...prev }
      if (p === defaultPolicy) {
        delete next[toolName]
      } else {
        next[toolName] = p
      }
      return next
    })
  }

  const displayTools = builtinTools.filter((t) => t.scope !== 'system')
  const grouped = groupByCategory(displayTools)

  const isLoading = toolsLoading || policiesLoading

  if (isLoading) {
    return (
      <div className="space-y-2 py-4">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-8 rounded-md bg-[var(--color-surface-2)] animate-pulse" />
        ))}
      </div>
    )
  }

  if (toolsError || policiesError) {
    return (
      <p className="text-xs text-red-400 py-4">
        Failed to load tool policies. Check that the backend is running.
      </p>
    )
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-[var(--color-muted)]">
        These policies apply globally across all agents. Per-agent policies shown in the Agent Profile cannot
        override a global "Deny". Tools blocked here are greyed out in each agent's tool list.
      </p>

      {/* Default policy */}
      <div className="space-y-1.5">
        <p className="text-xs text-[var(--color-muted)]">Default policy for unlisted tools</p>
        <div className="flex gap-1.5">
          {(['allow', 'ask', 'deny'] as ToolPolicy[]).map((p) => (
            <PolicyBadge
              key={p}
              policy={p}
              active={defaultPolicy === p}
              onClick={() => handleSetDefaultPolicy(p)}
            />
          ))}
        </div>
      </div>

      {/* Per-tool policies grouped by category */}
      <div className="space-y-3">
        <p className="text-xs text-[var(--color-muted)]">Per-tool policies ({displayTools.length} tools)</p>
        {Object.entries(grouped).map(([category, catTools]) => (
          <div key={category} className="space-y-1">
            <p className="text-[10px] font-semibold text-[var(--color-secondary)] uppercase tracking-wider">
              {CATEGORY_LABELS[category] || category}
            </p>
            <div className="space-y-0.5">
              {catTools.map((tool) => {
                const resolved: ToolPolicy = perToolPolicies[tool.name] ?? defaultPolicy
                const isOverridden = tool.name in perToolPolicies
                return (
                  <div
                    key={tool.name}
                    className="flex items-center justify-between py-1 px-2 rounded hover:bg-[var(--color-surface-2)] transition-colors"
                  >
                    <div className="flex-1 min-w-0">
                      <span className={`text-xs font-mono ${isOverridden ? 'text-[var(--color-secondary)]' : 'text-[var(--color-muted)]'}`}>
                        {tool.name}
                      </span>
                      <span className="text-[10px] text-[var(--color-muted)] ml-2 hidden sm:inline">
                        {tool.description?.slice(0, 50)}{(tool.description?.length ?? 0) > 50 ? '...' : ''}
                      </span>
                    </div>
                    <div className="flex gap-0.5 shrink-0">
                      {(['allow', 'ask', 'deny'] as ToolPolicy[]).map((p) => (
                        <PolicyBadge
                          key={p}
                          policy={p}
                          active={resolved === p}
                          onClick={() => handleSetToolPolicy(tool.name, p)}
                        />
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        ))}
      </div>

      <div className="pt-2 flex items-center gap-3">
        <AutoSaveIndicator status={saveStatus} error={saveError} />
        <span className="text-[10px] text-[var(--color-muted)]">
          {Object.keys(perToolPolicies).length} override{Object.keys(perToolPolicies).length !== 1 ? 's' : ''} | Default: {defaultPolicy}
        </span>
      </div>
    </div>
  )
}

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
    setDailyCostCap(config.security.daily_cost_cap?.toString() ?? '')
    setAgentLlmCallsPerHour(config.security.rate_limits.max_agent_llm_calls_per_hour?.toString() ?? '')
    setAgentToolCallsPerMin(config.security.rate_limits.max_agent_tool_calls_per_minute?.toString() ?? '')
    setExecTimeoutSecs(config.security.exec_timeout_seconds?.toString() ?? '')
    setMaxBackgroundSecs(config.security.max_background_seconds?.toString() ?? '')
    setEnableDenyPatterns(config.security.enable_deny_patterns ?? false)
  }, [config])

  const securityFormData = useMemo(() => ({
    policy_mode: policyMode,
    exec_approval: execApproval,
    daily_cost_cap: dailyCostCap,
    exec_timeout_seconds: execTimeoutSecs,
    max_background_seconds: maxBackgroundSecs,
    enable_deny_patterns: enableDenyPatterns,
    agent_llm_calls_per_hour: agentLlmCallsPerHour,
    agent_tool_calls_per_min: agentToolCallsPerMin,
  }), [policyMode, execApproval, dailyCostCap, execTimeoutSecs, maxBackgroundSecs, enableDenyPatterns, agentLlmCallsPerHour, agentToolCallsPerMin])

  const { status: saveStatus, error: saveError } = useAutoSave(
    securityFormData,
    async () => {
      await updateConfig({
        security: {
          policy_mode: policyMode,
          exec_approval: execApproval,
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
      })
      isDirtyRef.current = false
      queryClient.invalidateQueries({ queryKey: ['config'] })
    },
    { disabled: !config },
  )

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
        <AutoSaveIndicator status={saveStatus} error={saveError} />
      </div>

      <Accordion
        type="multiple"
        defaultValue={['diagnostics']}
        className="rounded-lg border border-[var(--color-border)] divide-y divide-[var(--color-border)] overflow-hidden"
      >
        {/* Diagnostics — open by default */}
        <AccordionItem value="diagnostics" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Diagnostics
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
              <DiagnosticsSection />
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Tool Access — Global Policies */}
        <AccordionItem value="tool-access" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Tool Access — Global Policies
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
              <GlobalToolPoliciesSection />
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Command Execution & Allowlist — merged section */}
        <AccordionItem value="command-execution" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Command Execution & Allowlist
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
              <p className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider mt-4">
                Timeouts & Patterns
              </p>
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

              <p className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider mt-4">
                Binary Allowlist
              </p>
              <ExecAllowlistSection />

              <p className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider mt-4">
                SSRF Proxy
              </p>
              <ExecProxyStatusCard />
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Prompt Guard */}
        <AccordionItem value="prompt-guard" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Prompt Injection Defense
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
              <PromptGuardSection />
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Process Sandbox */}
        <AccordionItem value="sandbox" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Process Sandbox
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
              <SandboxSection />
              <p className="text-xs text-[var(--color-muted)] pt-1">
                These settings can be changed in config.json. A restart is required for changes to take effect.
              </p>
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Policy */}
        <AccordionItem value="policy" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Policy
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
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
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Rate Limits & Cost Control */}
        <AccordionItem value="rate-limits" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            Rate Limits & Cost Control
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
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
                          ? "Today's spend: unavailable"
                          : `Today's spend: $${todaySpend.toFixed(2)}`}
                      </span>
                      <span>Cap: ${capValue.toFixed(2)}</span>
                    </div>
                    <Progress value={spendPercent} className="h-1.5" />
                  </div>
                </div>

                <Separator />

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
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Credential Vault */}
        <AccordionItem value="credential-vault" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            <span className="flex-1 text-left">Credential Vault</span>
            <Button
              size="sm"
              variant="outline"
              className="h-7 px-2 gap-1 text-xs mr-2"
              onClick={(e) => { e.stopPropagation(); setCredModalOpen(true) }}
            >
              <Plus size={11} weight="bold" />
              Add key
            </Button>
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 space-y-3">
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
            </div>
          </AccordionContent>
        </AccordionItem>

        {/* Audit Log */}
        <AccordionItem value="audit-log" className="border-0">
          <AccordionTrigger className="px-4 font-headline font-bold text-sm">
            <span className="flex-1 text-left">Audit Log</span>
            <Button
              variant="outline"
              size="sm"
              className="h-7 px-2 text-xs mr-2"
              onClick={(e) => { e.stopPropagation(); setAuditLogOpen(true) }}
            >
              View Log
            </Button>
          </AccordionTrigger>
          <AccordionContent>
            <div className="px-4 pb-2">
              <p className="text-xs text-[var(--color-muted)]">
                Security events, policy decisions, and tool executions. Use the button above to open the full viewer.
              </p>
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>

      <AuditLogViewer open={auditLogOpen} onOpenChange={setAuditLogOpen} />

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
