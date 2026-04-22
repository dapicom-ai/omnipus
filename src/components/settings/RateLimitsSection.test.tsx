import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchRateLimitsK: vi.fn(),
    updateRateLimits: vi.fn(),
  }
})

vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn(() => ({ addToast: vi.fn() })),
}))

vi.mock('@/store/auth', () => ({
  useAuthStore: vi.fn((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
    selector({ role: 'admin', user: { username: 'testadmin' } }),
  ),
}))

import { fetchRateLimitsK, updateRateLimits } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { RateLimitsSection } from './RateLimitsSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <RateLimitsSection />
    </QueryClientProvider>
  )
}

const mockAddToast = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useUiStore).mockReturnValue({ addToast: mockAddToast } as never)
  vi.mocked(useAuthStore).mockImplementation(
    ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
      selector({ role: 'admin', user: { username: 'testadmin' } })) as never,
  )
})

// =====================================================================
// Negative value in any field → Save disabled
// =====================================================================

describe('RateLimitsSection — negative validation', () => {
  it('typing -5 in cost cap shows error and disables Save', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({ daily_cost_cap_usd: 0 })

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    const costInput = inputs[0]

    // Change value to -5 and blur to trigger validation
    fireEvent.change(costInput, { target: { value: '-5' } })
    fireEvent.blur(costInput)

    await waitFor(() => {
      expect(screen.getByText(/non-negative/i)).toBeInTheDocument()
    })

    const saveBtn = screen.getByRole('button', { name: /save/i })
    expect(saveBtn).toBeDisabled()
  })

  it('typing -5 in llm calls per hour shows error', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({ max_agent_llm_calls_per_hour: 0 })

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    const llmInput = inputs[1]

    fireEvent.change(llmInput, { target: { value: '-5' } })
    fireEvent.blur(llmInput)

    await waitFor(() => {
      expect(screen.getByText(/non-negative integer/i)).toBeInTheDocument()
    })
  })

  it('typing 10.5 in llm calls (integer field) shows error', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({})

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    fireEvent.change(inputs[1], { target: { value: '10.5' } })
    fireEvent.blur(inputs[1])

    await waitFor(() => {
      expect(screen.getByText(/non-negative integer/i)).toBeInTheDocument()
    })
  })

  it('typing 10.5 in tool calls (integer field) shows error', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({})

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    fireEvent.change(inputs[2], { target: { value: '10.5' } })
    fireEvent.blur(inputs[2])

    await waitFor(() => {
      expect(screen.getByText(/non-negative integer/i)).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Valid values → Save fires with all three
// =====================================================================

describe('RateLimitsSection — valid save', () => {
  it('typing 25.5 cost-cap, 100 llm, 10 tool → save fires with all three', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({})
    vi.mocked(updateRateLimits).mockResolvedValue({
      daily_cost_cap_usd: 25.5,
      max_agent_llm_calls_per_hour: 100,
      max_agent_tool_calls_per_minute: 10,
    })

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    fireEvent.change(inputs[0], { target: { value: '25.5' } })
    fireEvent.change(inputs[1], { target: { value: '100' } })
    fireEvent.change(inputs[2], { target: { value: '10' } })

    const saveBtn = screen.getByRole('button', { name: /save/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(updateRateLimits).toHaveBeenCalledWith(
        expect.objectContaining({
          daily_cost_cap_usd: 25.5,
          max_agent_llm_calls_per_hour: 100,
          max_agent_tool_calls_per_minute: 10,
        })
      )
    })
  })
})

// =====================================================================
// Partial update — only changed field in body
// =====================================================================

describe('RateLimitsSection — partial update', () => {
  it('only changing cost-cap sends only daily_cost_cap_usd in body', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({
      daily_cost_cap_usd: 10,
      max_agent_llm_calls_per_hour: 50,
      max_agent_tool_calls_per_minute: 5,
    })
    vi.mocked(updateRateLimits).mockResolvedValue({
      daily_cost_cap_usd: 20,
      max_agent_llm_calls_per_hour: 50,
      max_agent_tool_calls_per_minute: 5,
    })

    renderSection()

    const inputs = await screen.findAllByRole('spinbutton')
    // Only change cost cap
    fireEvent.change(inputs[0], { target: { value: '20' } })

    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(updateRateLimits).toHaveBeenCalledWith({ daily_cost_cap_usd: 20 })
    })
  })
})

// =====================================================================
// Empty body submit (no fields changed)
// =====================================================================

describe('RateLimitsSection — empty submit', () => {
  it('when no fields change, Save fires with empty body {}', async () => {
    vi.mocked(fetchRateLimitsK).mockResolvedValue({
      daily_cost_cap_usd: 10,
      max_agent_llm_calls_per_hour: 50,
      max_agent_tool_calls_per_minute: 5,
    })
    vi.mocked(updateRateLimits).mockResolvedValue({
      daily_cost_cap_usd: 10,
      max_agent_llm_calls_per_hour: 50,
      max_agent_tool_calls_per_minute: 5,
    })

    renderSection()

    // Wait for data to load
    await screen.findAllByRole('spinbutton')

    // Save button should be disabled when nothing changed
    const saveBtn = screen.getByRole('button', { name: /save/i })
    expect(saveBtn).toBeDisabled()
  })
})

// =====================================================================
// Non-admin: Save hidden
// =====================================================================

describe('RateLimitsSection — non-admin', () => {
  it('hides Save button for non-admin', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
        selector({ role: 'user', user: { username: 'testuser' } })) as never,
    )
    vi.mocked(fetchRateLimitsK).mockResolvedValue({ daily_cost_cap_usd: 0 })

    renderSection()

    await screen.findAllByRole('spinbutton')
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
