import { useEffect, useRef, useState } from 'react'
import { generateId } from '@/lib/constants'
import { useQuery } from '@tanstack/react-query'
import {
  ThreadPrimitive,
  MessagePrimitive,
  MessagePartPrimitive,
  ComposerPrimitive,
  ActionBarPrimitive,
  AuiIf,
  useComposerRuntime,
  useMessageRuntime,
} from '@assistant-ui/react'
import {
  ArrowCounterClockwise,
  User,
  Robot,
  PaperPlaneRight,
  Stop,
  Copy,
  Check,
  Paperclip,
} from '@phosphor-icons/react'
import OmnipusAvatar from '@/assets/logo/omnipus-avatar.svg?url'
import { SessionPanel } from './SessionPanel'
import { GenericToolCall } from './tools/GenericToolCall'
import { ExecApprovalBlock } from './ExecApprovalBlock'
import { MarkdownText } from './markdown-text'
import { Button } from '@/components/ui/button'
import { useChatStore } from '@/store/chat'
import { useUiStore } from '@/store/ui'
import { fetchSessionMessages } from '@/lib/api'
import type { ToolCall } from '@/lib/api'
import { cn } from '@/lib/utils'

// ── Message components ────────────────────────────────────────────────────────

function UserMessage() {
  return (
    <MessagePrimitive.Root className="group flex gap-3 px-4 py-3 flex-row-reverse">
      <div className="shrink-0 w-7 h-7 rounded-full flex items-center justify-center bg-[var(--color-accent)]/20 text-[var(--color-accent)]">
        <User size={14} weight="bold" />
      </div>
      <div className="flex flex-col items-end gap-1 max-w-[85%] min-w-0">
        <div className="rounded-xl px-4 py-3 text-sm leading-relaxed bg-[var(--color-surface-2)] text-[var(--color-secondary)] rounded-tr-sm">
          <MessagePrimitive.Parts>
            {({ part }) => {
              if (part.type !== 'text') return null
              return <p className="whitespace-pre-wrap break-words">{part.text}</p>
            }}
          </MessagePrimitive.Parts>
        </div>
      </div>
    </MessagePrimitive.Root>
  )
}

function SystemMessage() {
  return (
    <MessagePrimitive.Root className="flex justify-center py-2">
      <div className="text-xs text-[var(--color-muted)] bg-[var(--color-surface-2)] px-3 py-1 rounded-full">
        <MessagePrimitive.Parts>
          {({ part }) => {
            if (part.type !== 'text') return null
            return <span>{part.text}</span>
          }}
        </MessagePrimitive.Parts>
      </div>
    </MessagePrimitive.Root>
  )
}

// Animated thinking indicator with rotating status messages
const THINKING_MESSAGES = [
  'Thinking…',
  'Composing response…',
  'Processing your request…',
  'Analyzing…',
  'Generating…',
]

function ThinkingIndicator() {
  const [msgIndex, setMsgIndex] = useState(0)

  useEffect(() => {
    const interval = setInterval(() => {
      setMsgIndex((i) => (i + 1) % THINKING_MESSAGES.length)
    }, 2000)
    return () => clearInterval(interval)
  }, [])

  return (
    <span className="text-[var(--color-muted)] italic flex items-center gap-2.5 py-1">
      <span className="flex gap-1">
        <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce" style={{ animationDelay: '0ms' }} />
        <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce" style={{ animationDelay: '150ms' }} />
        <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-bounce" style={{ animationDelay: '300ms' }} />
      </span>
      <span className="text-xs transition-opacity duration-300">{THINKING_MESSAGES[msgIndex]}</span>
    </span>
  )
}

// Custom text renderer with streaming cursor
function AssistantTextPart() {
  return (
    <div>
      <MarkdownText />
      <MessagePartPrimitive.InProgress>
        <span className="inline-block w-1.5 h-4 bg-[var(--color-accent)] ml-0.5 animate-pulse align-text-bottom" />
      </MessagePartPrimitive.InProgress>
    </div>
  )
}

// Shows thinking dots inside the assistant message when running with no content
function InlineThinkingIndicator() {
  const messageRuntime = useMessageRuntime()
  const state = messageRuntime.getState()
  const isRunning = state.status?.type === 'running'
  const hasContent = state.content?.some(
    (p: { type: string; text?: string }) => p.type === 'text' && p.text && p.text.length > 0
  )

  if (!isRunning || hasContent) return null
  return <ThinkingIndicator />
}

function AssistantMessage() {
  const storeToolCalls = useChatStore((s) => s.toolCalls)

  return (
    <MessagePrimitive.Root className="group flex gap-3 px-4 py-3">
      <div className="shrink-0 w-7 h-7 rounded-full flex items-center justify-center bg-[var(--color-surface-3)] text-[var(--color-secondary)]">
        <Robot size={14} weight="bold" />
      </div>
      <div className="flex flex-col gap-1 max-w-[85%] min-w-0 flex-1">
        <div className="text-sm leading-relaxed text-[var(--color-secondary)]">
          <InlineThinkingIndicator />
          <MessagePrimitive.Parts>
            {({ part }) => {
              if (part.type === 'text') {
                return <AssistantTextPart />
              }

              if (part.type === 'tool-call') {
                const enriched = part as typeof part & { toolUI?: React.ReactNode }
                if (enriched.toolUI) return <>{enriched.toolUI}</>

                const liveCall = storeToolCalls[part.toolCallId]
                const resolved: ToolCall | undefined = liveCall
                return (
                  <GenericToolCall
                    toolName={part.toolName}
                    args={part.args}
                    result={resolved?.result ?? part.result}
                    status={part.status}
                    error={resolved?.error}
                    durationMs={resolved?.duration_ms}
                  />
                )
              }

              return null
            }}
          </MessagePrimitive.Parts>
        </div>

        {/* Action bar — Copy button, visible on hover */}
        <ActionBarPrimitive.Root className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity duration-150">
          <ActionBarPrimitive.Copy asChild>
            <button
              type="button"
              className="flex items-center gap-1 px-2 py-1 rounded text-[10px] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors"
              title="Copy message"
            >
              <AuiIf condition={(s) => s.message.isCopied}>
                <Check size={11} weight="bold" className="text-[var(--color-success)]" />
                <span>Copied!</span>
              </AuiIf>
              <AuiIf condition={(s) => !s.message.isCopied}>
                <Copy size={11} />
                <span>Copy</span>
              </AuiIf>
            </button>
          </ActionBarPrimitive.Copy>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  )
}

// ── Slash command types ───────────────────────────────────────────────────────

interface SlashCommand {
  label: string
  description: string
}

// Built-in slash commands. Extensibility: agents can register custom commands
// by sending a "commands" frame over the WebSocket, keyed by agent_id.
const SLASH_COMMANDS: SlashCommand[] = [
  { label: '/session new', description: 'Start a new session' },
  { label: '/clear', description: 'Clear all messages' },
  { label: '/help', description: 'Show help information' },
]

const HELP_TEXT = `**Omnipus commands:**
- \`/session new\` — Start a new session
- \`/clear\` — Clear the current chat history
- \`/help\` — Show this help message

**Tips:**
- Press **Enter** to send, **Shift+Enter** for newline
- Click tool call headers to expand/collapse details
- Hover over messages to copy or pin them`

// ── Composer ──────────────────────────────────────────────────────────────────

function OmnipusComposer() {
  const isStreaming = useChatStore((s) => s.isStreaming)
  const isConnected = useChatStore((s) => s.isConnected)
  const cancelStream = useChatStore((s) => s.cancelStream)
  const setMessages = useChatStore((s) => s.setMessages)
  const appendMessage = useChatStore((s) => s.appendMessage)
  const addToast = useUiStore((s) => s.addToast)
  const composerRuntime = useComposerRuntime()

  const [inputValue, setInputValue] = useState('')
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashHighlight, setSlashHighlight] = useState(0)

  // Show slash dropdown when input starts with "/"
  const shouldShowSlash = inputValue.startsWith('/') && !isStreaming && isConnected

  function closeSlash() {
    setSlashOpen(false)
    setSlashHighlight(0)
  }

  function executeSlashCommand(cmd: string) {
    closeSlash()
    composerRuntime.setText('')
    setInputValue('')

    if (cmd === '/clear') {
      setMessages([])
      return
    }

    if (cmd === '/help') {
      appendMessage({
        id: generateId(),
        role: 'system',
        content: HELP_TEXT,
        timestamp: new Date().toISOString(),
        status: 'done',
      })
      return
    }

    if (cmd === '/session new') {
      addToast({ message: 'Session management coming soon', variant: 'default' })
      return
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (!shouldShowSlash) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSlashHighlight((h) => (h + 1) % SLASH_COMMANDS.length)
      setSlashOpen(true)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSlashHighlight((h) => (h - 1 + SLASH_COMMANDS.length) % SLASH_COMMANDS.length)
      setSlashOpen(true)
    } else if (e.key === 'Enter' && slashOpen) {
      e.preventDefault()
      executeSlashCommand(SLASH_COMMANDS[slashHighlight].label)
    } else if (e.key === 'Escape') {
      closeSlash()
    }
  }

  return (
    <div>
      {!isConnected && (
        <div className="mb-2 text-xs text-[var(--color-error)] flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-error)] inline-block" />
          Disconnected — reconnecting...
        </div>
      )}

      {/* Slash command dropdown */}
      {shouldShowSlash && slashOpen && (
        <div className="mb-2 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-2)] overflow-hidden shadow-lg">
          {SLASH_COMMANDS.map((cmd, i) => (
            <button
              key={cmd.label}
              type="button"
              className={cn(
                'w-full flex items-baseline gap-3 px-3 py-2 text-left transition-colors',
                i === slashHighlight
                  ? 'bg-[var(--color-accent)]/10 text-[var(--color-secondary)]'
                  : 'text-[var(--color-muted)] hover:bg-[var(--color-surface-3)] hover:text-[var(--color-secondary)]',
              )}
              onMouseDown={(e) => {
                e.preventDefault() // prevent blur before click registers
                executeSlashCommand(cmd.label)
              }}
              onMouseEnter={() => setSlashHighlight(i)}
            >
              <span className="font-mono text-xs text-[var(--color-accent)]">{cmd.label}</span>
              <span className="text-[11px]">{cmd.description}</span>
            </button>
          ))}
        </div>
      )}

      <ComposerPrimitive.Root
        className={cn(
          'flex items-end gap-2 px-2 py-2',
        )}
      >
        {/* File attachment */}
        <button
          type="button"
          onClick={() => addToast({ message: 'File attachments coming soon', variant: 'default' })}
          className="shrink-0 w-8 h-8 flex items-center justify-center rounded-full text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors"
          aria-label="Attach file"
          title="Attach file"
        >
          <Paperclip size={16} />
        </button>

        <ComposerPrimitive.Input
          placeholder={
            !isConnected
              ? 'Connecting to gateway...'
              : isStreaming
                ? 'Waiting for response...'
                : 'Message Omnipus…'
          }
          disabled={!isConnected || isStreaming}
          rows={1}
          className={cn(
            'flex-1 resize-none rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface-2)] px-4 py-2.5 text-sm text-[var(--color-secondary)] outline-none',
            'placeholder:text-[var(--color-muted)] min-h-[24px] max-h-[200px] leading-6 overflow-hidden',
            'focus:border-[var(--color-accent)]/50 focus:ring-1 focus:ring-[var(--color-accent)]/20',
            (!isConnected || isStreaming) && 'opacity-60 cursor-not-allowed',
          )}
          aria-label="Message input"
          onChange={(e) => {
            const val = (e.target as HTMLTextAreaElement).value
            setInputValue(val)
            if (val.startsWith('/')) {
              setSlashOpen(true)
            } else {
              closeSlash()
            }
          }}
          onKeyDown={handleKeyDown}
          onBlur={() => {
            // Delay so mouseDown on slash item fires first
            setTimeout(closeSlash, 150)
          }}
        />

        {isStreaming ? (
          <button
            type="button"
            onClick={cancelStream}
            className="shrink-0 w-8 h-8 rounded-lg bg-[var(--color-error)]/20 text-[var(--color-error)] hover:bg-[var(--color-error)]/30 flex items-center justify-center transition-colors"
            aria-label="Stop generation"
            title="Stop (Escape)"
          >
            <Stop size={15} weight="fill" />
          </button>
        ) : (
          <ComposerPrimitive.Send
            disabled={!isConnected}
            className={cn(
              'shrink-0 w-8 h-8 rounded-lg flex items-center justify-center transition-colors',
              isConnected
                ? 'bg-[var(--color-accent)] text-[var(--color-primary)] hover:bg-[var(--color-accent-hover)] disabled:bg-[var(--color-surface-3)] disabled:text-[var(--color-muted)] disabled:cursor-not-allowed'
                : 'bg-[var(--color-surface-3)] text-[var(--color-muted)] cursor-not-allowed',
            )}
            aria-label="Send message"
          >
            <PaperPlaneRight size={15} weight="bold" />
          </ComposerPrimitive.Send>
        )}
      </ComposerPrimitive.Root>

      <p className="mt-1.5 text-[10px] text-[var(--color-muted)] text-center">
        Agents can make mistakes. Verify important information.
      </p>
    </div>
  )
}

// ── Welcome state ─────────────────────────────────────────────────────────────

function WelcomeState({ hasAgent }: { hasAgent: boolean }) {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[60vh] gap-8 p-8">
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

// ── Main screen ───────────────────────────────────────────────────────────────

export function ChatScreen() {
  const {
    connectionError,
    activeSessionId,
    activeAgentId,
    pendingApprovals,
    setMessages,
    reconnect,
  } = useChatStore()

  // Load message history when session changes
  const {
    data: historyData,
    isError: historyError,
    refetch: refetchHistory,
  } = useQuery({
    queryKey: ['messages', activeSessionId],
    queryFn: () => fetchSessionMessages(activeSessionId!),
    enabled: !!activeSessionId,
    gcTime: 0,
  })

  useEffect(() => {
    if (historyData) setMessages(historyData)
  }, [historyData, setMessages])

  const activePendingApprovals = pendingApprovals.filter((a) => a.status === 'pending')

  return (
    <div className="flex flex-col h-full">
      {/* Connection error banner */}
      {connectionError && (
        <div className="flex items-center justify-between gap-2 px-4 py-2 bg-[var(--color-error)]/10 border-b border-[var(--color-error)]/20 text-xs text-[var(--color-error)]">
          <span>{connectionError}</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={reconnect}
            className="h-6 px-2 text-xs text-[var(--color-error)] hover:bg-[var(--color-error)]/10 gap-1"
          >
            <ArrowCounterClockwise size={11} /> Retry
          </Button>
        </div>
      )}

      {/* History fetch error */}
      {historyError ? (
        <div className="flex flex-col items-center justify-center flex-1 gap-3 text-sm text-[var(--color-muted)]">
          <p>Could not load messages.</p>
          <Button variant="outline" size="sm" onClick={() => refetchHistory()}>
            <ArrowCounterClockwise size={14} /> Retry
          </Button>
        </div>
      ) : (
        <ThreadPrimitive.Root className="flex flex-col flex-1 min-h-0">
          {/* Message viewport */}
          <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto pt-4 pb-8">
            <AuiIf condition={(s) => s.thread.isEmpty}>
              <WelcomeState hasAgent={!!activeAgentId} />
            </AuiIf>

            <div className="max-w-4xl mx-auto w-full">
              <ThreadPrimitive.Messages>
                {({ message }) => {
                  if (message.role === 'user') return <UserMessage />
                  if (message.role === 'system') return <SystemMessage />
                  return <AssistantMessage />
                }}
              </ThreadPrimitive.Messages>

              {/* Thinking indicator is now inside AssistantTextPart — no separate element needed */}
            </div>
          </ThreadPrimitive.Viewport>

          {/* Pending exec approval blocks — shown above composer */}
          {activePendingApprovals.length > 0 && (
            <div className="px-4 space-y-2 pb-2">
              {activePendingApprovals.map((approval) => (
                <ExecApprovalBlock key={approval.id} approval={approval} />
              ))}
            </div>
          )}

          {/* Composer — centered, ChatGPT-style floating layout */}
          <div className="relative w-full">
            {/* Gradient fade above composer */}
            <div className="absolute -top-8 left-0 right-0 h-8 bg-gradient-to-t from-[var(--color-primary)] to-transparent pointer-events-none" />
            <div className="w-full max-w-3xl mx-auto px-4 pb-6 pt-2">
              <OmnipusComposer />
            </div>
          </div>
        </ThreadPrimitive.Root>
      )}

      {/* Session slide-over panel */}
      <SessionPanel />
    </div>
  )
}
