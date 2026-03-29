import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SessionBar } from '../chat/SessionBar'
import { useChatStore } from '@/store/chat'
import { useUiStore } from '@/store/ui'

// test_session_bar_elements (test #11)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Session bar shows agent name, tokens, cost
//             wave5a-wire-ui-spec.md — Scenario: Agent selection via session bar dropdown

// Note: SessionBar lives at src/components/chat/SessionBar.tsx (not layout/)

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchAgents: vi.fn().mockResolvedValue([
      { id: 'general-assistant', name: 'General Assistant', type: 'core', status: 'active', model: 'claude-sonnet-4-6', description: 'General purpose' },
      { id: 'researcher', name: 'Researcher', type: 'core', status: 'idle', description: 'Research agent' },
    ]),
  }
})

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderBar() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <SessionBar />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  act(() => {
    useChatStore.setState({
      activeAgentId: 'general-assistant',
      activeSessionId: 'sess_1',
      sessionTokens: 1500,
      sessionCost: 0.0023,
      isStreaming: false,
    })
    useUiStore.setState({ sessionPanelOpen: false })
  })
})

describe('SessionBar — rendering (test #11)', () => {
  it('shows active agent name in the agent selector button', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Session bar shows agent name (AC1)
    renderBar()
    await screen.findByText('General Assistant')
    expect(screen.getByText('General Assistant')).toBeInTheDocument()
  })

  it('shows "Select agent" when no active agent', async () => {
    // Dataset: Session Bar row 3 — no active agent
    act(() => { useChatStore.setState({ activeAgentId: null }) })
    renderBar()
    await vi.waitFor(() => {
      expect(screen.getByText(/select agent/i)).toBeInTheDocument()
    })
  })

  it('renders Sessions button', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: Sessions button opens history panel
    renderBar()
    await vi.waitFor(() => {
      expect(screen.getByRole('button', { name: /sessions/i })).toBeInTheDocument()
    })
  })
})

describe('SessionBar — sessions panel (test #11)', () => {
  it('calls openSessionPanel when Sessions button is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: Sessions button opens history panel
    renderBar()
    await vi.waitFor(() => screen.getByRole('button', { name: /sessions/i }))
    fireEvent.click(screen.getByRole('button', { name: /sessions/i }))
    expect(useUiStore.getState().sessionPanelOpen).toBe(true)
  })
})
