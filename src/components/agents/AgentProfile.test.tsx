import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AgentProfile } from './AgentProfile'
import type { Agent } from '@/lib/api'

// test_agent_profile_sections (test #13)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Agent profile renders with type-appropriate sections
//             wave5a-wire-ui-spec.md — US-7 AC2: core agent sections
//             wave5a-wire-ui-spec.md — US-7 AC3: system agent sections

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return { ...actual, useNavigate: () => mockNavigate, useParams: () => ({}) }
})

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchAgent: vi.fn(), updateAgent: vi.fn() }
})

import { fetchAgent } from '@/lib/api'

const mockCoreAgent: Agent = {
  id: 'general-assistant',
  name: 'General Assistant',
  type: 'core',
  status: 'active',
  model: 'claude-sonnet-4-6',
  description: 'General purpose assistant',
  rate_limits: { use_global_defaults: true },
  tools: ['web_search', 'exec'],
  stats: { total_sessions: 5, total_tokens: 12000, total_cost: 0.05 },
}

const mockSystemAgent: Agent = {
  id: 'omnipus-system',
  name: 'Omnipus System',
  type: 'system',
  status: 'active',
  model: 'claude-opus-4-6',
  description: 'System agent with exclusive system.* tools',
}

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderProfile(agentId: string) {
  return render(
    <QueryClientProvider client={makeClient()}>
      <AgentProfile agentId={agentId} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  mockNavigate.mockClear()
  vi.mocked(fetchAgent).mockResolvedValue(mockCoreAgent)
})

describe('AgentProfile — loading state', () => {
  it('shows "Loading agent..." while data is fetching', () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7: profile shows loading state
    vi.mocked(fetchAgent).mockReturnValue(new Promise(() => {})) // never resolves
    renderProfile('general-assistant')
    expect(screen.getByText(/loading agent/i)).toBeInTheDocument()
  })
})

describe('AgentProfile — error state', () => {
  it('shows error message when fetch fails', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7: profile shows error state
    vi.mocked(fetchAgent).mockRejectedValue(new Error('Not found'))
    renderProfile('bad-id')
    const errorMsg = await screen.findByText(/could not load agent/i)
    expect(errorMsg).toBeInTheDocument()
  })
})

describe('AgentProfile — core agent sections (test #13)', () => {
  it('renders agent name, type badge, and model section after loading', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7 AC2: core agent sections
    renderProfile('general-assistant')
    await screen.findByText('General Assistant')
    expect(screen.getByText(/core/i)).toBeInTheDocument()
    expect(screen.getByText(/Model/)).toBeInTheDocument()
  })

  it('shows Rate Limits section with "Use global defaults" for core agent', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7 AC5: rate limits defaults toggle
    renderProfile('general-assistant')
    await screen.findByText('General Assistant')
    // Use heading role to distinguish the h2 from any <p> that also contains "Rate Limits"
    expect(screen.getByRole('heading', { name: /Rate Limits/i })).toBeInTheDocument()
    expect(screen.getByText(/Use global defaults/i)).toBeInTheDocument()
  })

  it('shows Stats section when stats are present', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7: stats section
    renderProfile('general-assistant')
    await screen.findByText('General Assistant')
    expect(screen.getByText('Sessions')).toBeInTheDocument()
  })

  it('shows Save button for core (editable) agent', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7 AC2: editable sections for core
    renderProfile('general-assistant')
    await screen.findByText('General Assistant')
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('shows tools & skills section when tools are present', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7: tools section
    renderProfile('general-assistant')
    await screen.findByText('General Assistant')
    expect(screen.getByText('Tools & Skills')).toBeInTheDocument()
    expect(screen.getByText('web_search')).toBeInTheDocument()
  })
})

describe('AgentProfile — system agent sections (test #13)', () => {
  beforeEach(() => {
    vi.mocked(fetchAgent).mockResolvedValue(mockSystemAgent)
  })

  it('does NOT show Rate Limits for system agents', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7 AC3: system agent sections
    renderProfile('omnipus-system')
    await screen.findByText('Omnipus System')
    expect(screen.queryByText(/Rate Limits/i)).toBeNull()
  })

  it('does NOT show Save button for system agent', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-7 AC3: system agents are not editable
    renderProfile('omnipus-system')
    await screen.findByText('Omnipus System')
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
  })
})
