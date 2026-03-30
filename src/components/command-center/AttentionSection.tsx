import { CheckCircle, Warning, Terminal } from '@phosphor-icons/react'
import { useQuery } from '@tanstack/react-query'
import { useChatStore } from '@/store/chat'
import { fetchProviders } from '@/lib/api'
import { cn } from '@/lib/utils'

export function AttentionSection() {
  const allApprovals = useChatStore((s) => s.pendingApprovals)
  const pendingApprovals = allApprovals.filter((a) => a.status === 'pending')
  const { data: providers = [], isLoading: providersLoading, isError: providersError } = useQuery({ queryKey: ['providers'], queryFn: fetchProviders })

  // Only show "no provider" warning when the query has settled without error
  const noProviders = !providersLoading && !providersError && (
    providers.length === 0 || providers.every((p) => p.status !== 'connected')
  )

  const hasItems = pendingApprovals.length > 0 || noProviders

  if (!hasItems) {
    return (
      <div className="flex items-center gap-2 px-4 py-2.5 border-b border-[var(--color-border)] text-xs text-[var(--color-muted)]">
        <CheckCircle size={13} className="text-[var(--color-success)]" />
        <span>All clear — no items need your attention</span>
      </div>
    )
  }

  return (
    <div className="border-b border-[var(--color-border)] px-4 py-2.5 space-y-1.5">
      {noProviders && (
        <AttentionItem icon={<Warning size={13} />} label="No provider configured — agents cannot run tasks" />
      )}
      {pendingApprovals.map((approval) => (
        <AttentionItem
          key={approval.id}
          icon={<Terminal size={13} />}
          label={`Exec approval pending: ${approval.command}`}
        />
      ))}
    </div>
  )
}

function AttentionItem({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div
      className={cn(
        'flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs',
        'border border-[var(--color-accent)]/40 bg-[var(--color-accent)]/5',
        'text-[var(--color-accent)]',
      )}
    >
      {icon}
      <span className="truncate">{label}</span>
    </div>
  )
}
