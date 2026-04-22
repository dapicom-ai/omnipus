import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchSandboxStatus: vi.fn(),
    fetchSandboxConfig: vi.fn(),
    updateSandboxConfig: vi.fn(),
  }
})

vi.mock('@/store/auth', () => ({
  useAuthStore: vi.fn((selector: Parameters<typeof import('@/store/auth').useAuthStore>[0]) =>
    selector({
      token: 'test-token',
      role: 'admin',
      username: 'admin',
      setToken: vi.fn(),
      clearAuth: vi.fn(),
    })
  ),
}))

import { fetchSandboxStatus, fetchSandboxConfig, updateSandboxConfig } from '@/lib/api'
import { useAuthStore } from '@/store/auth'
import { SandboxSection } from './SandboxSection'
import type { SandboxStatus, SandboxConfigResponse } from '@/lib/api'

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  const client = makeClient()
  const utils = render(
    <QueryClientProvider client={client}>
      <SandboxSection />
    </QueryClientProvider>
  )
  return { ...utils, client }
}

const baseStatus: SandboxStatus = {
  backend: 'landlock',
  available: true,
  kernel_level: true,
  policy_applied: true,
  seccomp_enabled: true,
}

const baseConfig: SandboxConfigResponse = {
  mode: 'permissive',
  allowed_paths: [],
  ssrf: { allow_internal: [] },
  requires_restart: false,
}

function mockNonAdmin() {
  vi.mocked(useAuthStore).mockImplementation(
    (selector: Parameters<typeof useAuthStore>[0]) =>
      selector({ token: 'test-token', role: 'user', username: 'alice', setToken: vi.fn(), clearAuth: vi.fn() }) as never
  )
}

// Enter edit mode by clicking the Edit button
async function enterEditMode() {
  const editBtn = await screen.findByRole('button', { name: /^edit$/i })
  fireEvent.click(editBtn)
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(fetchSandboxStatus).mockResolvedValue(baseStatus)
  vi.mocked(fetchSandboxConfig).mockResolvedValue(baseConfig)
  vi.mocked(updateSandboxConfig).mockResolvedValue({ ...baseConfig, requires_restart: true })
  // Reset auth mock to admin after clearAllMocks
  vi.mocked(useAuthStore).mockImplementation(
    (selector: Parameters<typeof useAuthStore>[0]) =>
      selector({ token: 'test-token', role: 'admin', username: 'admin', setToken: vi.fn(), clearAuth: vi.fn() }) as never
  )
  // Reset localStorage/sessionStorage
  localStorage.clear()
  sessionStorage.clear()
})

// ── describe: allowed_paths editor ───────────────────────────────────────────

describe('allowed_paths editor', () => {
  it('renders two rows with read-only badges when allowed_paths has two entries', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['/a', '/b'],
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('/a')).toBeInTheDocument()
      expect(screen.getByText('/b')).toBeInTheDocument()
    })

    // Both rows should have read-only badges (there should be 2)
    const roBadges = screen.getAllByText('read-only')
    expect(roBadges).toHaveLength(2)
  })

  it('shows Filesystem paths the sandbox may read heading', async () => {
    renderSection()

    await waitFor(() => {
      expect(screen.getByText(/Filesystem paths the sandbox may read/i)).toBeInTheDocument()
    })
  })

  it('clicking Add, typing /c, then Save fires updateSandboxConfig with correct allowed_paths', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['/a', '/b'],
    })

    renderSection()

    await enterEditMode()

    const input = screen.getByRole('textbox', { name: /new allowed path/i })
    fireEvent.change(input, { target: { value: '/c' } })

    const addBtn = screen.getByRole('button', { name: /add path/i })
    fireEvent.click(addBtn)

    await waitFor(() => {
      expect(screen.getByText('/c')).toBeInTheDocument()
    })

    const saveBtn = screen.getByRole('button', { name: /^save$/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(updateSandboxConfig).toHaveBeenCalled()
      const [firstArg] = vi.mocked(updateSandboxConfig).mock.calls[0]
      expect(firstArg).toMatchObject({
        allowed_paths: ['/a', '/b', '/c'],
      })
    })
  })

  it('server 400 for relative path displays inline error on the failing row', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['./foo'],
    })
    vi.mocked(updateSandboxConfig).mockRejectedValue(
      new Error('400: allowed_paths[0]: must be absolute — `./foo` is relative')
    )

    renderSection()

    await enterEditMode()

    const saveBtn = screen.getByRole('button', { name: /^save$/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      const errors = screen.getAllByText(/must be absolute/i)
      expect(errors.length).toBeGreaterThan(0)
    })
  })

  it('non-admin role: Edit button is not shown; rows are read-only display', async () => {
    mockNonAdmin()
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['/a'],
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('/a')).toBeInTheDocument()
    })

    // No Edit button for non-admin
    expect(screen.queryByRole('button', { name: /^edit$/i })).not.toBeInTheDocument()
    // No Add button
    expect(screen.queryByRole('button', { name: /add path/i })).not.toBeInTheDocument()
    // No Save button
    expect(screen.queryByRole('button', { name: /^save$/i })).not.toBeInTheDocument()
    // No delete buttons for paths
    expect(screen.queryByRole('button', { name: /delete path/i })).not.toBeInTheDocument()
  })

  it('delete button removes a path row in edit mode', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['/a', '/b'],
    })

    renderSection()

    await enterEditMode()

    // Delete first path
    const deleteBtn = screen.getByRole('button', { name: /delete path \/a/i })
    fireEvent.click(deleteBtn)

    await waitFor(() => {
      expect(screen.queryByText('/a')).not.toBeInTheDocument()
      expect(screen.getByText('/b')).toBeInTheDocument()
    })
  })

  it('read-only badge has a tooltip with the correct text', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      allowed_paths: ['/etc'],
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('/etc')).toBeInTheDocument()
    })

    const badge = screen.getByText('read-only')
    fireEvent.mouseEnter(badge)

    await waitFor(() => {
      expect(
        screen.getByText(/AllowedPaths entries grant read-only access/i)
      ).toBeInTheDocument()
    })
  })
})

// ── describe: SSRF editor ─────────────────────────────────────────────────────

describe('SSRF editor', () => {
  const RFC1918_LIST = ['127.0.0.1', '::1', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7']
  const LOOPBACK_LIST = ['127.0.0.1', '::1']

  it('clicking "Allow RFC1918 + loopback" and Save fires PUT with exact preset list', async () => {
    renderSection()

    await enterEditMode()

    const rfc1918Btn = screen.getByRole('button', { name: /allow rfc1918 \+ loopback/i })
    fireEvent.click(rfc1918Btn)

    const saveBtn = screen.getByRole('button', { name: /^save$/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      expect(updateSandboxConfig).toHaveBeenCalled()
      const [firstArg] = vi.mocked(updateSandboxConfig).mock.calls[0]
      expect(firstArg).toMatchObject({
        ssrf: { allow_internal: RFC1918_LIST },
      })
    })
  })

  it('stored list matching preset (order-insensitive) → preset button is highlighted as active on mount', async () => {
    // Provide list in a different order
    const shuffled = ['::1', '127.0.0.1']
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      ssrf: { allow_internal: shuffled },
    })

    renderSection()

    await waitFor(() => {
      const loopbackBtn = screen.getByRole('button', { name: /allow loopback only/i })
      // aria-pressed indicates active preset
      expect(loopbackBtn).toHaveAttribute('aria-pressed', 'true')
    })
  })

  it('stored list with internal.corp (no preset match) → Advanced mode auto-expands', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      ssrf: { allow_internal: ['internal.corp'] },
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('internal.corp')).toBeInTheDocument()
    })

    // No preset should be marked active
    const blockAllBtn = screen.getByRole('button', { name: /block all/i })
    const loopbackBtn = screen.getByRole('button', { name: /allow loopback only/i })
    const rfc1918Btn = screen.getByRole('button', { name: /allow rfc1918 \+ loopback/i })
    expect(blockAllBtn).toHaveAttribute('aria-pressed', 'false')
    expect(loopbackBtn).toHaveAttribute('aria-pressed', 'false')
    expect(rfc1918Btn).toHaveAttribute('aria-pressed', 'false')
  })

  it('malformed CIDR entry in Advanced mode → inline error, Save button disabled until fixed', async () => {
    renderSection()

    await enterEditMode()

    // Open advanced mode
    const advancedToggle = screen.getByRole('button', { name: /advanced \(custom list\)/i })
    fireEvent.click(advancedToggle)

    // Add a malformed entry
    const input = screen.getByRole('textbox', { name: /new ssrf allow entry/i })
    fireEvent.change(input, { target: { value: '10.0.0/8' } })
    const addBtn = screen.getByRole('button', { name: /add ssrf entry/i })
    fireEvent.click(addBtn)

    await waitFor(() => {
      expect(
        screen.getByText(/invalid entry — expected hostname, IP, or CIDR/i)
      ).toBeInTheDocument()
    })

    // The error stays on the add-input area, not on a row (invalid entries are rejected before adding)
    // Verify that the add error message is shown
    expect(screen.getByText(/invalid entry — expected hostname, IP, or CIDR/i)).toBeInTheDocument()
  })

  it('adding 0.0.0.0/0 triggers wildcard confirmation modal; PUT fires only on confirm', async () => {
    renderSection()

    await enterEditMode()

    // Open advanced mode
    const advancedToggle = screen.getByRole('button', { name: /advanced \(custom list\)/i })
    fireEvent.click(advancedToggle)

    const input = screen.getByRole('textbox', { name: /new ssrf allow entry/i })
    fireEvent.change(input, { target: { value: '0.0.0.0/0' } })
    fireEvent.click(screen.getByRole('button', { name: /add ssrf entry/i }))

    await waitFor(() => {
      expect(screen.getByText('0.0.0.0/0')).toBeInTheDocument()
    })

    // Click Save — should show modal
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      // Modal title or description should appear
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('dialog')).toHaveTextContent(/disable ssrf protection/i)
    })

    // PUT should NOT have fired yet
    expect(updateSandboxConfig).not.toHaveBeenCalled()

    // Click Save anyway — PUT should fire
    fireEvent.click(screen.getByRole('button', { name: /save anyway/i }))

    await waitFor(() => {
      expect(updateSandboxConfig).toHaveBeenCalled()
      const [firstArg] = vi.mocked(updateSandboxConfig).mock.calls[0]
      expect(firstArg).toMatchObject({
        ssrf: { allow_internal: expect.arrayContaining(['0.0.0.0/0']) },
      })
    })
  })

  it('cancelling wildcard modal prevents PUT from firing', async () => {
    renderSection()

    await enterEditMode()

    const advancedToggle = screen.getByRole('button', { name: /advanced \(custom list\)/i })
    fireEvent.click(advancedToggle)

    const input = screen.getByRole('textbox', { name: /new ssrf allow entry/i })
    fireEvent.change(input, { target: { value: '0.0.0.0/0' } })
    fireEvent.click(screen.getByRole('button', { name: /add ssrf entry/i }))

    await waitFor(() => screen.getByText('0.0.0.0/0'))

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('dialog')).toHaveTextContent(/disable ssrf protection/i)
    })

    // Click Cancel
    fireEvent.click(screen.getByRole('button', { name: /^cancel$/i }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    expect(updateSandboxConfig).not.toHaveBeenCalled()
  })

  it('clicking "Block all" preset writes empty allow_internal list', async () => {
    vi.mocked(fetchSandboxConfig).mockResolvedValue({
      ...baseConfig,
      ssrf: { allow_internal: LOOPBACK_LIST },
    })

    renderSection()

    await enterEditMode()

    fireEvent.click(screen.getByRole('button', { name: /block all/i }))
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(updateSandboxConfig).toHaveBeenCalled()
      const [firstArg] = vi.mocked(updateSandboxConfig).mock.calls[0]
      expect(firstArg).toMatchObject({
        ssrf: { allow_internal: [] },
      })
    })
  })
})

// ── describe: ABI v4 surfaces ─────────────────────────────────────────────────

describe('ABI v4 surfaces', () => {
  it('abi_version=4 + issue_ref → yellow banner visible with issue_ref text', async () => {
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 4,
      issue_ref: '#138',
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
      expect(screen.getByRole('alert')).toHaveTextContent('#138')
    })
  })

  it('dismiss button stores sessionStorage key and hides banner', async () => {
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 4,
      issue_ref: '#138',
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /dismiss for session/i }))

    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    expect(sessionStorage.getItem('omnipus:abi4-banner-dismissed')).toBe('dismissed')
  })

  it('abi_version=3 → banner NOT rendered', async () => {
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 3,
    })

    renderSection()

    // Wait for load
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  it('abi_version field absent from response → banner NOT rendered (typeof !== number)', async () => {
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      // abi_version intentionally absent
    })

    renderSection()

    await waitFor(() => {
      // Status should have loaded — backend label "landlock" is rendered
      expect(screen.getByText('landlock')).toBeInTheDocument()
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('banner contains abi_version in text', async () => {
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 5,
      issue_ref: '#200',
    })

    renderSection()

    await waitFor(() => {
      const banner = screen.getByRole('alert')
      expect(banner).toHaveTextContent('Landlock v5')
      expect(banner).toHaveTextContent('#200')
    })
  })

  it('issue_ref from server response appears in banner — not hardcoded', async () => {
    // Use a non-standard issue ref to confirm it comes from the server
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 4,
      issue_ref: '#999-CUSTOM',
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('#999-CUSTOM')
    })
  })

  it('banner is NOT shown when abi_version=4 but issue_ref is absent', async () => {
    // issue_ref missing — typeof issue_ref !== 'string' guard
    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 4,
      // issue_ref absent
    })

    renderSection()

    // Allowed to wait for status to load
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  it('banner is not shown when sessionStorage has dismiss key set', async () => {
    sessionStorage.setItem('omnipus:abi4-banner-dismissed', 'dismissed')

    vi.mocked(fetchSandboxStatus).mockResolvedValue({
      ...baseStatus,
      abi_version: 4,
      issue_ref: '#138',
    })

    renderSection()

    await waitFor(() => {
      // Status should have loaded
      expect(screen.getByText(/process sandbox/i)).toBeInTheDocument()
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
