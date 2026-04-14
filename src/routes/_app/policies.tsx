import { createFileRoute, Navigate } from '@tanstack/react-router'

// The Policies page has been merged into Settings > Security.
// This route exists only for backward compatibility with bookmarks/links.
function PoliciesRedirect() {
  return <Navigate to="/settings" replace />
}

export const Route = createFileRoute('/_app/policies')({
  component: PoliciesRedirect,
})
