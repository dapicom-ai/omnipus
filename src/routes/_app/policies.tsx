import { createFileRoute, Navigate } from '@tanstack/react-router'
import { useAuthStore } from '@/store/auth'
import { PolicyApprovalsSection } from '@/components/settings/PolicyApprovalsSection'

// Top-level /policies route. Mirrors the Settings > Policy Approvals tab
// so admins can deep-link to the policy approval queue without going through
// Settings. Non-admins are redirected home — the underlying REST endpoint is
// also admin-gated, so viewing the page would be useless for them.
function PoliciesScreen() {
  const role = useAuthStore((s) => s.role)
  if (role !== 'admin') {
    return <Navigate to="/" replace />
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <div className="mb-6">
        <h1 className="font-headline text-2xl font-bold text-[var(--color-secondary)]">Policies</h1>
        <p className="text-sm text-[var(--color-muted)] mt-0.5">
          Review and approve security policy changes proposed by agents.
        </p>
      </div>
      <PolicyApprovalsSection />
    </div>
  )
}

export const Route = createFileRoute('/_app/policies')({
  component: PoliciesScreen,
})
