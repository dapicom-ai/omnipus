// Unit tests for ToolsAndPermissions — FR-027, FR-029, FR-043, FR-044, FR-086
//
// Tests:
//  1. Renders tools from GET /api/v1/tools (new endpoint, not /tools/builtin)
//  2. Displays source badge for MCP tools
//  3. Displays fence badge when fence_applied=true
//  4. Shows configured_policy vs effective_policy when fence is applied
//  5. Preset confirmation dialog appears on preset click (FR-043)
//  6. Confirming preset replaces policy map (replace, not merge) (FR-043)
//  7. Cancelling preset leaves policy map unchanged (FR-043)

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// Mock the API module
vi.mock('@/lib/api', () => ({
  fetchRegistryTools: vi.fn(),
  fetchBuiltinTools: vi.fn(),
  fetchAgentTools: vi.fn(),
  fetchMcpServersForAgent: vi.fn(),
  updateAgentTools: vi.fn(),
  fetchGlobalToolPolicies: vi.fn(),
}))

// Mock useAutoSave to prevent debounce side effects in tests
vi.mock('@/hooks/useAutoSave', () => ({
  useAutoSave: () => ({ status: 'idle', error: null }),
}))

import * as api from '@/lib/api'
import type { RegistryTool, AgentToolEntry, AgentToolsCfg } from '@/lib/api'
import { ToolsAndPermissions } from './ToolsAndPermissions'

// MCPServerPicker depends on server data — mock it to simplify tests
vi.mock('./MCPServerPicker', () => ({
  MCPServerPicker: () => null,
}))

// AutoSaveIndicator — mock to simplify
vi.mock('@/components/ui/AutoSaveIndicator', () => ({
  AutoSaveIndicator: () => null,
}))

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: 0 },
      mutations: { retry: false },
    },
  })
}

function renderWithQuery(ui: React.ReactElement) {
  const qc = makeQueryClient()
  return render(
    <QueryClientProvider client={qc}>
      {ui}
    </QueryClientProvider>
  )
}

const BUILTIN_TOOL: RegistryTool = {
  name: 'read_file',
  scope: 'general',
  category: 'filesystem',
  description: 'Read file contents',
  source: 'builtin',
}

const MCP_TOOL: RegistryTool = {
  name: 'mcp_search',
  scope: 'general',
  category: 'web',
  description: 'Search via MCP',
  source: 'mcp',
}

const ADMIN_TOOL: RegistryTool = {
  name: 'system.config.set',
  scope: 'core',
  category: 'system',
  description: 'Set system configuration',
  source: 'builtin',
}

const DEFAULT_TOOLS_CFG: AgentToolsCfg = {
  builtin: {
    default_policy: 'allow',
    policies: {},
  },
}

const NOOP_CHANGE = () => {}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.fetchRegistryTools).mockResolvedValue([BUILTIN_TOOL, MCP_TOOL])
  vi.mocked(api.fetchBuiltinTools).mockResolvedValue([BUILTIN_TOOL, MCP_TOOL])
  vi.mocked(api.fetchAgentTools).mockResolvedValue({
    config: DEFAULT_TOOLS_CFG,
    tools: [],
  })
  vi.mocked(api.fetchMcpServersForAgent).mockResolvedValue([])
  vi.mocked(api.fetchGlobalToolPolicies).mockResolvedValue({
    default_policy: 'allow',
    policies: {},
  })
})

describe('ToolsAndPermissions — new endpoint (FR-027, FR-029)', () => {
  it('calls fetchRegistryTools (GET /api/v1/tools), not /tools/builtin', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(api.fetchRegistryTools).toHaveBeenCalledTimes(1)
    })
  })

  it('renders builtin tool names from the registry', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('read_file')).toBeInTheDocument()
    })
  })
})

describe('ToolsAndPermissions — source badge (FR-027)', () => {
  it('shows MCP badge for mcp-sourced tools', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('mcp_search')).toBeInTheDocument()
      // MCP source badge
      expect(screen.getByText('MCP')).toBeInTheDocument()
    })
  })

  it('does not show MCP badge for builtin tools', async () => {
    // Only builtin tool returned
    vi.mocked(api.fetchRegistryTools).mockResolvedValue([BUILTIN_TOOL])
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('read_file')).toBeInTheDocument()
      expect(screen.queryByText('MCP')).not.toBeInTheDocument()
    })
  })
})

describe('ToolsAndPermissions — fence badge (FR-086)', () => {
  const FENCED_ENTRY: AgentToolEntry = {
    name: 'system.config.set',
    configured_policy: 'allow',
    effective_policy: 'ask',
    fence_applied: true,
    requires_admin_ask: true,
  }

  beforeEach(() => {
    // Registry includes the admin tool (scope must not be 'system' for display)
    vi.mocked(api.fetchRegistryTools).mockResolvedValue([
      { ...ADMIN_TOOL, scope: 'core' },
    ])
    vi.mocked(api.fetchAgentTools).mockResolvedValue({
      config: {
        builtin: { default_policy: 'allow', policies: { 'system.config.set': 'allow' } },
      },
      tools: [FENCED_ENTRY],
    })
  })

  it('renders fence badge when fence_applied=true', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={{ builtin: { default_policy: 'allow', policies: { 'system.config.set': 'allow' } } }}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText(/downgraded to ask/i)).toBeInTheDocument()
    })
  })

  it('shows configured vs effective policy when fence is applied', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={{ builtin: { default_policy: 'allow', policies: { 'system.config.set': 'allow' } } }}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      // The sub-label shows configured: allow → effective: ask
      expect(screen.getByText(/Configured: allow.*Effective: ask/i)).toBeInTheDocument()
    })
  })

  it('does not render fence badge when fence_applied=false', async () => {
    vi.mocked(api.fetchAgentTools).mockResolvedValue({
      config: DEFAULT_TOOLS_CFG,
      tools: [{
        name: 'system.config.set',
        configured_policy: 'allow',
        effective_policy: 'allow',
        fence_applied: false,
        requires_admin_ask: false,
      }],
    })
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.queryByText(/downgraded to ask/i)).not.toBeInTheDocument()
    })
  })
})

describe('ToolsAndPermissions — shell/fs conflict banner', () => {
  const SHELL_TOOL: RegistryTool = {
    name: 'workspace.shell',
    scope: 'general',
    category: 'shell',
    description: 'Run a shell command in the workspace',
    source: 'builtin',
  }
  const FS_TOOLS: RegistryTool[] = [
    { name: 'write_file', scope: 'general', category: 'filesystem', description: 'Write file', source: 'builtin' },
    { name: 'read_file', scope: 'general', category: 'filesystem', description: 'Read file', source: 'builtin' },
    { name: 'list_dir', scope: 'general', category: 'filesystem', description: 'List dir', source: 'builtin' },
  ]

  beforeEach(() => {
    vi.mocked(api.fetchRegistryTools).mockResolvedValue([SHELL_TOOL, ...FS_TOOLS])
    vi.mocked(api.fetchAgentTools).mockResolvedValue({ config: DEFAULT_TOOLS_CFG, tools: [] })
    vi.mocked(api.fetchGlobalToolPolicies).mockResolvedValue({ default_policy: 'allow', policies: {} })
  })

  it('banner renders when workspace.shell is allow and a filesystem tool is deny', async () => {
    const conflictTools: AgentToolsCfg = {
      builtin: {
        default_policy: 'allow',
        policies: { write_file: 'deny' },
      },
    }
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={conflictTools}
        onChange={NOOP_CHANGE}
      />,
    )
    await waitFor(() => {
      expect(screen.getByTestId('shell-fs-conflict-banner')).toBeInTheDocument()
    })
  })

  it('banner hidden when workspace.shell is deny', async () => {
    const noConflictTools: AgentToolsCfg = {
      builtin: {
        default_policy: 'allow',
        policies: { 'workspace.shell': 'deny', write_file: 'deny' },
      },
    }
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={noConflictTools}
        onChange={NOOP_CHANGE}
      />,
    )
    await waitFor(() => {
      expect(screen.queryByTestId('shell-fs-conflict-banner')).not.toBeInTheDocument()
    })
  })

  it('banner hidden when no filesystem tool is denied', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />,
    )
    await waitFor(() => {
      expect(screen.queryByTestId('shell-fs-conflict-banner')).not.toBeInTheDocument()
    })
  })

  it('banner text is visible when conflict exists', async () => {
    const conflictTools: AgentToolsCfg = {
      builtin: {
        default_policy: 'allow',
        policies: { read_file: 'deny' },
      },
    }
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={conflictTools}
        onChange={NOOP_CHANGE}
      />,
    )
    await waitFor(() => {
      const banner = screen.getByTestId('shell-fs-conflict-banner')
      expect(banner.textContent).toMatch(/workspace\.shell/i)
      expect(banner.textContent).toMatch(/won.t stop the shell/i)
    })
  })
})

describe('ToolsAndPermissions — preset confirmation dialog (FR-043, FR-044)', () => {
  it('opens confirmation dialog when preset button is clicked', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('Cautious')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Cautious'))

    await waitFor(() => {
      expect(screen.getByText(/Apply preset: Cautious/i)).toBeInTheDocument()
      expect(screen.getByText(/replace/i)).toBeInTheDocument()
    })
  })

  it('applies replace semantics on confirm — existing overrides discarded', async () => {
    const onChange = vi.fn()
    const existingTools: AgentToolsCfg = {
      builtin: {
        default_policy: 'deny',
        policies: { exec: 'deny', read_file: 'allow' },
      },
    }

    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={existingTools}
        onChange={onChange}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('Unrestricted')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Unrestricted'))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Apply preset/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /Apply preset/i }))

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({
          builtin: expect.objectContaining({
            default_policy: 'allow',
            policies: {}, // replaced, not merged
          }),
        })
      )
    })
  })

  it('cancels without applying when Cancel is clicked', async () => {
    const onChange = vi.fn()

    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={onChange}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('Minimal')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Minimal'))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^Cancel$/i })).toBeInTheDocument()
    })

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: /^Cancel$/i }))
    })

    // Dialog should close, onChange should NOT have been called
    await waitFor(() => {
      expect(screen.queryByText(/Apply preset: Minimal/i)).not.toBeInTheDocument()
    })
    expect(onChange).not.toHaveBeenCalled()
  })

  it('dialog mentions fence semantics for admin-required tools', async () => {
    renderWithQuery(
      <ToolsAndPermissions
        agentId="agent-1"
        agentType="custom"
        tools={DEFAULT_TOOLS_CFG}
        onChange={NOOP_CHANGE}
      />
    )
    await waitFor(() => {
      expect(screen.getByText('Standard')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Standard'))

    await waitFor(() => {
      // Dialog contains the fence semantics note about admin-required tools
      expect(screen.getByText(/admin-required tools on custom agents/i)).toBeInTheDocument()
      // The note references the "ask" policy word in a code span
      const fenceNote = screen.getByText(/admin-required tools on custom agents/i)
      expect(fenceNote.textContent).toMatch(/ask/)
    })
  })
})
