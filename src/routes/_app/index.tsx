import { createFileRoute } from '@tanstack/react-router'
import { ChatScreen } from '@/components/chat/ChatScreen'

export const Route = createFileRoute('/_app/')({
  component: ChatScreen,
})
