import { describe, it, expect, vi, beforeAll } from 'vitest'
import { render, screen } from '@testing-library/react'

// Import screen components directly (without router wrapper).
// This tests the components in isolation — rendering and content.

// We import the inner function components by re-exporting from a test wrapper.
// Since TanStack Router's createFileRoute wraps the component, we test the
// rendered component by importing and calling it.

// Helper: render a route component and check heading text.
function renderAndCheckHeading(Component: () => JSX.Element, expectedTitle: string) {
  render(<Component />)
  const heading = screen.queryByRole('heading', { level: 1 })
  expect(heading).not.toBeNull()
  expect(heading!.textContent).toBe(expectedTitle)
}

// ---------------------------------------------------------------------------
// We need to reach the inner components. The Route.component is accessible
// after import via the Route export.
// ---------------------------------------------------------------------------

// test_screen_empty_states (integration)
// Traces to: wave0-brand-design-spec.md Scenario: Each non-chat screen renders empty state (US-4 AC2, FR-009)

describe('Command Center screen — empty state', () => {
  let CommandCenterScreen: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/_app/command-center')
    CommandCenterScreen = mod.Route.options.component as () => JSX.Element
  })

  it('renders "Command Center" as h1 heading', () => {
    render(<CommandCenterScreen />)
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Command Center')
  })

  it('h1 has font-headline class (Outfit Bold)', () => {
    const { container } = render(<CommandCenterScreen />)
    const h1 = container.querySelector('h1')
    expect(h1?.className).toContain('font-headline')
  })
})

describe('Agents screen — empty state', () => {
  let AgentsScreen: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/_app/agents')
    AgentsScreen = mod.Route.options.component as () => JSX.Element
  })

  it('renders "Agents" as h1 heading', () => {
    render(<AgentsScreen />)
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Agents')
  })
})

describe('Skills screen — empty state', () => {
  let SkillsScreen: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/_app/skills')
    SkillsScreen = mod.Route.options.component as () => JSX.Element
  })

  it('renders "Skills & Tools" as h1 heading', () => {
    render(<SkillsScreen />)
    const h1 = screen.getByRole('heading', { level: 1 })
    expect(h1.textContent).toContain('Skills')
  })
})

describe('Settings screen — empty state', () => {
  let SettingsScreen: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/_app/settings')
    SettingsScreen = mod.Route.options.component as () => JSX.Element
  })

  it('renders "Settings" as h1 heading', () => {
    render(<SettingsScreen />)
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Settings')
  })
})

// test_chat_empty_state (integration)
// Traces to: wave0-brand-design-spec.md Scenario: Chat empty state shows mascot (US-4 AC4, FR-010)
describe('Chat (index) screen — empty state', () => {
  let ChatScreen: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/_app/index')
    ChatScreen = mod.Route.options.component as () => JSX.Element
  })

  it('renders "Welcome to Omnipus" heading', () => {
    render(<ChatScreen />)
    expect(screen.getByText('Welcome to Omnipus')).toBeTruthy()
  })

  it('renders "Your agents are standing by" subtitle', () => {
    render(<ChatScreen />)
    expect(screen.getByText(/Your agents are standing by/i)).toBeTruthy()
  })

  it('renders mascot image with alt text', () => {
    render(<ChatScreen />)
    const img = screen.queryByRole('img', { name: /omnipus mascot/i })
    expect(img).not.toBeNull()
  })
})

// test_404_page (integration)
// Traces to: wave0-brand-design-spec.md Scenario: Unknown route shows branded 404 (FR-008)
// The NotFoundPage is defined in __root.tsx using TanStack Router's Link component.
// We mock TanStack Router to test it in isolation.
vi.mock('@tanstack/react-router', () => ({
  createRootRoute: (opts: Record<string, unknown>) => ({ options: opts }),
  createFileRoute: () => (opts: Record<string, unknown>) => ({ options: opts }),
  Outlet: () => null,
  Link: ({ children, to, className }: { children: React.ReactNode; to: string; className?: string }) => (
    <a href={to} className={className}>{children}</a>
  ),
  useLocation: () => ({ pathname: '/' }),
}))

vi.mock('@/assets/logo/omnipus-avatar.svg?url', () => ({ default: '/mock-avatar.svg' }))

describe('404 NotFound page — branded empty state', () => {
  let NotFoundPage: () => JSX.Element

  beforeAll(async () => {
    const mod = await import('@/routes/__root')
    NotFoundPage = mod.Route.options.notFoundComponent as () => JSX.Element
  })

  it('renders 404 heading', () => {
    render(<NotFoundPage />)
    expect(screen.getByText('404')).toBeTruthy()
  })

  it('renders "drifted into the deep" copy', () => {
    render(<NotFoundPage />)
    expect(screen.getByText(/drifted into the deep/i)).toBeTruthy()
  })

  it('renders a link back to Chat (/)', () => {
    render(<NotFoundPage />)
    const link = screen.queryByText(/Back to Chat/i)
    expect(link).not.toBeNull()
  })
})
