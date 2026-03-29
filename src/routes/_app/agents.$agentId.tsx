import { createFileRoute } from '@tanstack/react-router'
import { AgentProfile } from '@/components/agents/AgentProfile'

function AgentProfileRoute() {
  const { agentId } = Route.useParams()
  return <AgentProfile agentId={agentId} />
}

export const Route = createFileRoute('/_app/agents/$agentId')({
  component: AgentProfileRoute,
})
