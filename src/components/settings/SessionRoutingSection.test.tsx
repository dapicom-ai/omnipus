import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchSessionScope: vi.fn(),
    updateSessionScope: vi.fn(),
  }
})

vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn(() => ({ addToast: vi.fn() })),
}))

vi.mock('@/store/auth', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useAuthStore: vi.fn((selector: any) => selector({ role: 'admin' })),
}))

import { fetchSessionScope, updateSessionScope } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { SessionRoutingSection } from './SessionRoutingSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <SessionRoutingSection />
    </QueryClientProvider>
  )
}

const mockAddToast = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useUiStore).mockReturnValue({ addToast: mockAddToast } as never)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'admin' }))
})

// =====================================================================
// Four radios render with subtitles
// =====================================================================

describe('SessionRoutingSection — radio rendering', () => {
  it('renders exactly four radios', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => expect(screen.getAllByRole('radio')).toHaveLength(4))
  })

  it('renders main scope subtitle', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => screen.getByText(/one session per agent across all dms/i))
    expect(screen.getByText(/one session per agent across all dms/i)).toBeInTheDocument()
  })

  it('renders per-peer scope subtitle', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => screen.getByText(/separate session per sender identity/i))
    expect(screen.getByText(/separate session per sender identity/i)).toBeInTheDocument()
  })

  it('renders per-channel-peer scope subtitle', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => screen.getByText(/separate session per \(channel, sender\)/i))
    expect(screen.getByText(/separate session per \(channel, sender\)/i)).toBeInTheDocument()
  })

  it('renders per-account-channel-peer scope subtitle', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => screen.getByText(/one bot serves multiple tenants/i))
    expect(screen.getByText(/one bot serves multiple tenants/i)).toBeInTheDocument()
  })

  it('does NOT render the string "global" anywhere in the DOM', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    renderSection()
    await waitFor(() => screen.getAllByRole('radio'))
    expect(document.body.textContent).not.toMatch(/\bglobal\b/i)
  })
})

// =====================================================================
// Save fires updateSessionScope with canonical value
// =====================================================================

describe('SessionRoutingSection — save', () => {
  it('save fires updateSessionScope with dm_scope: main', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    vi.mocked(updateSessionScope).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_dm_scope: 'main',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[0]) // main
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(updateSessionScope).toHaveBeenCalledWith('main')
    })
  })

  it('save fires updateSessionScope with dm_scope: per-peer', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    vi.mocked(updateSessionScope).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_dm_scope: 'per-peer',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[1]) // per-peer
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(updateSessionScope).toHaveBeenCalledWith('per-peer')
    })
  })

  it('shows restart-required badge after save', async () => {
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })
    vi.mocked(updateSessionScope).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_dm_scope: 'main',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[0])
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(screen.getByText(/restart required/i)).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Non-admin: Save hidden
// =====================================================================

describe('SessionRoutingSection — non-admin', () => {
  it('hides Save button for non-admin', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'user' }))
    vi.mocked(fetchSessionScope).mockResolvedValue({ dm_scope: 'per-channel-peer' })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
