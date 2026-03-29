import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { useSidebarStore } from '@/store/sidebar'

// Mock TanStack Router — Sidebar uses useLocation
vi.mock('@tanstack/react-router', () => ({
  useLocation: () => ({ pathname: '/' }),
  Link: ({ children, to, onClick, className }: {
    children: React.ReactNode
    to: string
    onClick?: () => void
    className?: string
  }) => (
    <a href={to} onClick={onClick} className={className}>
      {children}
    </a>
  ),
}))

// Mock SVG URL import
vi.mock('@/assets/logo/omnipus-avatar.svg?url', () => ({ default: '/mock-avatar.svg' }))

// Mock Framer Motion — AnimatePresence/motion renders children without animation
vi.mock('framer-motion', () => ({
  motion: {
    aside: ({ children, className, style, ...rest }: React.HTMLAttributes<HTMLElement>) => (
      <aside className={className} style={style} {...rest}>{children}</aside>
    ),
    div: ({ children, className, onClick, ...rest }: React.HTMLAttributes<HTMLDivElement>) => (
      <div className={className} onClick={onClick} {...rest}>{children}</div>
    ),
  },
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

import { Sidebar } from './Sidebar'

beforeEach(() => {
  act(() => {
    useSidebarStore.setState({ isOpen: false, isPinned: false })
  })
})

// test_sidebar_overlay_rendering
// Traces to: wave0-brand-design-spec.md Scenario: Sidebar opens as overlay (US-5 AC2, FR-011)
describe('Sidebar — overlay rendering when open', () => {
  it('renders navigation items when sidebar is open', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: false }) })
    render(<Sidebar />)

    // All 5 nav item labels must be present.
    expect(screen.getByText('Chat')).toBeTruthy()
    expect(screen.getByText('Command Center')).toBeTruthy()
    expect(screen.getByText('Agents')).toBeTruthy()
    expect(screen.getByText('Skills & Tools')).toBeTruthy()
    expect(screen.getByText('Settings')).toBeTruthy()
  })

  it('shows "Omnipus" brand name in sidebar', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: false }) })
    render(<Sidebar />)
    expect(screen.getByText('Omnipus')).toBeTruthy()
  })

  it('renders nothing visible when sidebar is closed', () => {
    // Sidebar closed + unpinned: overlay motion aside should not render
    act(() => { useSidebarStore.setState({ isOpen: false, isPinned: false }) })
    render(<Sidebar />)
    // Nav labels should not be in the DOM when closed.
    expect(screen.queryByText('Chat')).toBeNull()
    expect(screen.queryByText('Agents')).toBeNull()
  })
})

// test_sidebar_pin_icon_hidden_mobile
// Traces to: wave0-brand-design-spec.md Scenario: Pin icon hidden on phone breakpoint (US-5 AC7, FR-015)
describe('Sidebar — pin icon visibility on mobile', () => {
  it('pin button has "hidden md:flex" class to hide on phones (<768px)', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: false }) })
    const { container } = render(<Sidebar />)

    // The pin button uses Tailwind's `hidden md:flex` which hides it below 768px.
    // jsdom cannot evaluate CSS media queries, so we verify the class is correct.
    const pinButton = container.querySelector('button[title="Pin sidebar"]')
    expect(pinButton).not.toBeNull()
    expect(pinButton!.className).toContain('hidden')
    expect(pinButton!.className).toContain('md:flex')
  })

  it('shows PushPinSlash icon title when pinned', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: true }) })
    const { container } = render(<Sidebar />)

    const pinButton = container.querySelector('button[title="Unpin sidebar"]')
    expect(pinButton).not.toBeNull()
    expect(pinButton!.getAttribute('title')).toBe('Unpin sidebar')
  })

  it('shows PushPin icon title when unpinned', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: false }) })
    const { container } = render(<Sidebar />)

    const pinButton = container.querySelector('button[title="Pin sidebar"]')
    expect(pinButton).not.toBeNull()
  })
})

// Traces to: wave0-brand-design-spec.md Scenario: Pinned sidebar stays open on nav (US-5 AC6, FR-014)
describe('Sidebar — pinned mode rendering', () => {
  it('renders pinned sidebar as aside element', () => {
    act(() => { useSidebarStore.setState({ isOpen: true, isPinned: true }) })
    const { container } = render(<Sidebar />)

    // Pinned mode renders an aside with 'hidden md:flex' class
    const pinnedAside = container.querySelector('aside.hidden.md\\:flex')
    expect(pinnedAside).not.toBeNull()
  })
})
