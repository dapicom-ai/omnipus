import { useState } from 'react'
import { MagnifyingGlass, CloudSlash } from '@phosphor-icons/react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

interface SkillBrowserProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SkillBrowser({ open, onOpenChange }: SkillBrowserProps) {
  const [query, setQuery] = useState('')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Browse Skills</DialogTitle>
          <DialogDescription>Search and install skills from ClawHub</DialogDescription>
        </DialogHeader>

        <div className="relative">
          <MagnifyingGlass
            size={14}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--color-muted)]"
          />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search skills..."
            className="pl-8 text-sm"
          />
        </div>

        <div className="flex flex-col items-center justify-center py-10 gap-3 text-center">
          <CloudSlash size={40} weight="thin" className="text-[var(--color-border)]" />
          <p className="text-sm text-[var(--color-muted)]">ClawHub search not yet available</p>
          <p className="text-xs text-[var(--color-muted)]">
            Install skills manually by placing a <span className="font-mono">SKILL.md</span> file in your skills directory.
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
