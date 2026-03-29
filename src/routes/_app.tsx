import { createFileRoute } from '@tanstack/react-router'
import { AppShell } from '@/components/layout/AppShell'

// Pathless layout route — wraps all app screens in AppShell
// Landing page (/landing) is a sibling, NOT nested here, so it renders without the shell
export const Route = createFileRoute('/_app')({
  component: AppShell,
})
