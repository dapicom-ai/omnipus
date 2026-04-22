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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useAuthStore: vi.fn((selector: any) => selector({ role: 'admin' })),
}))

import { fetchRetention, updateRetention, triggerRetentionSweep } from '@/lib/api'
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'admin' }))
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
// Disabled mode → confirmation modal on Save
// =====================================================================

describe('RetentionSection — disabled mode confirmation', () => {
  it('selecting Disabled and clicking Save shows confirmation modal', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[2]) // disabled

    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    expect(screen.getByText(/accumulate indefinitely/i)).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    await waitFor(() => {
      expect(updateRetention).toHaveBeenCalledWith({ session_days: 0, disabled: true })
    })
  })

  it('Cancel on modal does NOT fire updateRetention', async () => {
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    fireEvent.click(screen.getAllByRole('radio')[2])
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

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
    vi.mocked(triggerRetentionSweep).mockRejectedValue(new Error('409: sweep in progress'))

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
// Non-admin: Save hidden
// =====================================================================

describe('RetentionSection — non-admin', () => {
  it('hides Save button for non-admin', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'user' }))
    vi.mocked(fetchRetention).mockResolvedValue({ session_days: 0, disabled: false })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
