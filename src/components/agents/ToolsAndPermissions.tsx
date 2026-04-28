import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Info } from '@phosphor-icons/react'

import { MCPServerPicker } from './MCPServerPicker'
import { PolicyBadge, type ToolPolicy } from '@/components/shared/PolicyBadge'
import { CATEGORY_LABELS, groupByCategory, resolvePolicy } from '@/lib/toolCategories'
import {
  fetchRegistryTools,
  fetchAgentTools,
  fetchMcpServersForAgent,
  updateAgentTools,
  fetchGlobalToolPolicies,
  type AgentToolsCfg,
  type AgentToolEntry,
} from '@/lib/api'
import { useAutoSave } from '@/hooks/useAutoSave'
import { AutoSaveIndicator } from '@/components/ui/AutoSaveIndicator'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

interface ToolsAndPermissionsProps {
  agentId: string | null
  agentType: 'core' | 'custom'
  tools: AgentToolsCfg
  onChange: (tools: AgentToolsCfg) => void
}

// Presets for quick policy configuration (FR-044).
// Apply is replace semantics — a confirmation dialog is shown before applying (FR-043).
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

// Build an index from tool name → AgentToolEntry for fence-badge lookups.
function buildAgentToolIndex(tools: AgentToolEntry[]): Map<string, AgentToolEntry> {
  const m = new Map<string, AgentToolEntry>()
  for (const t of tools) m.set(t.name, t)
  return m
}

export function ToolsAndPermissions({ agentId, agentType: _agentType, tools, onChange }: ToolsAndPermissionsProps) {
  const queryClient = useQueryClient()

  // FR-043: preset confirmation dialog state
  const [pendingPresetKey, setPendingPresetKey] = useState<string | null>(null)

  const { status: saveStatus, error: saveError } = useAutoSave(
    tools,
    (data) => updateAgentTools(agentId!, data).then((result) => {
      onChange(result.config)
      queryClient.invalidateQueries({ queryKey: ['agent-tools', agentId] })
    }),
    { disabled: !agentId },
  )

  // FR-027: GET /api/v1/tools — central registry snapshot with source discriminator.
  const { data: registryTools = [], isLoading: toolsLoading, isError: toolsError } = useQuery({
    queryKey: ['registry-tools'],
    queryFn: fetchRegistryTools,
  })

  const { data: globalPolicies, isError: globalPoliciesError } = useQuery({
    queryKey: ['global-tool-policies'],
    queryFn: fetchGlobalToolPolicies,
  })

  const { data: mcpServers = [], isError: mcpError } = useQuery({
    queryKey: ['mcp-servers'],
    queryFn: fetchMcpServersForAgent,
  })

  // FR-028, FR-086: GET /api/v1/agents/{id}/tools — per-agent effective policy view.
  const { data: agentToolsData } = useQuery({
    queryKey: ['agent-tools', agentId],
    queryFn: () => fetchAgentTools(agentId!),
    enabled: !!agentId,
  })

  // Index agent-tools entries for efficient fence-badge lookup (FR-086).
  const agentToolIndex = useMemo(
    () => buildAgentToolIndex(agentToolsData?.tools ?? []),
    [agentToolsData],
  )

  useEffect(() => {
    if (agentToolsData && agentId) {
      // Only propagate when the incoming config actually differs from what is
      // already in state — prevents a spurious auto-save on the initial load.
      const incoming = JSON.stringify(agentToolsData.config)
      const current = JSON.stringify(tools)
      if (incoming !== current) {
        onChange(agentToolsData.config)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentToolsData, agentId])

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

  // FR-043: show confirmation dialog before applying a preset (replace semantics).
  function requestPreset(key: string) {
    if (!POLICY_PRESETS[key]) return
    setPendingPresetKey(key)
  }

  function confirmPreset() {
    if (!pendingPresetKey) return
    const preset = POLICY_PRESETS[pendingPresetKey]
    if (!preset) return
    onChange({
      ...tools,
      builtin: {
        default_policy: preset.defaultPolicy,
        policies: preset.overrides ? { ...preset.overrides } : {},
      },
    })
    setPendingPresetKey(null)
  }

  function cancelPreset() {
    setPendingPresetKey(null)
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

  // Filter out system-scope tools — never shown in agent tool configuration
  const displayTools = registryTools.filter((t) => {
    if (t.scope === 'system') return false
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

  const pendingPreset = pendingPresetKey ? POLICY_PRESETS[pendingPresetKey] : null

  return (
    <div className="space-y-5">
      {/* FR-043: Preset confirmation dialog */}
      <Dialog open={pendingPresetKey !== null} onOpenChange={(open) => { if (!open) cancelPreset() }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Apply preset: {pendingPreset?.label}?</DialogTitle>
            <DialogDescription>
              This will replace your existing per-tool policies with the {pendingPreset?.label} preset.
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="text-xs bg-[var(--color-surface-2)] rounded px-3 py-2 border border-[var(--color-border)] text-[var(--color-muted)]">
            Note: admin-required tools on custom agents will run with{' '}
            <span className="font-mono text-[var(--color-accent)]">ask</span> regardless of the
            preset setting.
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={cancelPreset} size="sm">
              Cancel
            </Button>
            <Button variant="default" onClick={confirmPreset} size="sm">
              Apply preset
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Auto-save status — top of section for visibility */}
      <div className="flex items-center gap-3">
        <AutoSaveIndicator status={saveStatus} error={saveError} />
        <span className="text-[10px] text-[var(--color-muted)]">
          {Object.keys(policies).length} override{Object.keys(policies).length !== 1 ? 's' : ''} | Default: {defaultPolicy}
        </span>
      </div>

      {/* Preset row */}
      <div className="space-y-2">
        <p className="text-xs text-[var(--color-muted)]">Policy presets</p>
        <div className="flex gap-2 flex-wrap">
          {Object.entries(POLICY_PRESETS).map(([key, preset]) => (
            <button
              key={key}
              type="button"
              onClick={() => requestPreset(key)}
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
        {globalPolicies && (
          <p className="text-[10px] text-[var(--color-muted)]">
            Tools blocked by global security policies are shown greyed out
          </p>
        )}
        {globalPoliciesError && (
          <p className="text-[10px] text-[var(--color-warning)]">
            Could not load global security policies — restrictions may not be shown
          </p>
        )}
        {Object.entries(grouped).map(([category, catTools]) => (
          <div key={category} className="space-y-1">
            <p className="text-[10px] font-semibold text-[var(--color-secondary)] uppercase tracking-wider">
              {CATEGORY_LABELS[category] || category}
            </p>
            <div className="space-y-0.5">
              {catTools.map((tool) => {
                const currentPolicy = resolvePolicy(tool.name, policies, defaultPolicy)
                const isOverridden = tool.name in policies

                // FR-086: look up the per-agent effective-policy entry for fence display.
                const agentEntry = agentToolIndex.get(tool.name)
                const fenceApplied = agentEntry?.fence_applied ?? false

                // Determine effective global policy for this tool.
                // When the fetch failed, default to 'deny' (deny-by-default security posture).
                const globalDefaultPolicy = globalPoliciesError ? 'deny' : (globalPolicies?.default_policy ?? 'allow')
                const globalToolPolicy = globalPoliciesError
                  ? 'deny'
                  : (globalPolicies?.policies?.[tool.name] ?? globalDefaultPolicy)
                const isGloballyDenied = globalToolPolicy === 'deny'
                const isGloballyAsked = globalToolPolicy === 'ask'

                return (
                  <div
                    key={tool.name}
                    className={`flex items-center justify-between py-1 px-2 rounded transition-colors ${
                      isGloballyDenied
                        ? 'opacity-50 cursor-not-allowed'
                        : 'hover:bg-[var(--color-surface-2)]'
                    }`}
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className={`text-xs font-mono ${isOverridden && !isGloballyDenied ? 'text-[var(--color-secondary)]' : 'text-[var(--color-muted)]'}`}>
                          {tool.name}
                        </span>

                        {/* FR-027: source badge — distinguishes MCP tools from builtins */}
                        {tool.source === 'mcp' && (
                          <span className="text-[9px] font-medium uppercase tracking-wide bg-[var(--color-surface-2)] text-[var(--color-muted)] border border-[var(--color-border)] px-1.5 py-0.5 rounded">
                            MCP
                          </span>
                        )}

                        {/* FR-086: fence badge — when configured allow was downgraded to ask */}
                        {fenceApplied && (
                          <span
                            className="text-[9px] font-medium uppercase tracking-wide bg-amber-500/10 text-amber-400 border border-amber-500/30 px-1.5 py-0.5 rounded flex items-center gap-0.5"
                            title="downgraded to ask: admin-required tool on custom agent"
                          >
                            <Info size={9} weight="bold" />
                            downgraded to ask
                          </span>
                        )}
                      </div>

                      <div>
                        {isGloballyDenied ? (
                          <span className="text-[10px] text-red-400 ml-0">Blocked by security policy</span>
                        ) : isGloballyAsked ? (
                          <span className="text-[10px] text-amber-400 ml-0" title="Global policy: Ask — agent cannot override to Allow">
                            Global: Ask
                          </span>
                        ) : fenceApplied ? (
                          <span className="text-[10px] text-amber-400 ml-0">
                            Configured: {agentEntry?.configured_policy} → Effective: {agentEntry?.effective_policy}
                          </span>
                        ) : (
                          <span className="text-[10px] text-[var(--color-muted)] hidden sm:inline">
                            {tool.description?.slice(0, 50)}{(tool.description?.length ?? 0) > 50 ? '...' : ''}
                          </span>
                        )}
                      </div>
                    </div>
                    <div className="flex gap-0.5 shrink-0">
                      {(['allow', 'ask', 'deny'] as ToolPolicy[]).map((p) => (
                        <PolicyBadge
                          key={p}
                          policy={p}
                          active={currentPolicy === p}
                          onClick={() => setToolPolicy(tool.name, p)}
                          // When globally denied: all buttons disabled
                          // When globally asked: Allow button disabled (can't upgrade past Ask)
                          disabled={isGloballyDenied || (isGloballyAsked && p === 'allow')}
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

    </div>
  )
}
