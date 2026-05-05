import { createFileRoute } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChatScreen } from '@/components/chat/ChatScreen'
import { fetchSessionDetail } from '@/lib/api'
import { useSessionStore } from '@/store/session'

function SessionRoute() {
  const { sessionId } = Route.useParams()
  const setActiveSession = useSessionStore((s) => s.setActiveSession)

  const { data: detail } = useQuery({
    queryKey: ['session-detail', sessionId],
    queryFn: () => fetchSessionDetail(sessionId),
    enabled: !!sessionId,
    retry: false,
  })

  // Activate the session in the store so the WebSocket attaches to it
  useEffect(() => {
    if (detail?.session) {
      setActiveSession(detail.session.id, detail.session.agent_id, null)
    }
  }, [detail?.session?.id, detail?.session?.agent_id, setActiveSession])

  return <ChatScreen agentRemoved={detail?.agent_removed ?? false} />
}

export const Route = createFileRoute('/_app/sessions/$sessionId')({
  component: SessionRoute,
})
