import { useEffect, useCallback } from 'react'
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router'
import { List, SignOut } from '@phosphor-icons/react'
import { Sidebar } from './Sidebar'
import { useSidebarStore } from '@/store/sidebar'
import { SessionBar } from '@/components/chat/SessionBar'
import { ToastContainer } from '@/components/ui/toast-container'
import { OmnipusRuntimeProvider } from '@/components/chat/OmnipusRuntimeProvider'
import { ErrorBoundary } from '@/components/ui/error-boundary'
import { queryClient } from '@/lib/queryClient'
import { fetchTasks, fetchAgents } from '@/lib/api'
import { useAuthStore } from '@/store/auth'
import { useConnectionStore } from '@/store/connection'

// US-4: Application shell — hamburger + sidebar + main content area
export function AppShell() {
  const { toggle } = useSidebarStore()
  const location = useLocation()
  const navigate = useNavigate()
  const connectionError = useConnectionStore((s) => s.connectionError)
  const reconnect = useConnectionStore((s) => s.reconnect)

  const handleLogout = useCallback(() => {
    useAuthStore.getState().clearAuth()
    navigate({ to: '/login' })
  }, [navigate])

  // Prefetch command center data on app load so it's cached when the user navigates there
  useEffect(() => {
    queryClient.prefetchQuery({ queryKey: ['tasks'], queryFn: () => fetchTasks(), staleTime: 30_000 })
    queryClient.prefetchQuery({ queryKey: ['agents'], queryFn: fetchAgents, staleTime: 30_000 })
  }, [])

  // Show SessionBar only on the chat screen (root route)
  const isChatScreen = location.pathname === '/' || location.pathname === ''

  return (
    <div className="flex h-dvh w-full overflow-hidden bg-[var(--color-primary)]">
      {/* Sidebar renders in both pinned (flex child) and overlay (fixed) modes */}
      <Sidebar />

      {/* Main content area — shrinks when sidebar is pinned */}
      <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
        {/* OmnipusRuntimeProvider: AssistantUI context + WebSocket connection for entire app */}
        <OmnipusRuntimeProvider>
          {/* Top bar with hamburger + session bar slot */}
          <header className="flex items-center gap-3 px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface-1)] flex-shrink-0">
            {/* US-5: Hamburger — always visible, toggles sidebar open/close */}
            <button
              id="sidebar-hamburger"
              onClick={toggle}
              aria-label="Toggle navigation sidebar"
              aria-expanded={undefined /* updated by Sidebar via aria pattern */}
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

            {/* Logout button */}
            <button
              onClick={handleLogout}
              title="Sign out"
              aria-label="Sign out"
              className="flex items-center justify-center h-8 w-8 rounded-md text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors flex-shrink-0"
            >
              <SignOut size={20} />
            </button>
          </header>

          {/* Global connection error banner — visible on every screen */}
          {connectionError && (
            <div className="flex items-center justify-between gap-2 px-4 py-2 bg-[var(--color-error)]/10 border-b border-[var(--color-error)]/20 text-xs text-[var(--color-error)] shrink-0">
              <span>{connectionError}</span>
              <button
                type="button"
                onClick={reconnect}
                className="px-2 py-1 rounded text-xs hover:bg-[var(--color-error)]/20 transition-colors"
              >
                Retry
              </button>
            </div>
          )}

          {/* Screen content — relative so children can use absolute inset-0 for bounded scrolling */}
          <main className="flex-1 relative min-h-0 overflow-hidden">
            <ErrorBoundary>
              <Outlet />
            </ErrorBoundary>
          </main>
        </OmnipusRuntimeProvider>
      </div>

      {/* Global toast notifications */}
      <ToastContainer />
    </div>
  )
}
