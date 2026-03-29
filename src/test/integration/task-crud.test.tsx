import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CommandCenterScreen } from '@/routes/_app/command-center'

// test_task_crud (test #28)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Task list renders with status grouping
//             wave5a-wire-ui-spec.md — Scenario: Status bar shows system overview

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchGatewayStatus: vi.fn(),
    fetchTasks: vi.fn(),
    createTask: vi.fn(),
  }
})

import { fetchGatewayStatus, fetchTasks } from '@/lib/api'

const mockStatus = {
  online: true,
  agent_count: 3,
  channel_count: 5,
  daily_cost: 4.82,
  version: '0.1.0',
}

const mockTasks = [
  { id: 't1', name: 'Refactor auth', agent_id: 'general-assistant', agent_name: 'General Assistant', status: 'active' as const, cost: 0.45, created_at: '2026-03-29T09:00:00Z', updated_at: '2026-03-29T10:00:00Z' },
  { id: 't2', name: 'Write docs', agent_id: 'researcher', agent_name: 'Researcher', status: 'next' as const, created_at: '2026-03-29T08:00:00Z', updated_at: '2026-03-29T09:00:00Z' },
  { id: 't3', name: 'Deploy', agent_id: 'general-assistant', agent_name: 'General Assistant', status: 'done' as const, cost: 1.23, created_at: '2026-03-28T08:00:00Z', updated_at: '2026-03-29T08:00:00Z' },
]

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>
}

beforeEach(() => {
  vi.mocked(fetchGatewayStatus).mockResolvedValue(mockStatus)
  vi.mocked(fetchTasks).mockResolvedValue(mockTasks)
})

describe('command center integration (test #28)', () => {
  it('displays gateway online status from API', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Status bar shows system overview
    render(<CommandCenterScreen />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/gateway online/i)).toBeInTheDocument()
    })
  })

  it('displays agent count and channel count from API', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-13 AC1: agent count, channel count
    render(<CommandCenterScreen />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/3 agents/i)).toBeInTheDocument()
      expect(screen.getByText(/5 channels/i)).toBeInTheDocument()
    })
  })

  it('renders task list with tasks from API', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Task list renders with status grouping
    render(<CommandCenterScreen />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/Refactor auth/i)).toBeInTheDocument()
      expect(screen.getByText(/Write docs/i)).toBeInTheDocument()
    })
  })

  it('shows "Gateway offline" when API returns online: false', async () => {
    // Dataset: Status Bar row 2 — gateway offline
    vi.mocked(fetchGatewayStatus).mockResolvedValue({ ...mockStatus, online: false })
    render(<CommandCenterScreen />, { wrapper })

    await waitFor(() => {
      expect(screen.getByText(/gateway offline/i)).toBeInTheDocument()
    })
  })
})
