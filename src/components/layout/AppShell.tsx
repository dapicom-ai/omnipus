import { Outlet, useLocation } from '@tanstack/react-router'
import { List } from '@phosphor-icons/react'
import { Sidebar } from './Sidebar'
import { useSidebarStore } from '@/store/sidebar'
import { SessionBar } from '@/components/chat/SessionBar'
import { ToastContainer } from '@/components/ui/toast-container'

// US-4: Application shell — hamburger + sidebar + main content area
export function AppShell() {
  const { toggle } = useSidebarStore()
  const location = useLocation()

  // Show SessionBar only on the chat screen (root route)
  const isChatScreen = location.pathname === '/' || location.pathname === ''

  return (
    <div className="flex h-screen w-full overflow-hidden bg-[var(--color-primary)]">
      {/* Sidebar renders in both pinned (flex child) and overlay (fixed) modes */}
      <Sidebar />

      {/* Main content area — shrinks when sidebar is pinned */}
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
        {/* Top bar with hamburger + session bar slot */}
        <header className="flex items-center gap-3 px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface-1)] flex-shrink-0">
          {/* US-5: Hamburger — always visible, toggles sidebar open/close */}
          <button
            onClick={toggle}
            aria-label="Toggle sidebar"
            className="flex items-center justify-center h-8 w-8 rounded-md text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors flex-shrink-0"
          >
            <List size={20} />
          </button>

          {/* Session bar — wired only on chat screen */}
          <div className="flex-1 min-w-0">
            {isChatScreen ? (
              <SessionBar />
            ) : (
              <div id="session-bar-slot" className="flex-1" />
            )}
          </div>
        </header>

        {/* Screen content */}
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      </div>

      {/* Global toast notifications */}
      <ToastContainer />
    </div>
  )
}
