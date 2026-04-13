import { useEffect, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FloppyDisk, ShieldCheck, ShieldWarning, Prohibit } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { MCPServerPicker } from './MCPServerPicker'
import {
  fetchBuiltinTools,
  fetchAgentTools,
  fetchMcpServersForAgent,
  updateAgentTools,
  type AgentToolsCfg,
  type BuiltinTool,
} from '@/lib/api'
import { useUiStore } from '@/store/ui'

type ToolPolicy = 'allow' | 'ask' | 'deny'

interface ToolsAndPermissionsProps {
  agentId: string | null
  agentType: 'core' | 'custom'
  tools: AgentToolsCfg
  onChange: (tools: AgentToolsCfg) => void
}

// Presets for quick policy configuration
const POLICY_PRESETS: Record<string, { label: string; description: string; defaultPolicy: ToolPolicy; overrides?: Record<string, ToolPolicy> }> = {
  unrestricted: {
    label: 'Unrestricted',
    description: 'All tools allowed without confirmation',
    defaultPolicy: 'allow',
  },
  cautious: {
    label: 'Cautious',
    description: 'All tools require approval before use',
    defaultPolicy: 'ask',
  },
  standard: {
    label: 'Standard',
    description: 'Safe tools allowed, exec and browser require approval',
    defaultPolicy: 'allow',
    overrides: {
      exec: 'ask',
      'browser.navigate': 'ask',
      'browser.click': 'ask',
      'browser.type': 'ask',
      'browser.evaluate': 'deny',
    },
  },
  minimal: {
    label: 'Minimal',
    description: 'Only reading and search allowed, everything else denied',
    defaultPolicy: 'deny',
    overrides: {
      read_file: 'allow',
      list_dir: 'allow',
      web_search: 'allow',
      web_fetch: 'allow',
      task_list: 'allow',
      agent_list: 'allow',
    },
  },
}

function PolicyBadge({ policy, onClick, active }: { policy: ToolPolicy; onClick: () => void; active: boolean }) {
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
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-[10px] font-medium border transition-colors ${
        active ? cfg.activeColor : `border-transparent ${cfg.color} hover:bg-[var(--color-surface-2)]`
      }`}
    >
      <Icon size={11} weight="bold" />
      {cfg.label}
    </button>
  )
}

function resolvePolicy(toolName: string, policies: Record<string, ToolPolicy> | undefined, defaultPolicy: ToolPolicy): ToolPolicy {
  return policies?.[toolName] ?? defaultPolicy
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
  system: 'System',
}

export function ToolsAndPermissions({ agentId, agentType: _agentType, tools, onChange }: ToolsAndPermissionsProps) {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const { data: builtinTools = [], isLoading: toolsLoading, isError: toolsError } = useQuery({
    queryKey: ['tools-builtin'],
    queryFn: fetchBuiltinTools,
  })

  const { data: mcpServers = [], isError: mcpError } = useQuery({
    queryKey: ['mcp-servers'],
    queryFn: fetchMcpServersForAgent,
  })

  const { data: agentToolsData } = useQuery({
    queryKey: ['agent-tools', agentId],
    queryFn: () => fetchAgentTools(agentId!),
    enabled: !!agentId,
  })

  useEffect(() => {
    if (agentToolsData && agentId) {
      onChange(agentToolsData.config)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentToolsData, agentId])

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: (cfg: AgentToolsCfg) => updateAgentTools(agentId!, cfg),
    onSuccess: (result) => {
      onChange(result.config)
      queryClient.invalidateQueries({ queryKey: ['agent-tools', agentId] })
      addToast({ message: 'Tool policies saved', variant: 'success' })
    },
    onError: (err: Error) => {
      addToast({ message: `Failed to save: ${err.message}`, variant: 'error' })
    },
  })

  const defaultPolicy: ToolPolicy = (tools.builtin?.default_policy as ToolPolicy) || 'allow'
  const policies: Record<string, ToolPolicy> = (tools.builtin?.policies as Record<string, ToolPolicy>) || {}

  function setDefaultPolicy(p: ToolPolicy) {
    onChange({
      ...tools,
      builtin: { ...tools.builtin, default_policy: p, policies },
    })
  }

  function setToolPolicy(toolName: string, p: ToolPolicy) {
    const newPolicies = { ...policies }
    if (p === defaultPolicy) {
      delete newPolicies[toolName] // matches default, no override needed
    } else {
      newPolicies[toolName] = p
    }
    onChange({
      ...tools,
      builtin: { ...tools.builtin, default_policy: defaultPolicy, policies: newPolicies },
    })
  }

  function applyPreset(key: string) {
    const preset = POLICY_PRESETS[key]
    if (!preset) return
    onChange({
      ...tools,
      builtin: {
        default_policy: preset.defaultPolicy,
        policies: preset.overrides ? { ...preset.overrides } : {},
      },
    })
  }

  // Detect which preset matches current config
  const activePreset = useMemo(() => {
    for (const [key, preset] of Object.entries(POLICY_PRESETS)) {
      if (preset.defaultPolicy !== defaultPolicy) continue
      const overrideCount = Object.keys(preset.overrides || {}).length
      const currentOverrideCount = Object.keys(policies).length
      if (overrideCount === 0 && currentOverrideCount === 0) return key
      if (overrideCount === currentOverrideCount) {
        const matches = Object.entries(preset.overrides || {}).every(([k, v]) => policies[k] === v)
        if (matches) return key
      }
    }
    return 'custom'
  }, [defaultPolicy, policies])

  // Filter out system tools for non-system agents
  const displayTools = builtinTools.filter((t) => {
    if (t.scope === 'system') return false // system tools not shown in agent config
    return true
  })

  const grouped = groupByCategory(displayTools)

  if (toolsLoading) {
    return (
      <div className="space-y-2 py-4">
        {[1, 2, 3].map((i) => (
          <div key={i} className="h-9 rounded-md bg-[var(--color-surface-2)] animate-pulse" />
        ))}
      </div>
    )
  }

  if (toolsError) {
    return (
      <p className="text-xs text-[var(--color-error)] py-4">
        Failed to load tool list. Check that the backend is running.
      </p>
    )
  }

  return (
    <div className="space-y-5">
      {/* Preset row */}
      <div className="space-y-2">
        <p className="text-xs text-[var(--color-muted)]">Policy presets</p>
        <div className="flex gap-2 flex-wrap">
          {Object.entries(POLICY_PRESETS).map(([key, preset]) => (
            <button
              key={key}
              type="button"
              onClick={() => applyPreset(key)}
              className={`px-3 py-1.5 rounded-md text-[11px] font-medium border transition-colors ${
                activePreset === key
                  ? 'bg-[var(--color-accent)]/20 text-[var(--color-accent)] border-[var(--color-accent)]/40'
                  : 'border-[var(--color-border)] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)]'
              }`}
              title={preset.description}
            >
              {preset.label}
            </button>
          ))}
        </div>
      </div>

      {/* Default policy */}
      <div className="space-y-1.5">
        <p className="text-xs text-[var(--color-muted)]">Default policy for unlisted tools</p>
        <div className="flex gap-1.5">
          {(['allow', 'ask', 'deny'] as ToolPolicy[]).map((p) => (
            <PolicyBadge key={p} policy={p} active={defaultPolicy === p} onClick={() => setDefaultPolicy(p)} />
          ))}
        </div>
      </div>

      {/* Per-tool policies grouped by category */}
      <div className="space-y-3">
        <p className="text-xs text-[var(--color-muted)]">
          Per-tool policies ({displayTools.length} tools)
          {mcpError && (
            <span className="text-[var(--color-warning)] ml-2">(MCP servers unavailable)</span>
          )}
        </p>
        {Object.entries(grouped).map(([category, catTools]) => (
          <div key={category} className="space-y-1">
            <p className="text-[10px] font-semibold text-[var(--color-secondary)] uppercase tracking-wider">
              {CATEGORY_LABELS[category] || category}
            </p>
            <div className="space-y-0.5">
              {catTools.map((tool) => {
                const currentPolicy = resolvePolicy(tool.name, policies, defaultPolicy)
                const isOverridden = tool.name in policies
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
                          active={currentPolicy === p}
                          onClick={() => setToolPolicy(tool.name, p)}
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

      {/* MCP section */}
      <div className="space-y-2">
        <p className="text-xs font-medium text-[var(--color-secondary)]">MCP Servers</p>
        <MCPServerPicker
          servers={mcpServers}
          mcpConfig={tools.mcp}
          onChange={(mcp) => onChange({ ...tools, mcp })}
        />
      </div>

      {/* Save */}
      {agentId && (
        <div className="pt-2 flex items-center gap-3">
          <Button
            size="sm"
            onClick={() => doSave(tools)}
            disabled={isSaving}
            className="gap-2"
          >
            <FloppyDisk size={13} weight="bold" />
            {isSaving ? 'Saving...' : 'Save Tool Policies'}
          </Button>
          <span className="text-[10px] text-[var(--color-muted)]">
            {Object.keys(policies).length} override{Object.keys(policies).length !== 1 ? 's' : ''} | Default: {defaultPolicy}
          </span>
        </div>
      )}
    </div>
  )
}
