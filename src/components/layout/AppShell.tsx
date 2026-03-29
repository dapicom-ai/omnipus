import { Outlet } from '@tanstack/react-router'
import { List } from '@phosphor-icons/react'
import { Sidebar } from './Sidebar'
import { useSidebarStore } from '@/store/sidebar'

// US-4: Application shell — hamburger + sidebar + main content area
export function AppShell() {
  const { toggle } = useSidebarStore()

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--color-primary)]">
      {/* Sidebar renders in both pinned (flex child) and overlay (fixed) modes */}
      <Sidebar />

      {/* Main content area — shrinks when sidebar is pinned */}
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
        {/* Top bar with hamburger */}
        <header className="flex items-center gap-3 px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface-1)] flex-shrink-0">
          {/* US-5: Hamburger — always visible, toggles sidebar open/close */}
          <button
            onClick={toggle}
            aria-label="Toggle sidebar"
            className="flex items-center justify-center h-8 w-8 rounded-md text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors flex-shrink-0"
          >
            <List size={20} />
          </button>

          {/* Slot for screen-level content (session bar, breadcrumb, etc.) */}
          <div className="flex-1 min-w-0" id="session-bar-slot" />
        </header>

        {/* Screen content */}
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
