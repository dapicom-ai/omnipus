import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CreateAgentModal } from '@/components/agents/CreateAgentModal'

// test_create_agent_flow (test #29)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Creating a custom agent

function makeQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={makeQueryClient()}>{children}</QueryClientProvider>
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

describe('create agent flow integration', () => {
  it('submits POST /api/v1/agents and closes modal on success', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Creating a custom agent (AC2)
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'devops-agent', name: 'DevOps Agent' }),
    } as Response)

    const onClose = vi.fn()
    const onCreate = vi.fn().mockResolvedValue(undefined)

    render(
      <CreateAgentModal open onClose={onClose} onCreate={onCreate} />,
      { wrapper }
    )

    const nameInput = screen.getByLabelText(/name/i) ?? screen.getByPlaceholderText(/name/i)
    fireEvent.change(nameInput, { target: { value: 'DevOps Agent' } })
    fireEvent.click(screen.getByRole('button', { name: /create agent/i }))

    await waitFor(() => {
      expect(onCreate).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'DevOps Agent' })
      )
    })
  })

  it('shows validation error for empty name and keeps modal open', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Validation prevents empty agent name (AC3)
    const onClose = vi.fn()
    const onCreate = vi.fn()

    render(
      <CreateAgentModal open onClose={onClose} onCreate={onCreate} />,
      { wrapper }
    )

    fireEvent.click(screen.getByRole('button', { name: /create agent/i }))

    await waitFor(() => {
      expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      expect(onCreate).not.toHaveBeenCalled()
      expect(onClose).not.toHaveBeenCalled()
    })
  })
})
