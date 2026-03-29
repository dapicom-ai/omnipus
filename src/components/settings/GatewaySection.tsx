import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, ArrowsClockwise, FloppyDisk, CheckCircle } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { fetchConfig, updateConfig, rotateGatewayToken } from '@/lib/api'
import { useUiStore } from '@/store/ui'

export function GatewaySection() {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)

  const { data: config, isLoading } = useQuery({
    queryKey: ['config'],
    queryFn: fetchConfig,
  })

  const [bindAddress, setBindAddress] = useState('127.0.0.1')
  const [port, setPort] = useState('8080')
  const [authMode, setAuthMode] = useState<'none' | 'token'>('none')

  const [initialized, setInitialized] = useState(false)
  if (config && !initialized) {
    setBindAddress(config.gateway.bind_address)
    setPort(config.gateway.port.toString())
    setAuthMode(config.gateway.auth_mode)
    setInitialized(true)
  }

  const { mutate: doSave, isPending: isSaving } = useMutation({
    mutationFn: () =>
      updateConfig({
        gateway: {
          bind_address: bindAddress,
          port: parseInt(port, 10),
          auth_mode: authMode,
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      addToast({ message: 'Gateway settings saved. Restart required to apply.', variant: 'default' })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const { mutate: doRotate, isPending: isRotating } = useMutation({
    mutationFn: rotateGatewayToken,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      addToast({ message: 'Gateway token rotated', variant: 'success' })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const copyToken = async () => {
    const token = config?.gateway.token
    if (!token) return
    await navigator.clipboard.writeText(token)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (isLoading) return <div className="text-sm text-[var(--color-muted)]">Loading...</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">Gateway</h2>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Configure how the gateway listens. Restart required for changes to take effect.
          </p>
        </div>
        <Button size="sm" onClick={() => doSave()} disabled={isSaving} className="gap-1.5">
          <FloppyDisk size={13} weight="bold" />
          {isSaving ? 'Saving...' : 'Save'}
        </Button>
      </div>

      <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
        {/* Bind address */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-[var(--color-secondary)]">Bind address</p>
            <p className="text-xs text-[var(--color-muted)]">Where the gateway listens</p>
          </div>
          <Select value={bindAddress} onValueChange={setBindAddress}>
            <SelectTrigger className="w-[160px] h-8 text-xs font-mono">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="127.0.0.1">127.0.0.1 (localhost only)</SelectItem>
              <SelectItem value="0.0.0.0">0.0.0.0 (all interfaces)</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Port */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-[var(--color-secondary)]">Port</p>
          </div>
          <Input
            type="number"
            min="1024"
            max="65535"
            value={port}
            onChange={(e) => setPort(e.target.value)}
            className="w-24 h-8 text-xs font-mono"
          />
        </div>

        {/* Auth mode */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-[var(--color-secondary)]">Auth mode</p>
            <p className="text-xs text-[var(--color-muted)]">Require a bearer token for API access</p>
          </div>
          <Select value={authMode} onValueChange={(v) => setAuthMode(v as 'none' | 'token')}>
            <SelectTrigger className="w-[120px] h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">None</SelectItem>
              <SelectItem value="token">Bearer token</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Token management */}
        {authMode === 'token' && (
          <div className="pt-2 border-t border-[var(--color-border)] space-y-2">
            <div className="flex items-center justify-between">
              <p className="text-sm text-[var(--color-secondary)]">Gateway token</p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 px-2 gap-1 text-xs"
                  onClick={copyToken}
                  disabled={!config?.gateway.token}
                >
                  {copied ? <CheckCircle size={11} className="text-[var(--color-success)]" /> : <Copy size={11} />}
                  {copied ? 'Copied' : 'Copy'}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 px-2 gap-1 text-xs"
                  onClick={() => doRotate()}
                  disabled={isRotating}
                >
                  <ArrowsClockwise size={11} className={isRotating ? 'animate-spin' : ''} />
                  Rotate
                </Button>
              </div>
            </div>
            {config?.gateway.token && (
              <p className="font-mono text-[10px] text-[var(--color-muted)] truncate">
                {config.gateway.token.slice(0, 20)}...
              </p>
            )}
          </div>
        )}
      </div>

      {/* Status */}
      <div className="flex items-center gap-2 text-xs text-[var(--color-muted)]">
        <Badge variant="success" className="gap-1 text-[10px]">
          <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-success)]" /> Online
        </Badge>
        <span>Listening on {bindAddress}:{port}</span>
      </div>
    </div>
  )
}
