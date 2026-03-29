import { useEffect, useCallback } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { motion, AnimatePresence } from 'framer-motion'
import {
  ChatCircle,
  Gauge,
  Robot,
  PuzzlePiece,
  Gear,
  PushPin,
  PushPinSlash,
} from '@phosphor-icons/react'
import { useSidebarStore } from '@/store/sidebar'
import { cn } from '@/lib/utils'
import avatarUrl from '@/assets/logo/omnipus-avatar.svg?url'

// FR-015/FR-016: spec breakpoint is 768px — matches Tailwind's `md` prefix
const PHONE_BREAKPOINT = 768

const NAV_ITEMS = [
  { to: '/', label: 'Chat', Icon: ChatCircle },
  { to: '/command-center', label: 'Command Center', Icon: Gauge },
  { to: '/agents', label: 'Agents', Icon: Robot },
  { to: '/skills', label: 'Skills & Tools', Icon: PuzzlePiece },
] as const

// US-5: Sidebar — overlay default, pin option, Framer Motion, Zustand
export function Sidebar() {
  const { isOpen, isPinned, close, toggle, togglePin, unpin } = useSidebarStore()
  const location = useLocation()

  // US-5: Cmd+B / Ctrl+B keyboard shortcut + Escape to close
  const handleKeydown = useCallback(
    (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'b') {
        e.preventDefault()
        toggle()
      }
      if (e.key === 'Escape' && isOpen && !isPinned) {
        close()
      }
    },
    [toggle, close, isOpen, isPinned]
  )

  // FR-016: Unpin when viewport drops below 768px so sidebar is reachable on mobile
  const handleResize = useCallback(() => {
    if (window.innerWidth < PHONE_BREAKPOINT && isPinned) {
      unpin()
    }
  }, [isPinned, unpin])

  useEffect(() => {
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [handleKeydown])

  useEffect(() => {
    window.addEventListener('resize', handleResize)
    // Run once on mount in case the component mounts at phone width while pinned
    handleResize()
    return () => window.removeEventListener('resize', handleResize)
  }, [handleResize])

  const isVisible = isPinned || isOpen

  // Sidebar content shared between pinned and overlay modes
  const sidebarContent = (
    <nav className="flex h-full flex-col">
      {/* Brand mark */}
      <div className="flex items-center gap-3 px-4 py-5 border-b border-[var(--color-border)]">
        <img
          src={avatarUrl}
          alt="Omnipus"
          className="h-8 w-8 flex-shrink-0"
        />
        <span className="font-headline text-lg font-bold text-[var(--color-secondary)]">
          Omnipus
        </span>
      </div>

      {/* Primary nav */}
      <div className="flex-1 overflow-y-auto py-3">
        {NAV_ITEMS.map(({ to, label, Icon }) => {
          const isActive = location.pathname === to
          return (
            <Link
              key={to}
              to={to}
              onClick={() => {
                // US-5: overlay mode — close on nav item click
                if (!isPinned) close()
              }}
              className={cn(
                'flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors',
                isActive
                  ? 'bg-[var(--color-surface-2)] text-[var(--color-accent)] font-medium'
                  : 'text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-secondary)]'
              )}
            >
              <Icon
                size={18}
                weight={isActive ? 'fill' : 'regular'}
                className={isActive ? 'text-[var(--color-accent)]' : ''}
              />
              {label}
            </Link>
          )
        })}
      </div>

      {/* Bottom: Settings + Pin toggle */}
      <div className="border-t border-[var(--color-border)] py-3">
        <Link
          to="/settings"
          onClick={() => { if (!isPinned) close() }}
          className={cn(
            'flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm transition-colors',
            location.pathname === '/settings'
              ? 'bg-[var(--color-surface-2)] text-[var(--color-accent)] font-medium'
              : 'text-[var(--color-secondary)] hover:bg-[var(--color-surface-2)]'
          )}
        >
          <Gear
            size={18}
            weight={location.pathname === '/settings' ? 'fill' : 'regular'}
          />
          Settings
        </Link>

        {/* FR-015: Pin toggle — hidden below 768px (Tailwind md = 768px, not sm = 640px) */}
        <button
          onClick={togglePin}
          title={isPinned ? 'Unpin sidebar' : 'Pin sidebar'}
          className="hidden md:flex items-center gap-3 px-4 py-2.5 mx-2 rounded-lg text-sm text-[var(--color-muted)] hover:bg-[var(--color-surface-2)] hover:text-[var(--color-secondary)] transition-colors w-[calc(100%-16px)]"
        >
          {isPinned ? <PushPinSlash size={18} /> : <PushPin size={18} />}
          {isPinned ? 'Unpin sidebar' : 'Pin sidebar'}
        </button>
      </div>
    </nav>
  )

  return (
    <>
      {/* FR-015: Pinned mode — permanent panel, hidden below 768px (md breakpoint) */}
      {isPinned && (
        <aside
          className="hidden md:flex flex-col h-full flex-shrink-0 bg-[var(--color-surface-1)] border-r border-[var(--color-border)]"
          style={{ width: 'var(--spacing-sidebar)' }}
        >
          {sidebarContent}
        </aside>
      )}

      {/* US-5: Overlay mode — slides in from left, no backdrop dim */}
      <AnimatePresence>
        {isVisible && !isPinned && (
          <motion.aside
            initial={{ x: '-100%' }}
            animate={{ x: 0 }}
            exit={{ x: '-100%' }}
            transition={{ type: 'tween', duration: 0.22, ease: [0.4, 0, 0.2, 1] }}
            className="fixed left-0 top-0 z-40 flex h-full flex-col bg-[var(--color-surface-1)] border-r border-[var(--color-border)] shadow-2xl"
            style={{ width: 'var(--spacing-sidebar)' }}
          >
            {sidebarContent}
          </motion.aside>
        )}
      </AnimatePresence>

      {/* US-5: Click-outside overlay dismiss — no background dimming */}
      <AnimatePresence>
        {isOpen && !isPinned && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-30"
            onClick={close}
            aria-hidden="true"
          />
        )}
      </AnimatePresence>
    </>
  )
}
