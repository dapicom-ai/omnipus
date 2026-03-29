import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ProvidersSection } from './ProvidersSection'

// test_provider_card_component (test #18) — tests ProvidersSection (ProviderCard is inline, not exported)
// Traces to: wave5a-wire-ui-spec.md — Scenario: Provider configuration form
//             wave5a-wire-ui-spec.md — Scenario: Provider status badges

// Note: ProviderCard is not a standalone component — provider rows are rendered inline
// inside ProvidersSection. This test file tests ProvidersSection instead.

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchProviders: vi.fn(),
    configureProvider: vi.fn(),
    testProvider: vi.fn(),
  }
})

import { fetchProviders } from '@/lib/api'

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <ProvidersSection />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.mocked(fetchProviders).mockResolvedValue([
    { id: 'anthropic', status: 'connected', models: ['claude-sonnet-4-6', 'claude-opus-4-6'] },
    { id: 'openai', status: 'error', error: 'Invalid API key' },
  ])
})

describe('ProvidersSection — provider list (test #18)', () => {
  it('renders all available provider names', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Provider list shows all providers
    renderSection()
    await screen.findByText('Anthropic')
    expect(screen.getByText('OpenAI')).toBeInTheDocument()
    expect(screen.getByText('Google Gemini')).toBeInTheDocument()
    expect(screen.getByText('Groq')).toBeInTheDocument()
    expect(screen.getByText('OpenRouter')).toBeInTheDocument()
  })

  it('shows "Connected" badge for a connected provider', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC1: connected status badge
    // Dataset: Provider Configuration row 1
    renderSection()
    await screen.findByText('Anthropic')
    expect(screen.getByText('Connected')).toBeInTheDocument()
  })

  it('shows "Error" badge for a provider with error status', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC2: error status badge
    // Dataset: Provider Configuration row 3
    renderSection()
    await screen.findByText('Anthropic')
    expect(screen.getByText('Error')).toBeInTheDocument()
    expect(screen.getByText('Invalid API key')).toBeInTheDocument()
  })

  it('shows "Not configured" badge for unconfigured providers', async () => {
    // Dataset: Provider Configuration row 5 — not configured
    renderSection()
    await screen.findByText('Anthropic')
    const notConfigured = screen.getAllByText('Not configured')
    expect(notConfigured.length).toBeGreaterThan(0)
  })
})

describe('ProvidersSection — configure form (test #18)', () => {
  it('shows API key input when Configure/Edit is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Provider configuration form
    renderSection()
    await screen.findByText('Anthropic')
    // Click Edit for Anthropic (first Configure button for unconfigured providers)
    const configureButtons = screen.getAllByRole('button', { name: /configure|edit/i })
    fireEvent.click(configureButtons[0])
    expect(screen.getByPlaceholderText(/sk-ant/i)).toBeInTheDocument()
  })

  it('shows eye/hide toggle for API key field', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC3: password field with show/hide toggle
    renderSection()
    await screen.findByText('Anthropic')
    const configureButtons = screen.getAllByRole('button', { name: /configure|edit/i })
    fireEvent.click(configureButtons[0])
    expect(screen.getByRole('button', { name: /show api key/i })).toBeInTheDocument()
  })
})
