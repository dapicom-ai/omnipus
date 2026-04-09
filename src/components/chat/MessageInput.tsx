import { useState, useRef, useCallback } from 'react'
import { PaperPlaneRight, Stop } from '@phosphor-icons/react'
import { useChatStore } from '@/store/chat'
import { useConnectionStore } from '@/store/connection'
import { cn } from '@/lib/utils'

export function MessageInput() {
  const [value, setValue] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const { sendMessage, cancelStream, isStreaming } = useChatStore()
  const { isConnected } = useConnectionStore()

  const handleSend = useCallback(() => {
    const trimmed = value.trim()
    if (!trimmed || isStreaming || !isConnected) return
    sendMessage(trimmed)
    setValue('')
    textareaRef.current?.focus()
  }, [value, isStreaming, isConnected, sendMessage])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
    // Escape cancels streaming (no-op when idle)
    if (e.key === 'Escape' && isStreaming) {
      cancelStream()
    }
  }

  const handleInput = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setValue(e.target.value)
    // Auto-resize
    const el = e.target
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }

  const canSend = value.trim().length > 0 && !isStreaming && isConnected
  const disconnected = !isConnected

  return (
    <div className="border-t border-[var(--color-border)] bg-[var(--color-surface-1)] px-4 py-3">
      {disconnected && (
        <div className="mb-2 text-xs text-[var(--color-error)] flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-error)] inline-block" />
          Disconnected — reconnecting...
        </div>
      )}
      <div
        className={cn(
          'flex items-end gap-2 rounded-xl border bg-[var(--color-surface-2)] px-3 py-2 transition-colors',
          'focus-within:border-[var(--color-accent)]/50 focus-within:ring-1 focus-within:ring-[var(--color-accent)]/20',
          disconnected ? 'border-[var(--color-error)]/30' : 'border-[var(--color-border)]'
        )}
      >
        <textarea
          ref={textareaRef}
          value={value}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          placeholder={
            disconnected
              ? 'Connecting to gateway...'
              : isStreaming
              ? 'Waiting for response...'
              : 'Message your agent… (Enter to send, Shift+Enter for newline)'
          }
          disabled={disconnected || isStreaming}
          rows={1}
          className={cn(
            'flex-1 resize-none bg-transparent text-sm text-[var(--color-secondary)] outline-none placeholder:text-[var(--color-muted)] min-h-[24px] max-h-[200px] leading-6',
            (disconnected || isStreaming) && 'opacity-60 cursor-not-allowed'
          )}
          style={{ overflow: 'hidden' }}
          aria-label="Message input"
        />

        {/* Send / Stop button */}
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
          <button
            type="button"
            onClick={handleSend}
            disabled={!canSend}
            className={cn(
              'shrink-0 w-8 h-8 rounded-lg flex items-center justify-center transition-colors',
              canSend
                ? 'bg-[var(--color-accent)] text-[var(--color-primary)] hover:bg-[var(--color-accent-hover)]'
                : 'bg-[var(--color-surface-3)] text-[var(--color-muted)] cursor-not-allowed'
            )}
            aria-label="Send message"
          >
            <PaperPlaneRight size={15} weight="bold" />
          </button>
        )}
      </div>

      <p className="mt-1.5 text-[10px] text-[var(--color-muted)] text-center">
        Agents can make mistakes. Verify important information.
      </p>
    </div>
  )
}
