import { createFileRoute, redirect } from '@tanstack/react-router'
import { AppShell } from '@/components/layout/AppShell'
import { fetchAppState } from '@/lib/api'

// Pathless layout route — wraps all app screens in AppShell
// Landing page (/landing) is a sibling, NOT nested here, so it renders without the shell
// /onboarding is also a sibling — no AppShell, no beforeLoad
export const Route = createFileRoute('/_app')({
  beforeLoad: async () => {
    let state: { onboarding_complete: boolean }
    try {
      state = await fetchAppState()
    } catch (err) {
      // Only suppress genuine network errors (API unreachable). For other errors
      // (4xx/5xx from the API) let them propagate so they are visible.
      if (err instanceof TypeError && err.message.toLowerCase().includes('fetch')) {
        // API unreachable — allow through to app (the chat screen will show
        // a connection error via the WebSocket state). Redirecting to onboarding
        // on every network failure would trap users in a loop.
        return
      }
      throw err
    }
    if (state && !state.onboarding_complete) {
      throw redirect({ to: '/onboarding' })
    }
  },
  component: AppShell,
})
