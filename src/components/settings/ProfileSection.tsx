import { useState, useEffect } from 'react'
import { FloppyDisk } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Slider } from '@/components/ui/slider'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { useUiStore } from '@/store/ui'

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

type Theme = 'dark' | 'light' | 'system'

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
  const [theme, setTheme] = useState<Theme>(() => loadPref<Theme>('theme', 'dark'))
  const [fontSize, setFontSize] = useState(() => loadPref<number>('font_size', 14))

  // Keep document font-size in sync for preview
  useEffect(() => {
    document.documentElement.style.setProperty('--user-font-size', `${fontSize}px`)
  }, [fontSize])

  function handleThemeChange(value: Theme) {
    if (value !== 'dark') {
      addToast({ message: `${value === 'light' ? 'Light' : 'System'} theme — coming soon`, variant: 'default' })
      return
    }
    setTheme(value)
  }

  function handleSave() {
    savePref('name', name)
    savePref('timezone', timezone)
    savePref('language', language)
    savePref('theme', theme)
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
              <p className="text-xs text-[var(--color-muted)]">Dark is the default. Light and System are coming soon.</p>
            </div>
            <Select value={theme} onValueChange={(v) => handleThemeChange(v as Theme)}>
              <SelectTrigger className="w-[120px] h-8 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dark">Dark</SelectItem>
                <SelectItem value="light">Light</SelectItem>
                <SelectItem value="system">System</SelectItem>
              </SelectContent>
            </Select>
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
    </div>
  )
}
