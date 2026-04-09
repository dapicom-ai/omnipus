import { useState, useRef, useEffect, useCallback } from 'react'
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { Circle, ChatCircle, ListChecks, Trash, MagnifyingGlass } from '@phosphor-icons/react'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { useUiStore } from '@/store/ui'
import { useSessionStore } from '@/store/session'
import { fetchAgents, fetchSessions, renameSession, deleteSession } from '@/lib/api'
import type { Agent, Session } from '@/lib/api'
import { cn } from '@/lib/utils'

function sessionButtonClass(isActive: boolean): string {
  return isActive
    ? 'bg-[var(--color-accent)]/10 text-[var(--color-accent)]'
    : 'text-[var(--color-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-secondary)]'
}

// ── Inline rename + delete session item ──────────────────────────────────────

const UNTITLED_SESSION = 'Untitled Session'

interface SessionItemProps {
  session: Session
  isActive: boolean
  onSelect: () => void
  onDeleted: (sessionId: string) => void
}

function SessionItem({ session, isActive, onSelect, onDeleted }: SessionItemProps) {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()

  const [isEditing, setIsEditing] = useState(false)
  const [editValue, setEditValue] = useState(session.title || UNTITLED_SESSION)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (isEditing && inputRef.current) {
      inputRef.current.focus()
      inputRef.current.select()
    }
  }, [isEditing])

  const { mutate: doRename, isPending: isRenaming } = useMutation({
    mutationFn: (title: string) => renameSession(session.id, title),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
    onError: (err: Error) => {
      addToast({ message: `Could not rename session: ${err.message}`, variant: 'error' })
      setEditValue(session.title || UNTITLED_SESSION)
    },
    onSettled: () => setIsEditing(false),
  })

  const { mutate: doDelete, isPending: isDeleting } = useMutation({
    mutationFn: () => deleteSession(session.id),
    onSuccess: () => {
      onDeleted(session.id)
    },
    onError: (err: Error) => {
      addToast({ message: `Could not delete session: ${err.message}`, variant: 'error' })
      setConfirmDelete(false)
    },
  })

  function commitRename() {
    const trimmed = editValue.trim()
    if (!trimmed || trimmed === session.title) {
      setIsEditing(false)
      setEditValue(session.title || 'Untitled Session')
      return
    }
    doRename(trimmed)
  }

  function handleTitleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commitRename()
    }
    if (e.key === 'Escape') {
      setIsEditing(false)
      setEditValue(session.title || UNTITLED_SESSION)
    }
  }

  if (confirmDelete) {
    return (
      <div className="flex items-center gap-1 px-10 py-2 text-xs">
        <span className="text-[var(--color-secondary)] flex-1 truncate">Delete?</span>
        <button
          type="button"
          disabled={isDeleting}
          onClick={() => doDelete()}
          className="px-1.5 py-0.5 rounded text-[var(--color-error)] hover:bg-[var(--color-error)]/10 transition-colors disabled:opacity-50"
        >
          {isDeleting ? '...' : 'Yes'}
        </button>
        <button
          type="button"
          onClick={() => setConfirmDelete(false)}
          className="px-1.5 py-0.5 rounded text-[var(--color-muted)] hover:bg-[var(--color-surface-2)] transition-colors"
        >
          No
        </button>
      </div>
    )
  }

  return (
    <div className={cn('group/item flex items-center gap-1 rounded-sm transition-colors', sessionButtonClass(isActive))}>
      {isEditing ? (
        <input
          ref={inputRef}
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onBlur={commitRename}
          onKeyDown={handleTitleKeyDown}
          disabled={isRenaming}
          className="flex-1 ml-10 mr-2 py-2 text-xs bg-transparent border-b border-[var(--color-accent)]/50 outline-none text-[var(--color-secondary)] disabled:opacity-50"
        />
      ) : (
        <button
          type="button"
          onClick={onSelect}
          onDoubleClick={(e) => {
            e.preventDefault()
            setIsEditing(true)
          }}
          aria-label={`Open session: ${session.title || UNTITLED_SESSION}`}
          className="flex-1 text-left px-10 py-2 text-xs transition-colors"
          title="Click to open, double-click to rename"
        >
          <div className="flex items-center gap-2">
            {isActive && (
              <Circle size={5} weight="fill" className="text-[var(--color-success)] shrink-0" />
            )}
            <span className="truncate">{session.title || UNTITLED_SESSION}</span>
          </div>
        </button>
      )}
      {!isEditing && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            setConfirmDelete(true)
          }}
          className="shrink-0 mr-2 p-1 rounded opacity-0 group-hover/item:opacity-100 text-[var(--color-muted)] hover:text-[var(--color-error)] hover:bg-[var(--color-error)]/10 transition-all"
          aria-label={`Delete session: ${session.title || UNTITLED_SESSION}`}
          title="Delete session"
        >
          <Trash size={11} />
        </button>
      )}
    </div>
  )
}

// ── Per-agent accordion with conversations + tasks sub-sections ───────────────

interface AgentAccordionItemProps {
  agent: Agent
  chatSessions: Session[]
  taskSessions: Session[]
  activeSessionId: string | null
  onSelectChat: (sessionId: string, agentId: string) => void
  onSelectTask: (session: Session) => void
  onSessionDeleted: (deletedId: string) => void
}

function taskStatusStyle(status: string | undefined): { color: string; label: string } {
  switch (status) {
    case 'archived':
      return { color: 'text-[var(--color-success)]', label: 'completed' }
    case 'interrupted':
      return { color: 'text-[var(--color-error)]', label: 'failed' }
    default:
      return { color: 'text-[var(--color-warning)]', label: 'running' }
  }
}

function AgentAccordionItem({
  agent,
  chatSessions,
  taskSessions,
  activeSessionId,
  onSelectChat,
  onSelectTask,
  onSessionDeleted,
}: AgentAccordionItemProps) {
  const hasTasks = taskSessions.length > 0

  return (
    <AccordionItem value={agent.id}>
      <AccordionTrigger className="px-4 hover:no-underline">
        <div className="flex items-center gap-2 min-w-0">
          <div
            className="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold shrink-0"
            style={{ backgroundColor: agent.color ?? 'var(--color-surface-3)' }}
          >
            {agent.name.charAt(0).toUpperCase()}
          </div>
          <span className="truncate text-sm">{agent.name}</span>
          {agent.status === 'active' && (
            <Circle size={6} weight="fill" className="text-[var(--color-success)] shrink-0" />
          )}
        </div>
      </AccordionTrigger>
      <AccordionContent className="pb-1">
        {/* Conversations sub-section */}
        <div className="mb-1">
          <div className="flex items-center gap-1.5 px-4 pt-1 pb-0.5">
            <ChatCircle size={10} className="text-[var(--color-accent)] shrink-0" />
            <span className="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted)]">
              Conversations
            </span>
          </div>
          <div className="space-y-0.5">
            {chatSessions.length === 0 ? (
              <p className="px-10 py-1.5 text-[11px] text-[var(--color-muted)] italic">No sessions yet.</p>
            ) : (
              chatSessions.map((session) => (
                <SessionItem
                  key={session.id}
                  session={session}
                  isActive={session.id === activeSessionId}
                  onSelect={() => onSelectChat(session.id, agent.id)}
                  onDeleted={onSessionDeleted}
                />
              ))
            )}
          </div>
        </div>

        {/* Tasks sub-section — only shown when the agent has task sessions */}
        {hasTasks && (
          <div className="mt-1">
            <div className="flex items-center gap-1.5 px-4 pt-1 pb-0.5">
              <ListChecks size={10} className="text-[var(--color-accent)] shrink-0" />
              <span className="text-[10px] font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                Tasks
              </span>
            </div>
            <div className="space-y-0.5 pb-1">
              {taskSessions.map((session) => (
                <button
                  key={session.id}
                  type="button"
                  onClick={() => onSelectTask(session)}
                  aria-label={`Open task: ${session.title || 'Untitled Task'}`}
                  className={`w-full text-left px-4 py-2 text-xs transition-colors rounded-sm ${sessionButtonClass(session.id === activeSessionId)}`}
                >
                  <div className="flex items-center gap-2 min-w-0 px-6">
                    <span className="truncate flex-1">
                      {session.title || 'Untitled Task'}
                    </span>
                    <Badge
                      variant="outline"
                      className={cn(
                        'text-[9px] h-4 px-1 shrink-0',
                        taskStatusStyle(session.status).color,
                      )}
                    >
                      {taskStatusStyle(session.status).label}
                    </Badge>
                  </div>
                </button>
              ))}
            </div>
          </div>
        )}
      </AccordionContent>
    </AccordionItem>
  )
}

// ── Main panel ────────────────────────────────────────────────────────────────

function defaultAccordionValue(activeAgentId: string | null, visibleAgents: Agent[]): string[] {
  if (activeAgentId) return [activeAgentId]
  if (visibleAgents.length > 0) return [visibleAgents[0].id]
  return []
}

export function SessionPanel() {
  const { sessionPanelOpen, closeSessionPanel } = useUiStore()
  const { activeSessionId, activeAgentId, setActiveSession, attachToSession } = useSessionStore()
  const queryClient = useQueryClient()

  const [searchValue, setSearchValue] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const handleSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value
    setSearchValue(val)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => setDebouncedSearch(val), 300)
  }, [])

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const { data: agents = [], isError: agentsError } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    enabled: sessionPanelOpen,
  })

  const { data: sessions = [], isError: sessionsError } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => fetchSessions(),
    enabled: sessionPanelOpen,
  })

  const handleSelectSession = (sessionId: string, agentId: string) => {
    const agent = agents.find((a) => a.id === agentId)
    setActiveSession(sessionId, agentId, agent?.type ?? null)
    closeSessionPanel()
  }

  const handleSelectTaskSession = (session: Session) => {
    attachToSession(session.id, 'task', session.title, session.agent_id)
    closeSessionPanel()
  }

  const handleSessionDeleted = (deletedId: string) => {
    queryClient.invalidateQueries({ queryKey: ['sessions'] })
    if (activeSessionId === deletedId) {
      setActiveSession(null, null, null)
    }
  }

  // Normalise system agent ID for sessions that were created under 'default' or 'main'
  const systemAgentId = agents.find((a) => a.type === 'system')?.id ?? 'omnipus-system'

  const chatSessions = sessions.filter((s) => !s.type || s.type === 'chat')
  const taskSessions = sessions.filter((s) => s.type === 'task')

  // Group sessions by agent, normalising legacy agent IDs
  function groupByAgent(items: Session[]): Record<string, Session[]> {
    return items.reduce<Record<string, Session[]>>((acc, s) => {
      const agentId = (s.agent_id === 'default' || s.agent_id === 'main') ? systemAgentId : s.agent_id
      if (!acc[agentId]) acc[agentId] = []
      acc[agentId].push(s)
      return acc
    }, {})
  }

  const chatByAgent = groupByAgent(chatSessions)
  const tasksByAgent = groupByAgent(taskSessions)

  // Show only agents that have at least one session (chat or task)
  const agentsWithContent = new Set([...Object.keys(chatByAgent), ...Object.keys(tasksByAgent)])
  const visibleAgents = agents.filter((a) => agentsWithContent.has(a.id))

  // Apply search filter
  const searchLower = debouncedSearch.toLowerCase().trim()

  function getFilteredSessions(group: Record<string, Session[]>, agentId: string): Session[] {
    const agentSessions = group[agentId] ?? []
    if (!searchLower) return agentSessions
    return agentSessions.filter((s) => (s.title ?? '').toLowerCase().includes(searchLower))
  }

  // When searching, show agent if agent name matches OR it has matching sessions
  const filteredAgents = searchLower
    ? visibleAgents.filter((agent) => {
        const agentNameMatches = agent.name.toLowerCase().includes(searchLower)
        return (
          agentNameMatches ||
          getFilteredSessions(chatByAgent, agent.id).length > 0 ||
          getFilteredSessions(tasksByAgent, agent.id).length > 0
        )
      })
    : visibleAgents

  const hasAnyResults = filteredAgents.length > 0

  return (
    <Sheet open={sessionPanelOpen} onOpenChange={(open) => !open && closeSessionPanel()}>
      <SheetContent side="right" className="w-72 p-0 flex flex-col" overlay={false}>
        <SheetHeader className="px-4 pt-5 pb-3 border-b border-[var(--color-border)]">
          <SheetTitle>Sessions</SheetTitle>
        </SheetHeader>

        {/* Search input */}
        <div className="px-4 py-2 border-b border-[var(--color-border)]">
          <div className="flex items-center gap-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)] px-3 py-1.5">
            <MagnifyingGlass size={13} className="text-[var(--color-muted)] shrink-0" />
            <input
              type="text"
              value={searchValue}
              onChange={handleSearchChange}
              placeholder="Search sessions..."
              aria-label="Search sessions"
              className="flex-1 bg-transparent text-xs text-[var(--color-secondary)] placeholder:text-[var(--color-muted)] outline-none"
            />
          </div>
        </div>

        <div className="flex-1 overflow-y-auto">
          {(agentsError || sessionsError) && (
            <div className="px-4 py-3 text-xs text-[var(--color-error)]">
              Could not load sessions.
            </div>
          )}

          {!hasAnyResults ? (
            <div className="px-4 py-6 text-xs text-[var(--color-muted)] text-center">
              {searchLower ? 'No results.' : 'No sessions yet. Start a conversation to begin.'}
            </div>
          ) : (
            <Accordion
              type="multiple"
              defaultValue={defaultAccordionValue(activeAgentId, visibleAgents)}
            >
              {filteredAgents.map((agent) => {
                const agentChatSessions = getFilteredSessions(chatByAgent, agent.id)
                const agentTaskSessions = getFilteredSessions(tasksByAgent, agent.id)
                // When searching, skip agents where name matches but nothing else does
                // (they still need to be shown so user can start a new session)
                return (
                  <AgentAccordionItem
                    key={agent.id}
                    agent={agent}
                    chatSessions={agentChatSessions}
                    taskSessions={agentTaskSessions}
                    activeSessionId={activeSessionId}
                    onSelectChat={handleSelectSession}
                    onSelectTask={handleSelectTaskSession}
                    onSessionDeleted={handleSessionDeleted}
                  />
                )
              })}
            </Accordion>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
