import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Circle, ChatCircle, ListChecks } from '@phosphor-icons/react'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { useUiStore } from '@/store/ui'
import { useChatStore } from '@/store/chat'
import { fetchAgents, fetchSessions, createSession } from '@/lib/api'
import type { Session } from '@/lib/api'

function sessionButtonClass(isActive: boolean): string {
  return isActive
    ? 'bg-[var(--color-accent)]/10 text-[var(--color-accent)]'
    : 'text-[var(--color-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-secondary)]'
}

export function SessionPanel() {
  const { sessionPanelOpen, closeSessionPanel, addToast } = useUiStore()
  const { activeSessionId, activeAgentId, setActiveSession, attachToSession } = useChatStore()
  const queryClient = useQueryClient()

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
    attachToSession(session.id, 'task', session.title)
    closeSessionPanel()
  }

  const handleNewSession = async (agentId: string) => {
    try {
      const session = await createSession(agentId)
      await queryClient.invalidateQueries({ queryKey: ['sessions'] })
      const agent = agents.find((a) => a.id === agentId)
      setActiveSession(session.id, agentId, agent?.type ?? null)
      closeSessionPanel()
    } catch (err) {
      addToast({
        message: `Could not create session: ${err instanceof Error ? err.message : 'Unknown error'}`,
        variant: 'error',
      })
    }
  }

  // Separate sessions by type. Legacy sessions without a type field default to 'chat'.
  const systemAgentId = agents.find((a) => a.type === 'system')?.id ?? 'omnipus-system'

  const chatSessions = sessions.filter((s) => !s.type || s.type === 'chat')
  const taskSessions = sessions.filter((s) => s.type === 'task')

  // Group chat sessions by agent for sub-accordions
  const chatByAgent = chatSessions.reduce<Record<string, Session[]>>((acc, s) => {
    const agentId = (s.agent_id === 'default' || s.agent_id === 'main') ? systemAgentId : s.agent_id
    if (!acc[agentId]) acc[agentId] = []
    acc[agentId].push(s)
    return acc
  }, {})

  // Show all agents that have sessions, including the system agent (Omnipus).
  // Previously the system agent was hidden, but it needs to be visible since users chat with it.
  const agentsWithSessions = new Set(Object.keys(chatByAgent))
  const visibleAgents = agents.filter((a) => agentsWithSessions.has(a.id) || a.type !== 'system')

  return (
    <Sheet open={sessionPanelOpen} onOpenChange={(open) => !open && closeSessionPanel()}>
      <SheetContent side="right" className="w-72 p-0 flex flex-col" overlay={false}>
        <SheetHeader className="px-4 pt-5 pb-3 border-b border-[var(--color-border)]">
          <SheetTitle>Sessions</SheetTitle>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto">
          {(agentsError || sessionsError) && (
            <div className="px-4 py-3 text-xs text-[var(--color-error)]">
              Could not load sessions.
            </div>
          )}

          {/* ── Conversations section ──────────────────────────────── */}
          <Accordion type="multiple" defaultValue={['conversations', ...(activeAgentId ? [activeAgentId] : [])]}>
            <AccordionItem value="conversations">
              <AccordionTrigger className="px-4 hover:no-underline">
                <div className="flex items-center gap-2 min-w-0">
                  <ChatCircle size={14} className="text-[var(--color-accent)] shrink-0" />
                  <span className="text-sm font-medium">Conversations</span>
                </div>
              </AccordionTrigger>
              <AccordionContent>
                {visibleAgents.length === 0 ? (
                  <div className="px-4 py-3 text-xs text-[var(--color-muted)]">
                    No agents configured yet.
                  </div>
                ) : (
                  <Accordion type="multiple" defaultValue={activeAgentId ? [activeAgentId] : []}>
                    {visibleAgents.map((agent) => {
                      const agentSessions = chatByAgent[agent.id] ?? []
                      return (
                        <AccordionItem key={agent.id} value={agent.id}>
                          <AccordionTrigger className="px-6 hover:no-underline">
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
                          <AccordionContent>
                            <div className="space-y-0.5 pb-1">
                              {agentSessions.map((session) => (
                                <button
                                  key={session.id}
                                  type="button"
                                  onClick={() => handleSelectSession(session.id, agent.id)}
                                  className={`w-full text-left px-10 py-2 text-xs transition-colors rounded-sm ${sessionButtonClass(session.id === activeSessionId)}`}
                                >
                                  <div className="flex items-center gap-2">
                                    {session.id === activeSessionId && (
                                      <Circle size={5} weight="fill" className="text-[var(--color-success)] shrink-0" />
                                    )}
                                    <span className="truncate">{session.title || 'Untitled Session'}</span>
                                  </div>
                                </button>
                              ))}
                              <button
                                type="button"
                                onClick={() => handleNewSession(agent.id)}
                                className="w-full text-left px-10 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] transition-colors flex items-center gap-1"
                              >
                                <Plus size={11} /> New session
                              </button>
                            </div>
                          </AccordionContent>
                        </AccordionItem>
                      )
                    })}
                  </Accordion>
                )}
              </AccordionContent>
            </AccordionItem>

            {/* ── Task Executions section ────────────────────────────── */}
            <AccordionItem value="task-executions">
              <AccordionTrigger className="px-4 hover:no-underline">
                <div className="flex items-center gap-2 min-w-0">
                  <ListChecks size={14} className="text-[var(--color-accent)] shrink-0" />
                  <span className="text-sm font-medium">Task Executions</span>
                  {taskSessions.length > 0 && (
                    <Badge variant="outline" className="text-[10px] ml-1 h-4 px-1">
                      {taskSessions.length}
                    </Badge>
                  )}
                </div>
              </AccordionTrigger>
              <AccordionContent>
                {taskSessions.length === 0 ? (
                  <div className="px-4 py-3 text-xs text-[var(--color-muted)]">
                    No task sessions yet.
                  </div>
                ) : (
                  <div className="space-y-0.5 pb-1">
                    {taskSessions.map((session) => {
                      const agent = agents.find((a) => a.id === session.agent_id)
                      return (
                        <button
                          key={session.id}
                          type="button"
                          onClick={() => handleSelectTaskSession(session)}
                          className={`w-full text-left px-4 py-2.5 text-xs transition-colors rounded-sm ${sessionButtonClass(session.id === activeSessionId)}`}
                        >
                          <div className="flex flex-col gap-0.5">
                            <div className="flex items-center gap-2 min-w-0">
                              <span className="truncate font-medium flex-1">
                                {session.title || 'Untitled Task'}
                              </span>
                              <Badge
                                variant="outline"
                                className={`text-[9px] h-4 px-1 shrink-0 ${
                                  session.status === 'archived' ? 'text-[var(--color-success)]' :
                                  session.status === 'interrupted' ? 'text-[var(--color-error)]' :
                                  'text-[var(--color-warning)]'
                                }`}
                              >
                                {session.status === 'archived' ? 'completed' :
                                 session.status === 'interrupted' ? 'failed' :
                                 'running'}
                              </Badge>
                            </div>
                            {agent && (
                              <span className="text-[10px] text-[var(--color-muted)] truncate">
                                {agent.name}
                              </span>
                            )}
                          </div>
                        </button>
                      )
                    })}
                  </div>
                )}
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </div>
      </SheetContent>
    </Sheet>
  )
}
