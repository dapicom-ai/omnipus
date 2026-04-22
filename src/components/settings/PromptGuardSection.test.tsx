import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchPromptGuardLevel: vi.fn(),
    updatePromptGuardLevel: vi.fn(),
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

import { fetchPromptGuardLevel, updatePromptGuardLevel } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { PromptGuardSection } from './PromptGuardSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <PromptGuardSection />
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
// Three radios with canonical values + subtitles
// =====================================================================

describe('PromptGuardSection — radio rendering', () => {
  it('renders three radios', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    renderSection()
    await waitFor(() => expect(screen.getAllByRole('radio')).toHaveLength(3))
  })

  it('renders low level with its subtitle', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    renderSection()
    await waitFor(() => screen.getByText(/minimal sanitization/i))
    expect(screen.getByText(/minimal sanitization/i)).toBeInTheDocument()
  })

  it('renders medium level with its subtitle', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    renderSection()
    await waitFor(() => screen.getByText(/balanced sanitization/i))
    expect(screen.getByText(/balanced sanitization/i)).toBeInTheDocument()
  })

  it('renders high level with its subtitle', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    renderSection()
    await waitFor(() => screen.getByText(/aggressive sanitization/i))
    expect(screen.getByText(/aggressive sanitization/i)).toBeInTheDocument()
  })
})

// =====================================================================
// Save fires updatePromptGuardLevel with correct value
// =====================================================================

describe('PromptGuardSection — save', () => {
  it('selecting high and saving fires updatePromptGuardLevel with level: high', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    vi.mocked(updatePromptGuardLevel).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_level: 'high',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[2]) // high is the third

    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(updatePromptGuardLevel).toHaveBeenCalledWith('high')
    })
  })

  it('no restart badge when response has requires_restart: false', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })
    vi.mocked(updatePromptGuardLevel).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_level: 'high',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[2])
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(updatePromptGuardLevel).toHaveBeenCalled()
    })
    expect(screen.queryByText(/restart required/i)).not.toBeInTheDocument()
  })

  it('shows success toast on save', async () => {
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'low' })
    vi.mocked(updatePromptGuardLevel).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_level: 'medium',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[1]) // medium
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ variant: 'success' })
      )
    })
  })
})

// =====================================================================
// Non-admin: Save hidden
// =====================================================================

describe('PromptGuardSection — non-admin', () => {
  it('hides Save button for non-admin', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
        selector({ role: 'user', user: { username: 'testuser' } })) as never,
    )
    vi.mocked(fetchPromptGuardLevel).mockResolvedValue({ level: 'medium' })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
