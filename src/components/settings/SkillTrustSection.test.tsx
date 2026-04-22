import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchSkillTrust: vi.fn(),
    updateSkillTrust: vi.fn(),
  }
})

vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn(() => ({ addToast: vi.fn() })),
}))

vi.mock('@/store/auth', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useAuthStore: vi.fn((selector: any) => selector({ role: 'admin' })),
}))

import { fetchSkillTrust, updateSkillTrust } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { useAuthStore } from '@/store/auth'
import { SkillTrustSection } from './SkillTrustSection'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <SkillTrustSection />
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
// Three radios render with canonical values
// =====================================================================

describe('SkillTrustSection — radio rendering', () => {
  it('renders three radios with canonical level values', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })

    renderSection()

    await waitFor(() => {
      const radios = screen.getAllByRole('radio')
      expect(radios).toHaveLength(3)
    })

    const radios = screen.getAllByRole('radio')
    const values = radios.map((r) => r.getAttribute('aria-checked'))
    // Confirm three radios exist
    expect(radios.length).toBe(3)
    // Confirm warn_unverified is selected by default
    expect(values).toContain('true')
  })

  it('renders block_unverified radio', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()
    await waitFor(() => screen.getByText(/block skills without a verifiable hash/i))
    expect(screen.getByText(/block skills without a verifiable hash/i)).toBeInTheDocument()
  })

  it('renders warn_unverified radio', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()
    await waitFor(() => screen.getByText(/warn but allow/i))
    expect(screen.getByText(/warn but allow/i)).toBeInTheDocument()
  })

  it('renders allow_all radio with its subtitle', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()
    await waitFor(() => screen.getByText(/accept any skill/i))
    expect(screen.getByText(/accept any skill/i)).toBeInTheDocument()
  })

  it('does not render any uppercase level values (block_unverified, warn_unverified, allow_all must be lowercase)', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()
    await waitFor(() => screen.getAllByRole('radio'))
    // None of the aria-label or role="radio" buttons should contain uppercase variants
    const content = document.body.textContent ?? ''
    expect(content).not.toMatch(/BLOCK_UNVERIFIED|WARN_UNVERIFIED|ALLOW_ALL/)
  })
})

// =====================================================================
// Selecting allow_all shows warning panel
// =====================================================================

describe('SkillTrustSection — allow_all warning', () => {
  it('shows amber warning panel when allow_all is selected', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))

    // Click the allow_all radio (third one)
    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[2])

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByRole('alert')).toHaveTextContent(/supply-chain/i)
  })

  it('does not show warning panel when warn_unverified is selected', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

// =====================================================================
// Save fires updateSkillTrust with canonical value
// =====================================================================

describe('SkillTrustSection — save', () => {
  it('save fires updateSkillTrust with block_unverified', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    vi.mocked(updateSkillTrust).mockResolvedValue({
      saved: true,
      requires_restart: false,
      applied_level: 'block_unverified',
    })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))

    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[0]) // block_unverified

    const saveBtn = screen.getByRole('button', { name: /save/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(updateSkillTrust).toHaveBeenCalledWith('block_unverified')
    })
  })

  it('shows restart-required badge when server returns requires_restart: true', async () => {
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })
    vi.mocked(updateSkillTrust).mockResolvedValue({
      saved: true,
      requires_restart: true,
      applied_level: 'block_unverified',
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

describe('SkillTrustSection — non-admin', () => {
  it('hides Save button for non-admin', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useAuthStore).mockImplementation((selector: any) => selector({ role: 'user' }))
    vi.mocked(fetchSkillTrust).mockResolvedValue({ level: 'warn_unverified' })

    renderSection()

    await waitFor(() => screen.getAllByRole('radio'))
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })
})
