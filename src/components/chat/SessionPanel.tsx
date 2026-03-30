import { useQuery } from '@tanstack/react-query'
import { Plus, Circle } from '@phosphor-icons/react'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from '@/components/ui/accordion'
import { useUiStore } from '@/store/ui'
import { useChatStore } from '@/store/chat'
import { fetchAgents, fetchSessions, createSession } from '@/lib/api'
import { useQueryClient } from '@tanstack/react-query'

export function SessionPanel() {
  const { sessionPanelOpen, closeSessionPanel, addToast } = useUiStore()
  const { activeSessionId, activeAgentId, setActiveSession } = useChatStore()
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
    setActiveSession(sessionId, agentId)
    closeSessionPanel()
  }

  const handleNewSession = async (agentId: string) => {
    try {
      const session = await createSession(agentId)
      await queryClient.invalidateQueries({ queryKey: ['sessions'] })
      setActiveSession(session.id, agentId)
      closeSessionPanel()
    } catch {
      addToast({ message: 'Could not create session', variant: 'error' })
    }
  }

  // Group sessions by agent
  const sessionsByAgent = sessions.reduce<Record<string, typeof sessions>>((acc, s) => {
    if (!acc[s.agent_id]) acc[s.agent_id] = []
    acc[s.agent_id].push(s)
    return acc
  }, {})

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
          <Accordion type="multiple" defaultValue={activeAgentId ? [activeAgentId] : []}>
            {(agents.length <= 1 ? agents : agents.filter((a) => a.type !== 'system'))
              .map((agent) => {
                const agentSessions = sessionsByAgent[agent.id] ?? []
                return (
                  <AccordionItem key={agent.id} value={agent.id}>
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
                    <AccordionContent>
                      <div className="space-y-0.5 pb-1">
                        {agentSessions.map((session) => (
                          <button
                            key={session.id}
                            type="button"
                            onClick={() => handleSelectSession(session.id, agent.id)}
                            className={`w-full text-left px-8 py-2 text-xs transition-colors rounded-sm ${
                              session.id === activeSessionId
                                ? 'bg-[var(--color-accent)]/10 text-[var(--color-accent)]'
                                : 'text-[var(--color-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-secondary)]'
                            }`}
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
                          className="w-full text-left px-8 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] transition-colors flex items-center gap-1"
                        >
                          <Plus size={11} /> New session
                        </button>
                      </div>
                    </AccordionContent>
                  </AccordionItem>
                )
              })}
          </Accordion>

          {agents.length === 0 && (
            <div className="px-4 py-8 text-center text-sm text-[var(--color-muted)]">
              No agents configured yet.
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
