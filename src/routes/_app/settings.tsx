import { createFileRoute } from '@tanstack/react-router'
import { Gear } from '@phosphor-icons/react'

// US-4: Settings empty state — Gear icon, Outfit Bold title
function SettingsScreen() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[70vh] gap-6 p-8 text-center">
      <Gear
        size={64}
        weight="thin"
        className="text-[var(--color-secondary)] opacity-40"
      />
      <div>
        <h1 className="font-headline text-3xl font-bold text-[var(--color-secondary)] mb-2">
          Settings
        </h1>
        <p className="text-[var(--color-muted)] text-base max-w-sm">
          Configure Omnipus — gateway connection, credentials, appearance, agent
          defaults, and privacy preferences.
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/settings')({
  component: SettingsScreen,
})
