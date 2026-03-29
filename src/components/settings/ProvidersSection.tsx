import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  CheckCircle,
  XCircle,
  Eye,
  EyeSlash,
  ArrowCounterClockwise,
  Plus,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { fetchProviders, configureProvider, testProvider } from '@/lib/api'
import { useUiStore } from '@/store/ui'

const AVAILABLE_PROVIDERS = [
  { id: 'anthropic', display_name: 'Anthropic', hint: 'Starts with sk-ant-...' },
  { id: 'openai', display_name: 'OpenAI', hint: 'Starts with sk-...' },
  { id: 'google', display_name: 'Google Gemini', hint: 'API key from Google AI Studio' },
  { id: 'groq', display_name: 'Groq', hint: 'Starts with gsk_...' },
  { id: 'openrouter', display_name: 'OpenRouter', hint: 'Starts with sk-or-v1-...' },
]

export function ProvidersSection() {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const [expandedProvider, setExpandedProvider] = useState<string | null>(null)
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({})
  const [showKey, setShowKey] = useState<Record<string, boolean>>({})
  const [testing, setTesting] = useState<Record<string, boolean>>({})

  const { data: providers = [], isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: fetchProviders,
  })

  const { mutate: doConfigure } = useMutation({
    mutationFn: ({ id, key }: { id: string; key: string }) => configureProvider(id, key),
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      addToast({ message: 'Provider saved', variant: 'success' })
      setExpandedProvider(null)
      setApiKeys((prev) => ({ ...prev, [id]: '' }))
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const handleTest = async (id: string) => {
    setTesting((prev) => ({ ...prev, [id]: true }))
    try {
      const result = await testProvider(id)
      if (result.success) {
        addToast({ message: 'Connection successful', variant: 'success' })
        queryClient.invalidateQueries({ queryKey: ['providers'] })
      } else {
        addToast({ message: result.error ?? 'Connection failed', variant: 'error' })
      }
    } catch (err) {
      addToast({ message: (err as Error).message, variant: 'error' })
    } finally {
      setTesting((prev) => ({ ...prev, [id]: false }))
    }
  }

  // Merge configured providers with available ones
  const providerMap = new Map(providers.map((p) => [p.id, p]))

  return (
    <div className="space-y-4">
      <div>
        <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">Providers</h2>
        <p className="text-xs text-[var(--color-muted)] mt-0.5">
          API keys are stored encrypted in credentials.json — never in config.json.
        </p>
      </div>

      {isLoading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-14 rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] animate-pulse" />
          ))}
        </div>
      ) : (
        <div className="space-y-2">
          {AVAILABLE_PROVIDERS.map((providerDef) => {
            const configured = providerMap.get(providerDef.id)
            const isExpanded = expandedProvider === providerDef.id
            const connected = configured?.status === 'connected'

            return (
              <div
                key={providerDef.id}
                className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] overflow-hidden"
              >
                {/* Provider row */}
                <div className="flex items-center gap-3 px-4 py-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-[var(--color-secondary)]">
                        {providerDef.display_name}
                      </span>
                      {configured ? (
                        connected ? (
                          <Badge variant="success" className="gap-1">
                            <CheckCircle size={10} weight="fill" /> Connected
                          </Badge>
                        ) : (
                          <Badge variant="error" className="gap-1">
                            <XCircle size={10} weight="fill" /> Error
                          </Badge>
                        )
                      ) : (
                        <Badge variant="muted">Not configured</Badge>
                      )}
                    </div>
                    {configured?.models && configured.models.length > 0 && (
                      <p className="text-[10px] text-[var(--color-muted)] mt-0.5 font-mono">
                        {configured.models.slice(0, 3).join(', ')}{configured.models.length > 3 ? ` +${configured.models.length - 3}` : ''}
                      </p>
                    )}
                    {configured?.error && (
                      <p className="text-[10px] text-[var(--color-error)] mt-0.5">{configured.error}</p>
                    )}
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    {configured && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleTest(providerDef.id)}
                        disabled={testing[providerDef.id]}
                        className="h-7 px-2 text-xs"
                      >
                        {testing[providerDef.id] ? (
                          <ArrowCounterClockwise size={12} className="animate-spin" />
                        ) : 'Test'}
                      </Button>
                    )}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() =>
                        setExpandedProvider(isExpanded ? null : providerDef.id)
                      }
                      className="h-7 px-3 text-xs"
                    >
                      {configured ? 'Edit' : (
                        <><Plus size={11} /> Configure</>
                      )}
                    </Button>
                  </div>
                </div>

                {/* Expanded config form */}
                {isExpanded && (
                  <div className="border-t border-[var(--color-border)] px-4 py-4 space-y-3 bg-[var(--color-surface-2)]">
                    <div>
                      <label className="text-xs font-medium text-[var(--color-muted)] mb-1.5 block">
                        API Key
                      </label>
                      <div className="relative">
                        <Input
                          type={showKey[providerDef.id] ? 'text' : 'password'}
                          value={apiKeys[providerDef.id] ?? ''}
                          onChange={(e) =>
                            setApiKeys((prev) => ({ ...prev, [providerDef.id]: e.target.value }))
                          }
                          placeholder={providerDef.hint}
                          className="pr-9 font-mono text-xs"
                          autoComplete="off"
                        />
                        <button
                          type="button"
                          onClick={() =>
                            setShowKey((prev) => ({ ...prev, [providerDef.id]: !prev[providerDef.id] }))
                          }
                          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--color-muted)] hover:text-[var(--color-secondary)]"
                          aria-label={showKey[providerDef.id] ? 'Hide API key' : 'Show API key'}
                        >
                          {showKey[providerDef.id] ? <EyeSlash size={14} /> : <Eye size={14} />}
                        </button>
                      </div>
                    </div>
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setExpandedProvider(null)}
                      >
                        Cancel
                      </Button>
                      <Button
                        size="sm"
                        onClick={() =>
                          doConfigure({ id: providerDef.id, key: apiKeys[providerDef.id] ?? '' })
                        }
                        disabled={!apiKeys[providerDef.id]?.trim()}
                      >
                        Save & Connect
                      </Button>
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
