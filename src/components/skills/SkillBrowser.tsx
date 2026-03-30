import { CloudSlash } from '@phosphor-icons/react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'

interface SkillBrowserProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SkillBrowser({ open, onOpenChange }: SkillBrowserProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Browse Skills</DialogTitle>
          <DialogDescription>Install skills from the ClawHub registry</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center justify-center py-10 gap-3 text-center">
          <CloudSlash size={40} weight="thin" className="text-[var(--color-border)]" />
          <p className="text-sm text-[var(--color-muted)]">ClawHub registry not yet available</p>
          <p className="text-xs text-[var(--color-muted)]">
            Install skills manually by placing a <span className="font-mono">SKILL.md</span> file in your skills directory.
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
