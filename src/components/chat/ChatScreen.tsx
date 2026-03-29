import { useEffect, useRef, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowCounterClockwise } from '@phosphor-icons/react'
import OmnipusAvatar from '@/assets/logo/omnipus-avatar.svg?url'
import { MessageItem } from './MessageItem'
import { MessageInput } from './MessageInput'
import { SessionPanel } from './SessionPanel'
import { Button } from '@/components/ui/button'
import { useChatStore } from '@/store/chat'
import { WsConnection } from '@/lib/ws'
import { fetchSessionMessages } from '@/lib/api'

export function ChatScreen() {
  const {
    messages,
    connectionError,
    activeSessionId,
    activeAgentId,
    setMessages,
    setConnection,
    setConnected,
    setConnectionError,
    handleFrame,
  } = useChatStore()

  const scrollRef = useRef<HTMLDivElement>(null)
  const connectionRef = useRef<WsConnection | null>(null)

  // Load message history when session changes (TanStack Query v5 — no onSuccess callback)
  const { data: historyData, isError: historyError, refetch: refetchHistory } = useQuery({
    queryKey: ['messages', activeSessionId],
    queryFn: () => fetchSessionMessages(activeSessionId!),
    enabled: !!activeSessionId,
    gcTime: 0,
  })

  useEffect(() => {
    if (historyData) setMessages(historyData)
  }, [historyData, setMessages])

  // WebSocket connection lifecycle
  const connect = useCallback(() => {
    if (connectionRef.current) {
      connectionRef.current.disconnect()
    }

    const conn = new WsConnection({
      onFrame: handleFrame,
      onConnected: () => {
        setConnected(true)
        setConnectionError(null)
      },
      onDisconnected: () => setConnected(false),
      onError: (error) => setConnectionError(error),
    })

    conn.connect()
    connectionRef.current = conn
    setConnection(conn)
  }, [handleFrame, setConnected, setConnectionError, setConnection])

  useEffect(() => {
    connect()
    return () => {
      connectionRef.current?.disconnect()
      connectionRef.current = null
    }
  }, [connect])

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    if (isNearBottom) {
      el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
    }
  }, [messages])

  const hasMessages = messages.length > 0

  return (
    <div className="flex flex-col h-full">
      {/* Connection error banner */}
      {connectionError && (
        <div className="flex items-center justify-between gap-2 px-4 py-2 bg-[var(--color-error)]/10 border-b border-[var(--color-error)]/20 text-xs text-[var(--color-error)]">
          <span>{connectionError}</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={connect}
            className="h-6 px-2 text-xs text-[var(--color-error)] hover:bg-[var(--color-error)]/10 gap-1"
          >
            <ArrowCounterClockwise size={11} /> Retry
          </Button>
        </div>
      )}

      {/* Message area */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto py-4"
      >
        {historyError ? (
          <div className="flex flex-col items-center justify-center h-full gap-3 text-sm text-[var(--color-muted)]">
            <p>Could not load messages.</p>
            <Button variant="outline" size="sm" onClick={() => refetchHistory()}>
              <ArrowCounterClockwise size={14} /> Retry
            </Button>
          </div>
        ) : !hasMessages && !activeSessionId ? (
          <WelcomeState hasAgent={!!activeAgentId} />
        ) : !hasMessages ? (
          <div className="flex flex-col items-center justify-center h-full gap-2 text-sm text-[var(--color-muted)]">
            <p>No messages yet. Start a conversation!</p>
          </div>
        ) : (
          <div className="space-y-0 max-w-4xl mx-auto w-full">
            {messages.map((msg) => (
              <MessageItem key={msg.id} message={msg} />
            ))}
          </div>
        )}
      </div>

      {/* Input */}
      <MessageInput />

      {/* Session slide-over panel */}
      <SessionPanel />
    </div>
  )
}

function WelcomeState({ hasAgent }: { hasAgent: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-8 p-8">
      <div className="flex flex-col items-center gap-6 text-center max-w-md">
        <img
          src={OmnipusAvatar}
          alt="Omnipus mascot"
          className="h-20 w-20 drop-shadow-lg"
        />
        <div>
          <h1 className="font-headline text-2xl font-bold text-[var(--color-secondary)] mb-2">
            Welcome to Omnipus
          </h1>
          <p className="text-[var(--color-muted)] text-sm">
            {hasAgent
              ? 'Your agent is ready. Start a conversation below.'
              : 'Select an agent in the session bar to get started.'}
          </p>
        </div>
      </div>
    </div>
  )
}
