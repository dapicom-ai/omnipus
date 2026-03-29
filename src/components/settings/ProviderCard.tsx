// ProviderCard — standalone single-provider configuration card.
// Designed for testability with explicit callback props.
// ProductionProvidersSection renders these in a list backed by TanStack Query.

import { useState } from 'react'
import { CheckCircle, XCircle, Eye, EyeSlash, ArrowCounterClockwise } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

type ProviderStatus = 'connected' | 'disconnected' | 'error' | 'unconfigured'

interface ProviderCardProps {
  id: string
  name: string
  status: ProviderStatus
  models?: string[]
  error?: string
  onSave: (args: { id: string; apiKey: string }) => Promise<void>
  onTest: (args: { id: string }) => Promise<{ success: boolean; models?: string[]; error?: string }>
}

export function ProviderCard({ id, name, status, models = [], error, onSave, onTest }: ProviderCardProps) {
  const [apiKey, setApiKey] = useState('')
  const [showKey, setShowKey] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [isTesting, setIsTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; error?: string } | null>(null)

  const connected = status === 'connected'

  const handleSave = async () => {
    setIsSaving(true)
    try {
      await onSave({ id, apiKey })
      setExpanded(false)
      setApiKey('')
    } finally {
      setIsSaving(false)
    }
  }

  const handleTest = async () => {
    setIsTesting(true)
    try {
      const result = await onTest({ id })
      setTestResult(result)
    } finally {
      setIsTesting(false)
    }
  }

  return (
    <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-[var(--color-secondary)]">{name}</span>
            {connected ? (
              <Badge variant="success" className="gap-1">
                <CheckCircle size={10} weight="fill" /> Connected
              </Badge>
            ) : status === 'error' ? (
              <Badge variant="error" className="gap-1">
                <XCircle size={10} weight="fill" /> Error
              </Badge>
            ) : (
              <Badge variant="muted">Not configured</Badge>
            )}
          </div>
          {models.length > 0 && (
            <p className="text-[10px] text-[var(--color-muted)] mt-0.5 font-mono">
              {models.slice(0, 3).join(', ')}
            </p>
          )}
          {error && <p className="text-[10px] text-[var(--color-error)] mt-0.5">{error}</p>}
          {testResult && (
            <p className={cn('text-[10px] mt-0.5', testResult.success ? 'text-[var(--color-success)]' : 'text-[var(--color-error)]')}>
              {testResult.success ? 'Connection successful' : testResult.error ?? 'Connection failed'}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {connected && (
            <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={handleTest} disabled={isTesting}>
              {isTesting ? <ArrowCounterClockwise size={12} className="animate-spin" /> : 'Test'}
            </Button>
          )}
          <Button variant="outline" size="sm" className="h-7 px-3 text-xs" onClick={() => setExpanded((v) => !v)}>
            {connected ? 'Edit' : 'Configure'}
          </Button>
        </div>
      </div>
      {expanded && (
        <div className="border-t border-[var(--color-border)] px-4 py-4 space-y-3 bg-[var(--color-surface-2)]">
          <div className="relative">
            <Input
              type={showKey ? 'text' : 'password'}
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="API Key"
              className="pr-9 font-mono text-xs"
              autoComplete="off"
              aria-label="API Key"
            />
            <button
              type="button"
              onClick={() => setShowKey((v) => !v)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--color-muted)]"
              aria-label={showKey ? 'Hide key' : 'Show key'}
            >
              {showKey ? <EyeSlash size={14} /> : <Eye size={14} />}
            </button>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" size="sm" onClick={() => setExpanded(false)}>Cancel</Button>
            <Button size="sm" onClick={handleSave} disabled={!apiKey.trim() || isSaving}>
              {isSaving ? 'Saving...' : 'Save & Connect'}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
