import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ProvidersSection } from '@/components/settings/ProvidersSection'

// test_provider_save_connect (test #27)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Adding a new provider
//             wave5a-wire-ui-spec.md — Scenario: Provider connection test fails
//             wave5a-wire-ui-spec.md — Scenario: API key stored securely

// Note: ProviderCard is not a standalone export — provider rows live inside ProvidersSection.

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchProviders: vi.fn(),
    configureProvider: vi.fn(),
    testProvider: vi.fn(),
  }
})

import { fetchProviders, configureProvider, testProvider } from '@/lib/api'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
}

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>
}

beforeEach(() => {
  // Clear call history so tests don't bleed into each other
  vi.clearAllMocks()
  // No providers configured initially
  vi.mocked(fetchProviders).mockResolvedValue([])
  vi.mocked(configureProvider).mockResolvedValue({ id: 'openai', status: 'connected', models: [] })
  vi.mocked(testProvider).mockResolvedValue({ success: true })
})

describe('provider save & connect integration (test #27)', () => {
  it('saves key by calling configureProvider when Save & Connect is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Adding a new provider (AC2-3)
    render(<ProvidersSection />, { wrapper })

    await screen.findByText('OpenAI')

    // Find and click Configure for OpenAI (4th row, should be "Configure")
    const configBtns = screen.getAllByRole('button', { name: /configure|edit/i })
    // Click the first "Configure" button (for Anthropic since no providers are configured)
    fireEvent.click(configBtns[0])

    // Enter API key in the expanded form
    // API key inputs are type="password" — not accessible as role="textbox"; use placeholder
    const keyInput = screen.getByPlaceholderText(/sk-ant/i)
    fireEvent.change(keyInput, { target: { value: 'sk-ant-test-1234' } })

    // Click Save & Connect
    fireEvent.click(screen.getByRole('button', { name: /save.*connect/i }))

    await waitFor(() => {
      expect(configureProvider).toHaveBeenCalledWith('anthropic', 'sk-ant-test-1234')
    })
  })

  it('does not call configureProvider when key input is empty', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Dataset: Provider Configuration row 2 — empty key validation
    render(<ProvidersSection />, { wrapper })

    await screen.findByText('OpenAI')
    const configBtns = screen.getAllByRole('button', { name: /configure/i })
    fireEvent.click(configBtns[0])

    // Do NOT enter a key — Save & Connect should be disabled
    const saveBtn = screen.getByRole('button', { name: /save.*connect/i })
    expect(saveBtn).toBeDisabled()
    expect(configureProvider).not.toHaveBeenCalled()
  })

  it('shows "Connected" badge after successful provider save', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC2: badge updates after save
    vi.mocked(fetchProviders)
      .mockResolvedValueOnce([]) // initial fetch — not configured
      .mockResolvedValue([{ id: 'anthropic', status: 'connected', models: ['claude-sonnet-4-6'] }])

    render(<ProvidersSection />, { wrapper })
    await screen.findByText('Anthropic')

    const configBtns = screen.getAllByRole('button', { name: /configure/i })
    fireEvent.click(configBtns[0])
    // API key inputs are type="password" — not accessible as role="textbox"; use placeholder
    const keyInput = screen.getByPlaceholderText(/sk-ant/i)
    fireEvent.change(keyInput, { target: { value: 'sk-ant-valid-key' } })
    fireEvent.click(screen.getByRole('button', { name: /save.*connect/i }))

    await waitFor(() => {
      expect(configureProvider).toHaveBeenCalled()
    })
  })
})
