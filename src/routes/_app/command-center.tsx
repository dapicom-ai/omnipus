import { createFileRoute } from '@tanstack/react-router'
import { Gauge } from '@phosphor-icons/react'

// US-4: Command Center empty state — Gauge icon, Outfit Bold title
function CommandCenterScreen() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[70vh] gap-6 p-8 text-center">
      <Gauge
        size={64}
        weight="thin"
        className="text-[var(--color-secondary)] opacity-40"
      />
      <div>
        <h1 className="font-headline text-3xl font-bold text-[var(--color-secondary)] mb-2">
          Command Center
        </h1>
        <p className="text-[var(--color-muted)] text-base max-w-sm">
          Task boards, active agent runs, and system status will appear here.
          GTD-style kanban with Inbox, Next, Active, Waiting, and Done lanes.
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/command-center')({
  component: CommandCenterScreen,
})
