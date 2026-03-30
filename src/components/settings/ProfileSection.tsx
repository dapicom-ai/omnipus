import { useState, useEffect } from 'react'
import { FloppyDisk } from '@phosphor-icons/react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Slider } from '@/components/ui/slider'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { useUiStore } from '@/store/ui'
import { fetchUserContext, updateUserContext } from '@/lib/api'

const TIMEZONES = [
  'UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Anchorage',
  'Pacific/Honolulu',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Moscow',
  'Asia/Dubai',
  'Asia/Kolkata',
  'Asia/Bangkok',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Seoul',
  'Australia/Sydney',
  'Pacific/Auckland',
]

const LANGUAGES = [
  { value: 'en', label: 'English' },
  { value: 'de', label: 'German' },
  { value: 'es', label: 'Spanish' },
  { value: 'fr', label: 'French' },
  { value: 'zh', label: 'Chinese' },
  { value: 'ja', label: 'Japanese' },
]

const LS_PREFIX = 'omnipus_pref_'

function loadPref<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(`${LS_PREFIX}${key}`)
    return raw !== null ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

function savePref(key: string, value: unknown) {
  localStorage.setItem(`${LS_PREFIX}${key}`, JSON.stringify(value))
}

export function ProfileSection() {
  const { addToast } = useUiStore()

  const [name, setName] = useState(() => loadPref<string>('name', ''))
  const [timezone, setTimezone] = useState(() => loadPref<string>('timezone', 'UTC'))
  const [language, setLanguage] = useState(() => loadPref<string>('language', 'en'))
  const [fontSize, setFontSize] = useState(() => loadPref<number>('font_size', 14))
  const [userContent, setUserContent] = useState('')

  const { data: userContextData } = useQuery({
    queryKey: ['user-context'],
    queryFn: fetchUserContext,
  })

  useEffect(() => {
    if (userContextData) {
      setUserContent(userContextData.content)
    }
  }, [userContextData])

  const { mutate: saveUserContext, isPending: isSavingContext } = useMutation({
    mutationFn: () => updateUserContext(userContent),
    onSuccess: () => addToast({ message: 'Workspace context saved', variant: 'success' }),
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  // Keep document font-size in sync for preview
  useEffect(() => {
    document.documentElement.style.setProperty('--user-font-size', `${fontSize}px`)
  }, [fontSize])

  function handleSave() {
    savePref('name', name)
    savePref('timezone', timezone)
    savePref('language', language)
    savePref('font_size', fontSize)
    addToast({ message: 'Preferences saved', variant: 'success' })
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="font-headline font-bold text-base text-[var(--color-secondary)]">Profile & Preferences</h2>
          <p className="text-xs text-[var(--color-muted)] mt-0.5">
            Personal preferences stored in this browser.
          </p>
        </div>
        <Button size="sm" onClick={handleSave} className="gap-1.5">
          <FloppyDisk size={13} weight="bold" />
          Save
        </Button>
      </div>

      {/* Identity */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Identity</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div className="flex items-center justify-between gap-4">
            <Label htmlFor="pref-name" className="text-sm text-[var(--color-secondary)] shrink-0">
              Display name
            </Label>
            <Input
              id="pref-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Your name"
              className="h-8 text-xs max-w-xs"
            />
          </div>
        </div>
      </section>

      {/* Locale */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Locale</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Timezone</p>
            </div>
            <Select value={timezone} onValueChange={setTimezone}>
              <SelectTrigger className="w-[200px] h-8 text-xs font-mono">
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="max-h-60">
                {TIMEZONES.map((tz) => (
                  <SelectItem key={tz} value={tz} className="text-xs font-mono">
                    {tz}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Separator />

          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Language</p>
            </div>
            <Select value={language} onValueChange={setLanguage}>
              <SelectTrigger className="w-[160px] h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {LANGUAGES.map((lang) => (
                  <SelectItem key={lang.value} value={lang.value} className="text-xs">
                    {lang.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      {/* Appearance */}
      <section className="space-y-3">
        <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">Appearance</h3>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-[var(--color-secondary)]">Theme</p>
              <p className="text-xs text-[var(--color-muted)]">Omnipus uses the Sovereign Deep dark theme.</p>
            </div>
            <span className="text-sm text-[var(--color-secondary)]">
              Dark <span className="text-xs text-[var(--color-muted)]">(only dark theme is supported)</span>
            </span>
          </div>

          <Separator />

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <p className="text-sm text-[var(--color-secondary)]">Font size</p>
              <span className="text-xs font-mono text-[var(--color-muted)]">{fontSize}px</span>
            </div>
            <Slider
              min={12}
              max={20}
              step={1}
              value={[fontSize]}
              onValueChange={([v]) => setFontSize(v)}
              className="w-full"
            />
            <div className="flex justify-between text-[10px] text-[var(--color-muted)]">
              <span>12px</span>
              <span>20px</span>
            </div>
          </div>
        </div>
      </section>

      {/* Workspace Context (USER.md) */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-semibold text-[var(--color-muted)] uppercase tracking-wider">
            Workspace Context
          </h3>
          <Button
            size="sm"
            variant="outline"
            onClick={() => saveUserContext()}
            disabled={isSavingContext}
            className="gap-1.5"
          >
            <FloppyDisk size={13} weight="bold" />
            {isSavingContext ? 'Saving...' : 'Save'}
          </Button>
        </div>
        <div className="rounded-lg border border-[var(--color-border)] bg-[var(--color-surface-1)] p-4 space-y-3">
          <p className="text-xs text-[var(--color-muted)]">
            Shared context available to all agents — your role, preferences, and project information.
          </p>
          <Textarea
            value={userContent}
            onChange={(e) => setUserContent(e.target.value)}
            placeholder={"# About Me\n\nDescribe your role, expertise, and preferences..."}
            rows={8}
            className="text-xs font-mono resize-none"
          />
        </div>
      </section>
    </div>
  )
}
