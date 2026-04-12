import { useEffect, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { ToolPickerPreset } from './ToolPickerPreset'
import { ToolGroupList } from './ToolGroupList'
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
import { TOOL_PRESETS, type PresetKey } from '@/lib/agentToolPresets'

interface ToolsAndPermissionsProps {
  agentId: string | null
  agentType: 'system' | 'core' | 'custom'
  tools: AgentToolsCfg
  onChange: (tools: AgentToolsCfg) => void
}

function detectPreset(visible: string[] | undefined, allTools: BuiltinTool[]): PresetKey {
  if (!visible) return 'custom'
  const sorted = [...visible].sort().join(',')

  // Check "unrestricted" first — its tool set is derived from allTools at runtime.
  const unrestrictedNames = allTools
    .filter((t) => t.scope !== 'system')
    .map((t) => t.name)
    .sort()
    .join(',')
  if (sorted === unrestrictedNames) return 'unrestricted'

  // Check named presets (skip unrestricted and custom which have no static list).
  const namedPresets = (Object.keys(TOOL_PRESETS) as PresetKey[]).filter(
    (k) => k !== 'unrestricted' && k !== 'custom',
  )
  for (const key of namedPresets) {
    const presetSorted = [...(TOOL_PRESETS[key].tools as string[])].sort().join(',')
    if (sorted === presetSorted) return key
  }
  return 'custom'
}

export function ToolsAndPermissions({ agentId, agentType, tools, onChange }: ToolsAndPermissionsProps) {
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

  // When editing an existing agent, load its tool config from the server
  const { data: agentToolsData } = useQuery({
    queryKey: ['agent-tools', agentId],
    queryFn: () => fetchAgentTools(agentId!),
    enabled: !!agentId,
  })

  // Sync server-loaded config into parent state on first load
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
      addToast({ message: 'Tool permissions saved', variant: 'success' })
    },
    onError: (err: Error) => {
      addToast({ message: `Failed to save tools: ${err.message}`, variant: 'error' })
    },
  })

  const visibleTools = tools.builtin.visible ?? []

  const activePreset = useMemo(
    () => detectPreset(visibleTools, builtinTools),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [visibleTools.join(','), builtinTools]
  )

  function applyPreset(key: PresetKey) {
    const preset = TOOL_PRESETS[key]
    if (preset.tools === 'all') {
      const allVisible = builtinTools
        .filter((t) => t.scope !== 'system')
        .map((t) => t.name)
      onChange({ ...tools, builtin: { mode: 'explicit', visible: allVisible } })
    } else if (key === 'custom') {
      onChange({ ...tools, builtin: { mode: 'explicit', visible: visibleTools } })
    } else {
      onChange({
        ...tools,
        builtin: { mode: 'explicit', visible: [...(preset.tools as string[])] },
      })
    }
  }

  function toggleTool(toolName: string) {
    const current = tools.builtin.visible ?? []
    const next = current.includes(toolName)
      ? current.filter((n) => n !== toolName)
      : [...current, toolName]
    onChange({ ...tools, builtin: { mode: 'explicit', visible: next } })
  }

  if (toolsLoading) {
    return (
      <div className="space-y-2 py-4">
        {[1, 2, 3].map((i) => (
          <div
            key={i}
            className="h-9 rounded-md bg-[var(--color-surface-2)] animate-pulse"
          />
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
        <p className="text-xs text-[var(--color-muted)]">Quick-select a preset</p>
        <ToolPickerPreset activePreset={activePreset} onSelect={applyPreset} />
      </div>

      {/* Tool list */}
      <div className="space-y-1.5">
        <p className="text-xs text-[var(--color-muted)]">
          Tools available to this agent
          {mcpError && (
            <span className="text-[var(--color-warning)] ml-2">(MCP servers unavailable)</span>
          )}
        </p>
        <ToolGroupList
          tools={builtinTools}
          selected={visibleTools}
          agentType={agentType}
          onToggle={toggleTool}
        />
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

      {/* Save — only shown when editing an existing agent */}
      {agentId && (
        <div className="pt-2 flex items-center gap-3">
          <Button
            size="sm"
            onClick={() => doSave(tools)}
            disabled={isSaving}
            className="gap-2"
          >
            <FloppyDisk size={13} weight="bold" />
            {isSaving ? 'Saving...' : 'Save Tool Permissions'}
          </Button>
          <span className="text-[10px] text-[var(--color-muted)]">
            Selected: {visibleTools.length} tool{visibleTools.length !== 1 ? 's' : ''}
          </span>
        </div>
      )}
    </div>
  )
}
