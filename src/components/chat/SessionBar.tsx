import { useQuery } from '@tanstack/react-query'
import { Robot, Timer, CurrencyDollar, ArrowsClockwise, CaretDown, PencilSimpleLine } from '@phosphor-icons/react'
import { IconRenderer } from '@/components/shared/IconRenderer'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'
import { useChatStore } from '@/store/chat'
import { useUiStore } from '@/store/ui'
import { fetchAgents } from '@/lib/api'

function formatCost(cost: number): string {
  if (cost === 0) return '$0.00'
  if (cost < 0.001) return '<$0.001'
  return `$${cost.toFixed(4)}`
}

function formatTokens(tokens: number): string {
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}k`
  return tokens.toString()
}

export function SessionBar() {
  const { activeAgentId, setActiveSession, sessionTokens, sessionCost, isStreaming } = useChatStore()
  const { openSessionPanel } = useUiStore()

  const { data: agents = [], isError: agentsError } = useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
  })

  // Auto-select first agent if none is active
  if (agentsError) {
    return <span className="text-xs text-[var(--color-error)] px-2">Could not load agents</span>
  }

  const effectiveAgentId = activeAgentId || agents[0]?.id
  const activeAgent = agents.find((a) => a.id === effectiveAgentId)
  const chatAgents = agents.length <= 1 ? agents : agents.filter((a) => a.type !== 'system')

  const handleAgentSelect = (agentId: string) => {
    // Switch agent — start new session
    setActiveSession(null, agentId)
  }

  return (
    <div className="flex items-center gap-3 min-w-0 w-full">
      {/* Agent selector */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="flex items-center gap-2 h-7 px-2 text-xs font-medium max-w-[180px]"
          >
            <div
              className="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold shrink-0"
              style={{
                backgroundColor: activeAgent?.color ?? 'var(--color-surface-3)',
              }}
            >
              {activeAgent
                ? activeAgent.icon
                  ? <IconRenderer icon={activeAgent.icon} size={11} />
                  : activeAgent.name.charAt(0).toUpperCase()
                : <Robot size={11} />}
            </div>
            <span className="truncate">{activeAgent?.name ?? 'Select agent'}</span>
            <CaretDown size={11} className="shrink-0 opacity-60" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-48">
          {chatAgents.map((agent) => (
            <DropdownMenuItem
              key={agent.id}
              onClick={() => handleAgentSelect(agent.id)}
              className="flex items-center gap-2"
            >
              <div
                className="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold shrink-0"
                style={{ backgroundColor: agent.color ?? 'var(--color-surface-3)' }}
              >
                {agent.icon
                  ? <IconRenderer icon={agent.icon} size={11} />
                  : agent.name.charAt(0).toUpperCase()}
              </div>
              <span className="truncate">{agent.name}</span>
              {agent.id === effectiveAgentId && (
                <span className="ml-auto text-[var(--color-success)] text-[10px]">active</span>
              )}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* New Chat button */}
      <Button
        variant="ghost"
        size="sm"
        className="h-7 px-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-secondary)] gap-1"
        onClick={() => setActiveSession(null, effectiveAgentId ?? undefined)}
        title="New chat"
      >
        <PencilSimpleLine size={13} />
        <span className="hidden sm:inline">New</span>
      </Button>

      {/* Model */}
      {activeAgent?.model && (
        <span className="hidden sm:inline text-xs text-[var(--color-muted)] font-mono truncate max-w-[120px]">
          {activeAgent.model}
        </span>
      )}

      {/* Separator */}
      <div className="h-4 w-px bg-[var(--color-border)] hidden sm:block" />

      {/* Token counter */}
      <div className="hidden sm:flex items-center gap-1 text-xs text-[var(--color-muted)]">
        <ArrowsClockwise
          size={11}
          className={isStreaming ? 'animate-spin text-[var(--color-accent)]' : ''}
        />
        <span className={isStreaming ? 'text-[var(--color-secondary)]' : ''}>
          {formatTokens(sessionTokens)}
        </span>
      </div>

      {/* Cost */}
      <div className="hidden md:flex items-center gap-1 text-xs text-[var(--color-muted)]">
        <CurrencyDollar size={11} />
        <span>{formatCost(sessionCost)}</span>
      </div>

      {/* Sessions button */}
      <Button
        variant="ghost"
        size="sm"
        onClick={openSessionPanel}
        className="ml-auto h-7 px-2 text-xs gap-1"
      >
        <Timer size={13} />
        <span className="hidden sm:inline">Sessions</span>
      </Button>
    </div>
  )
}
