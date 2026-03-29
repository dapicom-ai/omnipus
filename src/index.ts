// @omnipus/ui — Sovereign Deep component library
// Shared across Open Source (go:embed), Electron Desktop, and SaaS variants

// UI primitives (shadcn/ui themed for Omnipus)
export { Button, buttonVariants } from './components/ui/button'
export type { ButtonProps } from './components/ui/button'
export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardDescription,
  CardContent,
} from './components/ui/card'
export { Input } from './components/ui/input'
export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
} from './components/ui/dialog'

// Layout
export { AppShell } from './components/layout/AppShell'
export { Sidebar } from './components/layout/Sidebar'

// State
export { useSidebarStore } from './store/sidebar'

// Utilities
export { cn } from './lib/utils'
