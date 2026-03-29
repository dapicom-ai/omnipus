import { createFileRoute } from '@tanstack/react-router'
import { ChatCircle } from '@phosphor-icons/react'
import OmnipusAvatar from '@/assets/logo/omnipus-avatar.svg?url'

// US-4: Chat — default home route with mascot welcome empty state
function ChatScreen() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[70vh] gap-8 p-8">
      <div className="flex flex-col items-center gap-6 text-center max-w-md">
        <img
          src={OmnipusAvatar}
          alt="Omnipus mascot"
          className="h-24 w-24 drop-shadow-lg"
        />
        <div>
          <h1 className="font-headline text-3xl font-bold text-[var(--color-secondary)] mb-2">
            Welcome to Omnipus
          </h1>
          <p className="text-[var(--color-muted)] text-base">
            Your agents are standing by.
          </p>
        </div>
        <ChatCircle
          size={64}
          weight="thin"
          className="text-[var(--color-border)] mt-2"
        />
        <p className="text-sm text-[var(--color-muted)] max-w-xs">
          Start a conversation with your agents. Chat-first, inline-everything —
          your autonomous command center is ready.
        </p>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/')({
  component: ChatScreen,
})
