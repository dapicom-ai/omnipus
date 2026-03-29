import { createFileRoute } from '@tanstack/react-router'
import { PuzzlePiece } from '@phosphor-icons/react'

// US-4: Skills & Tools empty state — Puzzle icon, Outfit Bold title
function SkillsScreen() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[70vh] gap-6 p-8 text-center">
      <PuzzlePiece
        size={64}
        weight="thin"
        className="text-[var(--color-secondary)] opacity-40"
      />
      <div>
        <h1 className="font-headline text-3xl font-bold text-[var(--color-secondary)] mb-2">
          Skills &amp; Tools
        </h1>
        <p className="text-[var(--color-muted)] text-base max-w-sm">
          Discover, install, and manage skills and MCP tool integrations. Extend
          your agents with browser automation, APIs, and custom capabilities.
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/skills')({
  component: SkillsScreen,
})
