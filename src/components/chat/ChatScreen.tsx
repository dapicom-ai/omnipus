import React, { useEffect, useRef, useState } from 'react'
import { generateId } from '@/lib/constants'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ThreadPrimitive,
  MessagePrimitive,
  MessagePartPrimitive,
  ComposerPrimitive,
  ActionBarPrimitive,
  AuiIf,
  useComposerRuntime,
  useMessage,
} from '@assistant-ui/react'
import {
  ArrowCounterClockwise,
  User,
  Robot,
  PaperPlaneRight,
  Stop,
  Copy,
  Check,
  ListChecks,
  Paperclip,
  File,
  X,
} from '@phosphor-icons/react'
import OmnipusAvatar from '@/assets/logo/omnipus-avatar.svg?url'
import { SessionPanel } from './SessionPanel'
import { GenericToolCall } from './tools/GenericToolCall'
import { ExecApprovalBlock } from './ExecApprovalBlock'
import { RateLimitIndicator } from './RateLimitIndicator'
import { MarkdownText } from './markdown-text'
import { Button } from '@/components/ui/button'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'
import { useSessionStore } from '@/store/session'
import { useUiStore } from '@/store/ui'
import { fetchAgents, fetchSessionMessages, createSession, uploadFiles } from '@/lib/api'
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

// Custom text renderer with streaming cursor.
// Wrapped in ErrorBoundary because MessagePartPrimitive.InProgress throws
// "MessagePartText can only be used inside text or reasoning message parts"
// when AssistantUI calls this component for a tool-call-only message.
class TextPartErrorBoundary extends React.Component<{ children: React.ReactNode }, { hasError: boolean }> {
  constructor(props: { children: React.ReactNode }) {
    super(props)
    this.state = { hasError: false }
  }
  static getDerivedStateFromError() { return { hasError: true } }
  render() { return this.state.hasError ? null : this.props.children }
}

function AssistantTextPart() {
  return (
    <TextPartErrorBoundary>
      <div>
        <MarkdownText />
        <MessagePartPrimitive.InProgress>
          <span className="inline-block w-1.5 h-4 bg-[var(--color-accent)] ml-0.5 animate-pulse align-text-bottom" />
        </MessagePartPrimitive.InProgress>
      </div>
    </TextPartErrorBoundary>
  )
}

// Shows thinking dots inside the assistant message when running with no text content.
// Uses useMessage() for reactive state (not getState() which is a snapshot).
function InlineThinkingIndicator() {
  const message = useMessage()
  const isRunning = message.status?.type === 'running'
  const hasText = message.content?.some(
    (p: { type: string; text?: string }) => p.type === 'text' && p.text && p.text.trim().length > 0
  )
  if (!isRunning || hasText) return null
  return <ThinkingIndicator />
}

// Fallback tool UI for tools without a registered makeAssistantToolUI component.
// ToolCallMessagePartProps passes: toolCallId, toolName, args, result, status
function FallbackToolUI(props: { toolCallId: string; toolName: string; args: unknown; result: unknown; status: import('@assistant-ui/react').MessagePartStatus }) {
  const storeToolCalls = useChatStore((s) => s.toolCalls)
  const liveCall = storeToolCalls[props.toolCallId]
  return (
    <GenericToolCall
      toolName={props.toolName}
      args={props.args}
      result={liveCall?.result ?? props.result}
      status={props.status}
      error={liveCall?.error}
      durationMs={liveCall?.duration_ms}
    />
  )
}

function AssistantMessageRetryButton() {
  const message = useMessage()
  const sendMessage = useChatStore((s) => s.sendMessage)
  const messages = useChatStore((s) => s.messages)
  const isStreaming = useChatStore((s) => s.isStreaming)

  const status = message.status?.type
  // AssistantUI maps our store's 'error' and 'interrupted' statuses both to
  // { type: 'incomplete' } via buildMessageStatus in omnipus-runtime.ts.
  const isErrorOrIncomplete = status === 'incomplete'
  const hasUserMessage = messages.some((m) => m.role === 'user')

  if (!isErrorOrIncomplete || isStreaming || !hasUserMessage) return null

  function handleRetry() {
    const lastUserMsg = [...messages].reverse().find((m) => m.role === 'user')
    if (lastUserMsg) {
      sendMessage(lastUserMsg.content)
    }
  }

  return (
    <button
      type="button"
      onClick={handleRetry}
      aria-label="Retry — resend the last user message"
      className="flex items-center gap-1 px-2 py-1 rounded text-[10px] text-[var(--color-error)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors"
      title="Retry — resend the last user message"
    >
      <ArrowCounterClockwise size={11} />
      <span>Retry</span>
    </button>
  )
}

// Renders inline media attachments (images, files) for the last assistant
// message that carries media. Media arrives via WebSocket "media" frames and
// is appended to the most recent assistant message in the chat store.
function InlineMedia() {
  const messages = useChatStore((s) => s.messages)
  const storeMsg = [...messages].reverse().find((m) => m.role === 'assistant' && m.media?.length)

  if (!storeMsg?.media?.length) return null

  return (
    <div className="flex flex-col gap-2 mt-2">
      {storeMsg.media.map((m, i) =>
        m.type === 'image' ? (
          <div key={`${m.url}-${i}`} className="rounded-lg overflow-hidden border border-[var(--color-border)] max-w-md">
            <img src={m.url} alt={m.caption || m.filename} className="w-full" loading="lazy" />
            {m.caption && (
              <p className="text-xs text-[var(--color-muted)] px-2 py-1">{m.caption}</p>
            )}
          </div>
        ) : (
          <a
            key={`${m.url}-${i}`}
            href={m.url}
            download={m.filename}
            className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-[var(--color-border)] text-xs text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors"
          >
            <File size={14} />
            {m.filename}
          </a>
        ),
      )}
    </div>
  )
}

function AssistantMessage() {
  return (
    <MessagePrimitive.Root className="group flex gap-3 px-4 py-3">
      <div className="shrink-0 w-7 h-7 rounded-full flex items-center justify-center bg-[var(--color-surface-3)] text-[var(--color-secondary)]">
        <Robot size={14} weight="bold" />
      </div>
      <div className="flex flex-col gap-1 max-w-[85%] min-w-0 flex-1">
        <div className="text-sm leading-relaxed text-[var(--color-secondary)]">
          <InlineThinkingIndicator />
          {/* Use components prop so AssistantUI can inject registered tool UIs
              (from makeAssistantToolUI) automatically by tool name. Unregistered
              tools fall through to FallbackToolUI (generic JSON badge). */}
          <MessagePrimitive.Parts
            components={{
              Text: AssistantTextPart,
              tools: {
                Fallback: FallbackToolUI as unknown as import('@assistant-ui/react').ToolCallMessagePartComponent,
              },
            }}
          />
          <InlineMedia />
        </div>

        {/* Action bar — Copy + Retry buttons, visible on hover */}
        <ActionBarPrimitive.Root className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity duration-150">
          <ActionBarPrimitive.Copy asChild>
            <button
              type="button"
              aria-label="Copy message"
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
          <AssistantMessageRetryButton />
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

// Built-in slash commands.
// TODO: agents will be able to register custom commands via a 'commands' WebSocket frame.
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
- Hover over messages to copy them`

// ── Composer ──────────────────────────────────────────────────────────────────

function composerPlaceholder(isConnected: boolean, isStreaming: boolean, agentName: string): string {
  if (!isConnected) return 'Connecting to gateway...'
  if (isStreaming) return 'Waiting for response...'
  return `Message ${agentName}…`
}

const HARMFUL_EXTENSIONS = ['.exe', '.bat', '.cmd', '.sh', '.ps1', '.dll', '.sys', '.msi', '.scr', '.com']

/** Renders an image thumbnail for a pending file, revoking the object URL on unmount to prevent memory leaks. */
function FilePreviewThumbnail({ file }: { file: File }) {
  const [url, setUrl] = useState<string | null>(null)

  useEffect(() => {
    const objectUrl = URL.createObjectURL(file)
    setUrl(objectUrl)
    return () => URL.revokeObjectURL(objectUrl)
  }, [file])

  if (!url) return null
  return <img src={url} className="w-8 h-8 rounded object-cover" alt={file.name} />
}

function OmnipusComposer() {
  const isStreaming = useChatStore((s) => s.isStreaming)
  const isConnected = useConnectionStore((s) => s.isConnected)
  const cancelStream = useChatStore((s) => s.cancelStream)
  const setMessages = useChatStore((s) => s.setMessages)
  const appendMessage = useChatStore((s) => s.appendMessage)
  const setActiveSession = useSessionStore((s) => s.setActiveSession)
  const activeAgentId = useSessionStore((s) => s.activeAgentId)
  const activeSessionId = useSessionStore((s) => s.activeSessionId)
  const sendMessage = useChatStore((s) => s.sendMessage)
  const addToast = useUiStore((s) => s.addToast)
  const composerRuntime = useComposerRuntime()
  const queryClient = useQueryClient()

  const { data: agents = [] } = useQuery({ queryKey: ['agents'], queryFn: fetchAgents })
  const activeAgentName = agents.find((a) => a.id === activeAgentId)?.name ?? 'Omnipus'

  const { mutate: doCreateSession, isPending: isCreatingSession } = useMutation({
    mutationFn: () => {
      if (!activeAgentId) throw new Error('No agent selected')
      return createSession(activeAgentId)
    },
    onSuccess: (session) => {
      // agentType is already set in the store from the agent selection in SessionBar.
      // Passing null here preserves the existing activeAgentType via setActiveSession's fallback.
      setActiveSession(session.id, session.agent_id, null)
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      addToast({ message: 'New session started', variant: 'success' })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const [inputValue, setInputValue] = useState('')
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashHighlight, setSlashHighlight] = useState(0)
  const [pendingFiles, setPendingFiles] = useState<File[]>([])
  const [isUploading, setIsUploading] = useState(false)
  const [isDragging, setIsDragging] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  // Tracks whether we already warned for the current large-input threshold crossing,
  // so we only fire one toast per paste/input event that exceeds 1MB.
  const hasWarnedLargeInput = useRef(false)

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
      const { activeSessionId: sid } = useSessionStore.getState()
      if (sid) queryClient.removeQueries({ queryKey: ['messages', sid] })
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
      if (!isCreatingSession) doCreateSession()
      return
    }
  }

  function handleFilesSelected(files: File[]) {
    const harmful = files.filter((f) =>
      HARMFUL_EXTENSIONS.some((ext) => f.name.toLowerCase().endsWith(ext))
    )
    if (harmful.length > 0) {
      const confirmed = window.confirm(
        `Warning: ${harmful.map((f) => f.name).join(', ')} may be potentially harmful file(s).\n\nAre you sure you want to upload?`
      )
      if (!confirmed) return
      const doubleConfirmed = window.confirm(
        `Please confirm again: Upload ${harmful.length} potentially harmful file(s)?`
      )
      if (!doubleConfirmed) return
    }
    setPendingFiles((prev) => [...prev, ...files])
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

  async function handleSendWithFiles(text: string) {
    if (pendingFiles.length === 0) {
      sendMessage(text)
      return
    }
    if (!activeSessionId) {
      addToast({ message: 'No active session — cannot upload files', variant: 'error' })
      return
    }
    setIsUploading(true)
    try {
      const { files: uploaded } = await uploadFiles(activeSessionId, pendingFiles)
      const fileList = uploaded.map((f) => `[${f.name}](${f.path})`).join(', ')
      sendMessage(text ? `${text}\n\nAttached files: ${fileList}` : `Attached files: ${fileList}`)
      setPendingFiles([])
    } catch (err) {
      addToast({
        message: err instanceof Error ? err.message : 'Upload failed',
        variant: 'error',
      })
    } finally {
      setIsUploading(false)
    }
  }

  return (
    <div
      className="relative"
      onDragOver={(e) => { e.preventDefault(); setIsDragging(true) }}
      onDragLeave={() => setIsDragging(false)}
      onDrop={(e) => {
        e.preventDefault()
        setIsDragging(false)
        const droppedFiles = Array.from(e.dataTransfer.files)
        if (droppedFiles.length > 0) handleFilesSelected(droppedFiles)
      }}
    >
      {isDragging && (
        <div className="absolute inset-0 z-50 flex items-center justify-center bg-[var(--color-primary)]/80 border-2 border-dashed border-[var(--color-accent)] rounded-lg">
          <p className="text-[var(--color-accent)] font-medium">Drop files here</p>
        </div>
      )}

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

      {/* Pending file previews */}
      {pendingFiles.length > 0 && (
        <div className="flex flex-wrap gap-2 px-2 pb-2">
          {pendingFiles.map((file, i) => (
            <div
              key={`${file.name}-${file.size}-${file.lastModified}`}
              className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-[var(--color-surface-2)] border border-[var(--color-border)] text-xs"
            >
              {file.type.startsWith('image/') ? (
                <FilePreviewThumbnail file={file} />
              ) : (
                <File size={14} className="text-[var(--color-muted)]" />
              )}
              <span className="truncate max-w-[120px]">{file.name}</span>
              <button
                type="button"
                onClick={() => setPendingFiles((prev) => prev.filter((_, j) => j !== i))}
                className="text-[var(--color-muted)] hover:text-[var(--color-error)]"
                aria-label={`Remove ${file.name}`}
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Hidden file input */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          if (files.length) handleFilesSelected(files)
          e.target.value = ''
        }}
      />

      <div className="flex items-end gap-2 px-2 py-2">
        {/* Paperclip button */}
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={!isConnected || isStreaming || isUploading}
          className="shrink-0 w-11 h-11 rounded-xl flex items-center justify-center text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          aria-label="Attach file"
          title="Attach file"
        >
          <Paperclip size={16} />
        </button>

      <ComposerPrimitive.Root
        className="flex items-end gap-2 flex-1"
        onSubmit={(e) => {
          if (pendingFiles.length === 0) return // Let AssistantUI handle the standard send path
          e.preventDefault()
          const text = composerRuntime.getState().text.trim()
          composerRuntime.setText('')
          setInputValue('')
          void handleSendWithFiles(text)
        }}
      >
        <ComposerPrimitive.Input
          placeholder={composerPlaceholder(isConnected, isStreaming || isUploading, activeAgentName)}
          disabled={!isConnected || isStreaming || isUploading}
          rows={1}
          className={cn(
            'flex-1 resize-none rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface-2)] px-4 py-2.5 text-sm text-[var(--color-secondary)] outline-none',
            'placeholder:text-[var(--color-muted)] min-h-[24px] max-h-[200px] leading-6 overflow-hidden',
            'focus:border-[var(--color-accent)]/50 focus:ring-1 focus:ring-[var(--color-accent)]/20',
            (!isConnected || isStreaming || isUploading) && 'opacity-60 cursor-not-allowed',
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
            if (val.length > 1_000_000) {
              if (!hasWarnedLargeInput.current) {
                hasWarnedLargeInput.current = true
                useUiStore.getState().addToast({
                  message: `Large input (${(val.length / 1_000_000).toFixed(1)}MB). This may be slow to process.`,
                  variant: 'default',
                })
              }
            } else {
              hasWarnedLargeInput.current = false
            }
          }}
          onKeyDown={handleKeyDown}
          onBlur={() => {
            // Delay so mouseDown on slash item fires first
            setTimeout(closeSlash, 150)
          }}
          onPaste={(e) => {
            const items = Array.from(e.clipboardData?.items ?? [])
            const imageItems = items.filter((item) => item.type.startsWith('image/'))
            const imageFiles = imageItems.map((item) => item.getAsFile()).filter(Boolean) as File[]
            if (imageFiles.length > 0) {
              e.preventDefault()
              handleFilesSelected(imageFiles)
            } else if (imageItems.length > 0) {
              // Images were in clipboard but couldn't be materialized as files
              e.preventDefault()
              addToast({ message: 'Could not paste image — try saving it to a file first', variant: 'error' })
            }
          }}
        />

        {isStreaming || isUploading ? (
          <button
            type="button"
            onClick={isStreaming ? cancelStream : undefined}
            disabled={isUploading}
            className={cn(
              'shrink-0 w-11 h-11 rounded-xl flex items-center justify-center transition-colors',
              isStreaming
                ? 'bg-[var(--color-error)]/20 text-[var(--color-error)] hover:bg-[var(--color-error)]/30'
                : 'bg-[var(--color-surface-3)] text-[var(--color-muted)] cursor-wait',
            )}
            aria-label={isUploading ? 'Uploading...' : 'Stop generation'}
            title={isUploading ? 'Uploading files...' : 'Stop (Escape)'}
          >
            <Stop size={15} weight="fill" />
          </button>
        ) : (
          <ComposerPrimitive.Send
            disabled={!isConnected}
            className={cn(
              'shrink-0 w-11 h-11 rounded-xl flex items-center justify-center transition-colors',
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
      </div>

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
  const activeSessionId = useSessionStore((s) => s.activeSessionId)
  const activeAgentId = useSessionStore((s) => s.activeAgentId)
  const pendingApprovals = useChatStore((s) => s.pendingApprovals)
  const rateLimitEvent = useChatStore((s) => s.rateLimitEvent)
  const clearRateLimitEvent = useChatStore((s) => s.clearRateLimitEvent)
  const setMessages = useChatStore((s) => s.setMessages)
  const attachedSessionType = useSessionStore((s) => s.attachedSessionType)
  const attachedTaskTitle = useSessionStore((s) => s.attachedTaskTitle)
  // For the ARIA live region: track the last assistant message id for screen reader announcements
  const messages = useChatStore((s) => s.messages)
  const lastAssistantMessage = [...messages].reverse().find((m) => m.role === 'assistant')
  const lastAnnouncedIdRef = useRef<string | null>(null)
  const shouldAnnounce = lastAssistantMessage?.id != null && lastAssistantMessage.id !== lastAnnouncedIdRef.current

  useEffect(() => {
    if (lastAssistantMessage?.id && lastAssistantMessage.status === 'done') {
      lastAnnouncedIdRef.current = lastAssistantMessage.id
    }
  }, [lastAssistantMessage?.id, lastAssistantMessage?.status])

  const { data: agentsForAria = [] } = useQuery({ queryKey: ['agents'], queryFn: fetchAgents })
  const activeAgentName = agentsForAria.find((a) => a.id === activeAgentId)?.name ?? 'Omnipus'

  // Load message history when session changes
  const isConnected = useConnectionStore((s) => s.isConnected)

  const {
    data: historyData,
    isError: historyError,
    refetch: refetchHistory,
  } = useQuery({
    queryKey: ['messages', activeSessionId],
    queryFn: () => fetchSessionMessages(activeSessionId!),
    enabled: !!activeSessionId,
    gcTime: 0,
    // Never re-fetch on window focus — the WebSocket delivers live updates
    refetchOnWindowFocus: false,
    // Skip re-fetch on mount when the WebSocket is already connected and delivering messages
    refetchOnMount: !isConnected,
  })

  useEffect(() => {
    if (historyData) {
      // Filter out tool_call entries that have no role — they crash AssistantUI's convertMessages.
      // Tool calls are replayed via WebSocket frames instead.
      const validMessages = historyData.filter((m: { role?: string }) => m.role === 'user' || m.role === 'assistant' || m.role === 'system')
      setMessages(validMessages)
    }
  }, [historyData, setMessages])

  const activePendingApprovals = pendingApprovals.filter((a) => a.status === 'pending')

  return (
    <div className="flex flex-col absolute inset-0 overflow-hidden">
      {/* Task session banner — shown when viewing a task execution transcript */}
      {attachedSessionType === 'task' && (
        <div className="px-4 py-2 bg-[var(--color-surface-2)] border-b border-[var(--color-border)] flex items-center gap-2">
          <ListChecks size={14} className="text-[var(--color-accent)] shrink-0" />
          <span className="text-xs text-[var(--color-secondary)] flex-1 truncate">
            Task: {attachedTaskTitle ?? 'Task Execution'}
          </span>
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
          {/* ARIA live region: announces new assistant messages to screen readers.
              Only fires when a genuinely new message ID arrives and is complete. */}
          <div aria-live="polite" aria-atomic="true" className="sr-only">
            {shouldAnnounce && lastAssistantMessage?.status === 'done' && (
              <span>New response from {activeAgentName}</span>
            )}
          </div>

          {/* Message viewport */}
          <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto pt-4 pb-2">
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

            </div>
          </ThreadPrimitive.Viewport>

          {/* Pending exec approval blocks — shown above composer */}
          {(activePendingApprovals.length > 0 || rateLimitEvent) && (
            <div className="px-4 space-y-2 pb-2">
              {rateLimitEvent && (
                <RateLimitIndicator
                  scope={rateLimitEvent.scope}
                  resource={rateLimitEvent.resource}
                  policyRule={rateLimitEvent.policyRule}
                  retryAfterSeconds={rateLimitEvent.retryAfterSeconds}
                  tool={rateLimitEvent.tool}
                  onDismiss={clearRateLimitEvent}
                />
              )}
              {activePendingApprovals.map((approval) => (
                <ExecApprovalBlock key={approval.id} approval={approval} />
              ))}
            </div>
          )}

          {/* Composer — centered, ChatGPT-style floating layout */}
          <div className="relative w-full">
            {/* Gradient fade above composer */}
            <div className="absolute -top-8 left-0 right-0 h-8 bg-gradient-to-t from-[var(--color-primary)] to-transparent pointer-events-none" />
            <div className="w-full max-w-3xl mx-auto px-4 pb-2 pt-2">
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
