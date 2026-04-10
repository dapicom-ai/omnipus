// PolicyApprovalsSection — admin-only policy change approval panel
// Full implementation: Phase 3B (policy change approval with admin approval)
//
// Traces to: wave3-skill-ecosystem-spec.md line 846 (Test #16: RBAC — admin-only REST endpoints)

import { ShieldCheck, ArrowRight } from '@phosphor-icons/react'
import { Separator } from '@/components/ui/separator'

export function PolicyApprovalsSection() {
  return (
    <section className="space-y-4">
      <div>
        <h3 className="text-xs font-semibold uppercase tracking-wider" style={{ color: 'var(--color-muted)' }}>
          Policy Change Approvals
        </h3>
        <p className="text-xs mt-0.5" style={{ color: 'var(--color-muted)' }}>
          Review and approve security policy changes proposed by agents. Only admins can approve policy changes.
        </p>
      </div>

      <div
        className="flex flex-col items-center justify-center gap-3 py-8 rounded-lg border border-dashed text-center"
        style={{ borderColor: 'var(--color-border)' }}
      >
        <ShieldCheck size={28} weight="duotone" style={{ color: 'var(--color-muted)' }} />
        <div>
          <p className="text-sm" style={{ color: 'var(--color-secondary)' }}>No pending approvals</p>
          <p className="text-xs mt-0.5" style={{ color: 'var(--color-muted)' }}>
            Policy change approvals will appear here when agents request security policy modifications.
          </p>
        </div>
      </div>

      <Separator style={{ opacity: 0.3 }} />

      <div>
        <h4 className="text-[10px] font-semibold uppercase tracking-wider mb-2" style={{ color: 'var(--color-muted)' }}>
          v1.0 Scope
        </h4>
        <ul className="space-y-1 text-xs" style={{ color: 'var(--color-muted)' }}>
          <li className="flex items-center gap-1.5">
            <ArrowRight size={10} />
            <code className="text-[10px] px-1 py-0.5 rounded" style={{ backgroundColor: 'var(--color-surface-2)' }}>exec_allowlist_add</code>
            — add command to execution allowlist
          </li>
          <li className="flex items-center gap-1.5">
            <ArrowRight size={10} />
            <code className="text-[10px] px-1 py-0.5 rounded" style={{ backgroundColor: 'var(--color-surface-2)' }}>exec_mode_change</code>
            — switch exec mode (prompt → auto → ask)
          </li>
        </ul>
      </div>
    </section>
  )
}
