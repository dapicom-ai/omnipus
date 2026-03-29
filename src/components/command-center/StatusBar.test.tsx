import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StatusBar } from './StatusBar'

// test_status_bar_component (test #17)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Command center status bar shows gateway health
//             wave5a-wire-ui-spec.md — US-13 AC1: gateway status, agent count, channel count, daily cost

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchGatewayStatus: vi.fn().mockResolvedValue({
      online: true,
      agent_count: 3,
      channel_count: 2,
      daily_cost: 0.0045,
      version: '0.1.0',
    }),
  }
})

import { fetchGatewayStatus } from '@/lib/api'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderBar() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <StatusBar />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.mocked(fetchGatewayStatus).mockResolvedValue({
    online: true,
    agent_count: 3,
    channel_count: 2,
    daily_cost: 0.0045,
    version: '0.1.0',
  })
})

describe('StatusBar — gateway online (test #17)', () => {
  it('shows "Gateway online" with agent count and channel count', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-13 AC1: gateway status
    renderBar()
    await screen.findByText(/Gateway online/i)
    expect(screen.getByText(/3 agents/i)).toBeInTheDocument()
    expect(screen.getByText(/2 channels/i)).toBeInTheDocument()
  })

  it('shows daily cost', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-13 AC1: daily cost
    renderBar()
    await screen.findByText(/Gateway online/i)
    expect(screen.getByText(/0\.0045/)).toBeInTheDocument()
  })

  it('shows version number when present', async () => {
    // Dataset: Status Bar row 1 — version present
    renderBar()
    await screen.findByText(/v0\.1\.0/i)
    expect(screen.getByText(/v0\.1\.0/i)).toBeInTheDocument()
  })
})

describe('StatusBar — gateway offline', () => {
  it('shows "Gateway offline" when online is false', async () => {
    // Traces to: wave5a-wire-ui-spec.md — US-13 AC2: offline indicator
    // Dataset: Status Bar row 2 — gateway offline
    vi.mocked(fetchGatewayStatus).mockResolvedValue({
      online: false,
      agent_count: 0,
      channel_count: 0,
      daily_cost: 0,
    })
    renderBar()
    await screen.findByText(/Gateway offline/i)
    expect(screen.getByText(/Gateway offline/i)).toBeInTheDocument()
  })
})
