import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
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
  useAuthStore: vi.fn((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
    selector({ role: 'admin', user: { username: 'testadmin' } }),
  ),
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
  vi.mocked(useAuthStore).mockImplementation(
    ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
      selector({ role: 'admin', user: { username: 'testadmin' } })) as never,
  )
})

// =====================================================================
// Toggle fires mutation immediately on change (autosave)
// =====================================================================

describe('AuditLogSection — autosave on toggle', () => {
  it('toggling off→on immediately calls updateAuditLog with enabled: true (no Save button needed)', async () => {
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

    await waitFor(() => {
      expect(updateAuditLog).toHaveBeenCalledWith(true)
    })
  })

  it('no Save button is rendered', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)

    renderSection()

    await screen.findByRole('switch')
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('shows SaveStatus "Saving…" then "Saved" in happy path', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve({ saved: true, requires_restart: false, applied_enabled: true }), 50))
    )

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(screen.getByText(/saving/i)).toBeInTheDocument()
    })

    await waitFor(() => {
      expect(screen.getByText(/saved/i)).toBeInTheDocument()
    })
  })

  it('shows restart required badge after save when server returns requires_restart: true', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_enabled: true,
    })

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(screen.getByText(/restart required/i)).toBeInTheDocument()
    })
  })

  it('shows error toast when save fails', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockRejectedValue(new Error('save failed'))

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ variant: 'error' })
      )
    })
  })

  it('shows SaveStatus "Save failed" indicator on error', async () => {
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockRejectedValue(new Error('network error'))

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(screen.getByText(/save failed/i)).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Non-admin: no Save button, input disabled
// =====================================================================

describe('AuditLogSection — non-admin', () => {
  it('does not render a Save button for non-admin users', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
        selector({ role: 'user', user: { username: 'testuser' } })) as never,
    )
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: true } as never)

    renderSection()

    await screen.findByRole('switch')
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('disables the checkbox for non-admin users', async () => {
    vi.mocked(useAuthStore).mockImplementation(
      ((selector: (s: { role?: string; user?: { username: string } }) => unknown) =>
        selector({ role: 'user', user: { username: 'testuser' } })) as never,
    )
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: true } as never)

    renderSection()

    const checkbox = await screen.findByRole('switch')
    expect(checkbox).toBeDisabled()
  })
})

// =====================================================================
// SaveStatus auto-revert to idle after 2s
// =====================================================================

describe('AuditLogSection — SaveStatus auto-revert', () => {
  it('SaveStatus disappears after 2 seconds following a successful save', async () => {
    // Use real timers for the mutation flow; saved status sets a 2s timeout
    vi.mocked(fetchAuditLogToggle).mockResolvedValue({ enabled: false } as never)
    vi.mocked(updateAuditLog).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_enabled: true,
    })

    renderSection()

    const checkbox = await screen.findByRole('switch')
    fireEvent.click(checkbox)

    // Wait for mutation to complete and "Saved" to appear
    await waitFor(() => {
      expect(screen.getByText(/saved/i)).toBeInTheDocument()
    })

    // Advance time by 2.1s using real timers — wait for "Saved" to disappear
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 2100))
    })

    expect(screen.queryByText(/saved/i)).not.toBeInTheDocument()
  })
})
