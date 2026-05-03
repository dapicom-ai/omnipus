import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchRetention: vi.fn(),
    updateRetention: vi.fn(),
    triggerRetentionSweep: vi.fn(),
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

import { fetchRetention, updateRetention, triggerRetentionSweep, ApiError } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { RetentionSection } from './RetentionSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <RetentionSection />
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
// Three modes render
// =====================================================================

describe('RetentionSection — mode rendering', () => {
  it('renders three mode radios', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    renderSection()
    await waitFor(() => expect(screen.getAllByRole('radio')).toHaveLength(3))
  })

  it('renders Default (90 days) option', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    renderSection()
    await waitFor(() => screen.getByText(/default \(90 days\)/i))
    expect(screen.getByText(/default \(90 days\)/i)).toBeInTheDocument()
  })

  it('renders Custom option', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    renderSection()
    await waitFor(() => screen.getByText(/^custom$/i))
    expect(screen.getByText(/^custom$/i)).toBeInTheDocument()
  })

  it('renders Disabled (keep forever) option', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    renderSection()
    await waitFor(() => screen.getByText(/disabled \(keep forever\)/i))
    expect(screen.getByText(/disabled \(keep forever\)/i)).toBeInTheDocument()
  })
})

// =====================================================================
// Autosave: non-forever modes save immediately on click
// =====================================================================

describe('RetentionSection — autosave on radio click', () => {
  it('clicking Default fires updateRetention immediately (no Save button)', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 30, disabled: false })
    vi.mocked(updateRetention).mockResolvedValue({
      saved: true,
      requires_restart: false,
      session_days: 0,
      disabled: false,
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    // Start from custom, click default
    fireEvent.click(screen.getAllByRole('radio')[0]) // default

    await waitFor(() => {
      expect(updateRetention).toHaveBeenCalledWith({ session_days: 0, disabled: false })
    })
  })

  it('no Save button is rendered', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('shows SaveStatus "Saving…" while mutation is in flight', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 30, disabled: false })
    vi.mocked(updateRetention).mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve({ saved: true, requires_restart: false, session_days: 0, disabled: false }), 50))
    )

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[0])

    await waitFor(() => {
      expect(screen.getByText(/saving/i)).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Disabled mode → confirmation modal intercepts autosave
// =====================================================================

describe('RetentionSection — disabled mode confirmation modal', () => {
  it('clicking Disabled (forever) shows confirmation modal before saving', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[2]) // disabled/forever

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    expect(screen.getByText(/accumulate indefinitely/i)).toBeInTheDocument()
    // Mutation should NOT have fired yet
    expect(updateRetention).not.toHaveBeenCalled()
  })

  it('Continue on modal fires updateRetention with disabled: true', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    vi.mocked(updateRetention).mockResolvedValue({
      saved: true,
      requires_restart: false,
      session_days: 0,
      disabled: true,
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[2]) // disabled

    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    await waitFor(() => {
      expect(updateRetention).toHaveBeenCalledWith({ session_days: 0, disabled: true })
    })
  })

  it('Cancel on modal does NOT fire updateRetention and reverts mode', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[2])

    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    expect(updateRetention).not.toHaveBeenCalled()
  })
})

// =====================================================================
// Stored disabled: true → yellow warning alert
// =====================================================================

describe('RetentionSection — disabled warning banner', () => {
  it('renders persistent yellow warning when server has disabled: true', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: true })

    renderSection()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent(/sessions will accumulate indefinitely/i)
  })

  it('does NOT render warning alert when retention is enabled', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

// =====================================================================
// Run sweep now button
// =====================================================================

describe('RetentionSection — sweep button', () => {
  it('Run sweep now fires triggerRetentionSweep and shows toast with removed count', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    vi.mocked(triggerRetentionSweep).mockResolvedValue({ removed: 42 })

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run sweep now/i }))
    fireEvent.click(screen.getByRole('button', { name: /run sweep now/i }))

    await waitFor(() => {
      expect(triggerRetentionSweep).toHaveBeenCalledOnce()
    })
    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ message: expect.stringContaining('42') })
      )
    })
  })

  it('409 sweep-in-progress response shows specific toast', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })
    vi.mocked(triggerRetentionSweep).mockRejectedValue(new ApiError(409, 'sweep in progress'))

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run sweep now/i }))
    fireEvent.click(screen.getByRole('button', { name: /run sweep now/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringContaining('sweep is already running'),
          variant: 'error',
        })
      )
    })
  })

  it('hides Run sweep now button when retention is disabled', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: true })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /run sweep now/i })).not.toBeInTheDocument()
  })
})

// =====================================================================
// Non-admin: no Save button
// =====================================================================

describe('RetentionSection — non-admin', () => {
  it('does not render a Save button for non-admin', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
        selector({ role: 'user', user: { username: 'testuser' } })) as never,
    )
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
