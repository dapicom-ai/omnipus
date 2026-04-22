import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchAuditLogToggle: vi.fn(),
    updateAuditLog: vi.fn(),
  }
})

vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn(() => ({ addToast: vi.fn() })),
}))

vi.mock('@/store/auth', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useAuthStore: vi.fn((selector: any) => selector({ role: 'admin' })),
}))

import { fetchAuditLogToggle, updateAuditLog } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { AuditLogSection } from './AuditLogSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <AuditLogSection />
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
// Toggle off → on → save → updateAuditLog called with {enabled: true}
// =====================================================================

describe('AuditLogSection — save flow', () => {
  it('toggling off→on and saving calls updateAuditLog with enabled: true', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_enabled: true,
    })

    renderSection()

    const checkbox = await screen.findByRole('switch')
    expect(checkbox).not.toBeChecked()

    fireEvent.click(checkbox)
    expect(checkbox).toBeChecked()

    const saveBtn = screen.getByRole('button', { name: /save/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(updateAuditLog).toHaveBeenCalledWith(true)
    })
  })

  it('shows Restart required badge after save when server returns requires_restart: true', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_enabled: true,
    })

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(screen.getByText(/restart required/i)).toBeInTheDocument()
    })
  })

  it('shows success toast after save', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_enabled: true,
    })

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ variant: 'success' })
      )
    })
  })

  it('shows error toast when save fails', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockRejectedValue(new Error('save failed'))

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ variant: 'error' })
      )
    })
  })
})

// =====================================================================
// Non-admin: Save button hidden, input disabled
// =====================================================================

describe('AuditLogSection — non-admin', () => {
  it('hides Save button for non-admin users', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'user' }))
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: true } as never)

    renderSection()

    await screen.findByRole('switch')
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('disables the checkbox for non-admin users', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'user' }))
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: true } as never)

    renderSection()

    const checkbox = await screen.findByRole('switch')
    expect(checkbox).toBeDisabled()
  })
})
