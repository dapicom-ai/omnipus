import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fetchChannels, updateConfig } from '@/lib/api'
import { useUiStore } from '@/store/ui'

type DmPolicy = 'allow' | 'deny' | 'known_only'

interface ChannelRoute {
  id: string
  name: string
  allow_from: string
  dm_policy: DmPolicy
}

export function RoutingSection() {
  const { addToast } = useUiStore()
  const [isSaving, setIsSaving] = useState(false)
  const [routes, setRoutes] = useState<ChannelRoute[]>([])

  const { data: channels, isLoading } = useQuery({
    queryKey: ['channels'],
    queryFn: fetchChannels,
  })

  useEffect(() => {
    if (!channels) return
    setRoutes(
      channels.map((ch) => ({
        id: ch.id,
        name: ch.name,
        allow_from: '',
        dm_policy: 'allow' as DmPolicy,
      }))
    )
  }, [channels])

  function updateRoute(id: string, field: keyof ChannelRoute, value: string) {
    setRoutes((prev) =>
      prev.map((r) => (r.id === id ? { ...r, [field]: value } : r))
    )
  }

  async function handleSave() {
    setIsSaving(true)
    try {
      // Persist routing rules as channel config entries
      const channelConfig: Record<string, { allow_from: string[]; dm_policy: string }> = {}
      for (const r of routes) {
        channelConfig[r.id] = {
          allow_from: r.allow_from
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
          dm_policy: r.dm_policy,
        }
      }
      await updateConfig({ channels: channelConfig } as Parameters<typeof updateConfig>[0])
      addToast({ message: 'Routing rules saved', variant: 'success' })
    } catch (err) {
      addToast({
        message: err instanceof Error ? err.message : 'Failed to save routing rules',
        variant: 'error',
      })
    } finally {
      setIsSaving(false)
    }
  }

  if (isLoading) return <div className="text-sm text-[var(--color-muted)]">Loading...</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">Routing & Policies</h2>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Control which users can send messages per channel and how DMs are handled.
          </p>
        </div>
        <Button size="sm" onClick={handleSave} disabled={isSaving} className="gap-1.5">
          <FloppyDisk size={13} weight="bold" />
          {isSaving ? 'Saving...' : 'Save'}
        </Button>
      </div>

      {routes.length === 0 ? (
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-8 text-center">
          <p className="text-sm text-[var(--color-muted)]">No channels configured.</p>
          <p className="text-xs text-[var(--color-muted)] mt-1">Enable channels in the Providers tab first.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-[var(--color-border)] overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="border-[var(--color-border)] hover:bg-transparent">
                <TableHead className="text-xs text-[var(--color-muted)]">Channel</TableHead>
                <TableHead className="text-xs text-[var(--color-muted)]">
                  Allow From
                  <span className="ml-1 font-normal">(comma-separated IDs)</span>
                </TableHead>
                <TableHead className="text-xs text-[var(--color-muted)]">DM Policy</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {routes.map((route) => (
                <TableRow key={route.id} className="border-[var(--color-border)]">
                  <TableCell className="text-sm text-[var(--color-secondary)] font-medium">
                    {route.name}
                  </TableCell>
                  <TableCell>
                    <Input
                      value={route.allow_from}
                      onChange={(e) => updateRoute(route.id, 'allow_from', e.target.value)}
                      placeholder="Leave empty to allow all"
                      className="h-7 text-xs font-mono w-full max-w-xs"
                    />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={route.dm_policy}
                      onValueChange={(v) => updateRoute(route.id, 'dm_policy', v)}
                    >
                      <SelectTrigger className="w-[130px] h-7 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="allow">Allow all</SelectItem>
                        <SelectItem value="known_only">Known users only</SelectItem>
                        <SelectItem value="deny">Deny all</SelectItem>
                      </SelectContent>
                    </Select>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <p className="text-xs text-[var(--color-muted)]">
        Changes take effect immediately for new messages. Existing sessions are not affected.
      </p>
    </div>
  )
}
