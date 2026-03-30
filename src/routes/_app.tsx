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
      // Suppress network-class errors (API unreachable, aborted requests, all
      // TypeErrors from fetch). For application errors (4xx/5xx) let them
      // propagate so they are visible.
      // TypeError covers: failed to fetch, network error, CORS failure
      // DOMException covers: AbortError from request cancellation
      if (err instanceof TypeError || err instanceof DOMException) {
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
