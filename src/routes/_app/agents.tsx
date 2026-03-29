import { createFileRoute } from '@tanstack/react-router'
import { Robot } from '@phosphor-icons/react'

// US-4: Agents empty state — Robot icon, Outfit Bold title
function AgentsScreen() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[70vh] gap-6 p-8 text-center">
      <Robot
        size={64}
        weight="thin"
        className="text-[var(--color-secondary)] opacity-40"
      />
      <div>
        <h1 className="font-headline text-3xl font-bold text-[var(--color-secondary)] mb-2">
          Agents
        </h1>
        <p className="text-[var(--color-muted)] text-base max-w-sm">
          Browse, create, and configure your AI agents here. Each agent has its
          own persona, tools, and session history.
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/agents')({
  component: AgentsScreen,
})
