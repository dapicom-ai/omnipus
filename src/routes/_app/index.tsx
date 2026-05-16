import { useEffect } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { ChatScreen } from '@/components/chat/ChatScreen'
import { useSessionStore } from '@/store/session'

// Wrapper that starts a fresh session whenever the root chat route mounts.
// This ensures that navigating to '/' (e.g. via page.goto('/') in tests, or
// the user clicking the logo) always presents an empty composer rather than
// resuming the previous session.  Sessions.$sessionId route sets its own
// activeSessionId, so this only fires on the unparameterised root route.
function RootChatScreen() {
  const startNewSession = useSessionStore((s) => s.startNewSession)

  useEffect(() => {
    startNewSession()
    // Run once on mount — startNewSession is a stable Zustand reference.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return <ChatScreen />
}

export const Route = createFileRoute('/_app/')({
  component: RootChatScreen,
})
