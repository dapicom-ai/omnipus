import { create } from 'zustand'
import type { Session } from '@/lib/api'
import { useConnectionStore } from '@/store/connection'

interface SessionStore {
  activeSessionId: string | null
  activeAgentId: string | null
  /** The type of the currently active agent ('core' | 'custom' | null).
   *  Set by setActiveSession so all callers stay in sync without manual tracking. */
  activeAgentType: 'core' | 'custom' | null
  setActiveSession: (
    sessionId: string | null,
    agentId?: string | null,
    agentType?: 'core' | 'custom' | null
  ) => void

  // Attached session context — tracks when viewing a task/channel session
  attachedSessionType: 'chat' | 'task' | 'channel' | null
  attachedTaskTitle: string | null
  attachToSession: (
    sessionId: string,
    type: Session['type'],
    title?: string,
    agentId?: string
  ) => void
}

// Breaks the chat.ts ↔ session.ts circular import: chat.ts imports this module,
// then registers resetSession so session.ts never imports chat.ts directly.
// This avoids any ES module circular-init ordering issues entirely.
let _chatResetSession: (() => void) | null = null

/** Called once by chat.ts after it creates useChatStore. */
export function registerChatResetSession(fn: () => void): void {
  _chatResetSession = fn
}

function resetChatSession(): void {
  if (_chatResetSession) {
    _chatResetSession()
  } else {
    console.warn('[session] resetChatSession called before chat store registered — session state may be stale')
  }
}

export const useSessionStore = create<SessionStore>((set, get) => ({
  activeSessionId: null,
  activeAgentId: null,
  activeAgentType: null,

  setActiveSession: (sessionId, agentId, agentType) => {
    resetChatSession()

    set({
      activeSessionId: sessionId,
      activeAgentId: agentId ?? get().activeAgentId,
      activeAgentType: agentType ?? get().activeAgentType,
      attachedSessionType: null,
      attachedTaskTitle: null,
    })
  },

  attachedSessionType: null,
  attachedTaskTitle: null,

  attachToSession: (sessionId, type, title, agentId) => {
    const { connection } = useConnectionStore.getState()

    resetChatSession()

    set({
      activeSessionId: sessionId,
      attachedSessionType: type,
      attachedTaskTitle: title ?? null,
      activeAgentId: agentId ?? get().activeAgentId,
    })

    if (connection) {
      const sent = connection.send({ type: 'attach_session', session_id: sessionId })
      if (!sent) {
        useConnectionStore.getState().setConnectionError(
          'Could not attach to session — connection dropped. Please reconnect and try again.'
        )
      }
    } else {
      console.warn('[session] attachToSession: no connection — attach_session not sent')
    }
  },
}))
