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
    } catch {
      // API unreachable — allow through to app (the chat screen will show
      // a connection error via the WebSocket state). Redirecting to onboarding
      // on every API failure would trap users in a loop.
      return
    }
    if (state && !state.onboarding_complete) {
      throw redirect({ to: '/onboarding' })
    }
  },
  component: AppShell,
})
