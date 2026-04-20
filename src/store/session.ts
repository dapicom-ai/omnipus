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
  /** W3-8: proper store action for updating activeAgentType.
   *  Replaces direct useSessionStore.setState({ activeAgentType }) call-sites
   *  so future side-effects can be added here without touching callers. */
  setActiveAgentType: (type: 'core' | 'custom' | null) => void

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
// then registers resetSession and setReplaying so session.ts never imports chat.ts directly.
// This avoids any ES module circular-init ordering issues entirely.
let _chatResetSession: (() => void) | null = null
let _chatSetReplaying: ((value: boolean) => void) | null = null

/** Called once by chat.ts after it creates useChatStore. */
export function registerChatResetSession(fn: () => void): void {
  _chatResetSession = fn
}

/** Called once by chat.ts after it creates useChatStore (FR-I-014). */
export function registerChatSetReplaying(fn: (value: boolean) => void): void {
  _chatSetReplaying = fn
}

function resetChatSession(): void {
  if (_chatResetSession) {
    _chatResetSession()
  } else {
    console.warn('[session] resetChatSession called before chat store registered — session state may be stale')
  }
}

function setChatReplaying(value: boolean): void {
  if (_chatSetReplaying) {
    _chatSetReplaying(value)
  } else {
    console.warn('[session] setChatReplaying called before chat store registered — isReplaying not set')
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

  // W3-8: dedicated action so future side effects can be added without touching callers.
  setActiveAgentType: (type) => {
    set({ activeAgentType: type })
  },

  attachedSessionType: null,
  attachedTaskTitle: null,

  attachToSession: (sessionId, type, title, agentId) => {
    const { connection } = useConnectionStore.getState()

    // W1-11: send the WS frame BEFORE committing state. If send fails, leave
    // the previous session state intact so the UI doesn't show a phantom
    // attached session with no gateway replay in flight.
    if (connection) {
      const sent = connection.send({ type: 'attach_session', session_id: sessionId })
      if (!sent) {
        useConnectionStore.getState().setConnectionError(
          'Could not attach to session — connection dropped. Please reconnect and try again.'
        )
        // Leave state unchanged — do not call resetChatSession or update the store.
        return
      }
      // Send succeeded — now safe to commit new state.
      resetChatSession()
      set({
        activeSessionId: sessionId,
        attachedSessionType: type,
        attachedTaskTitle: title ?? null,
        activeAgentId: agentId ?? get().activeAgentId,
      })
      // FR-I-014: replay is now in flight — disable send until done arrives.
      setChatReplaying(true)
    } else {
      // No connection at all — commit state anyway (offline/optimistic path) but
      // do not set replaying since no replay will arrive.
      console.warn('[session] attachToSession: no connection — attach_session not sent')
      resetChatSession()
      set({
        activeSessionId: sessionId,
        attachedSessionType: type,
        attachedTaskTitle: title ?? null,
        activeAgentId: agentId ?? get().activeAgentId,
      })
    }
  },
}))
