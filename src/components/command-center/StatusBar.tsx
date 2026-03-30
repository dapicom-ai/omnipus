import { useQuery } from '@tanstack/react-query'
import { Circle, Robot, Hash, CurrencyDollar } from '@phosphor-icons/react'
import { fetchGatewayStatus } from '@/lib/api'

export function StatusBar() {
  const { data: status, isError } = useQuery({
    queryKey: ['gateway-status'],
    queryFn: fetchGatewayStatus,
    refetchInterval: 15_000,
  })

  return (
    <div className="flex items-center gap-6 px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface-1)]">
      {/* Gateway status */}
      <div className="flex items-center gap-1.5 text-xs">
        <Circle
          size={7}
          weight="fill"
          className={status?.online ? 'text-[var(--color-success)]' : 'text-[var(--color-error)]'}
        />
        <span className="text-[var(--color-secondary)]">
          {isError ? 'Gateway unreachable' : `Gateway ${status?.online ? 'online' : 'offline'}`}
        </span>
      </div>

      <div className="h-4 w-px bg-[var(--color-border)]" />

      {/* Agents */}
      <div className="flex items-center gap-1.5 text-xs text-[var(--color-muted)]">
        <Robot size={13} />
        <span>{status?.agent_count ?? '—'} agents</span>
      </div>

      {/* Channels */}
      <div className="flex items-center gap-1.5 text-xs text-[var(--color-muted)]">
        <Hash size={13} />
        <span>{status?.channel_count ?? '—'} channels</span>
      </div>

      {/* Cost */}
      <div className="flex items-center gap-1.5 text-xs text-[var(--color-muted)]">
        <CurrencyDollar size={13} />
        <span>${(status?.daily_cost ?? 0).toFixed(4)} today</span>
      </div>

      {status?.version && (
        <>
          <div className="h-4 w-px bg-[var(--color-border)] ml-auto" />
          <span className="text-[10px] font-mono text-[var(--color-muted)]">v{status.version}</span>
        </>
      )}
    </div>
  )
}
