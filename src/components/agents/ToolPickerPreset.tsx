import { TOOL_PRESETS, type PresetKey } from '@/lib/agentToolPresets'

interface ToolPickerPresetProps {
  activePreset: PresetKey | null
  onSelect: (preset: PresetKey) => void
}

export function ToolPickerPreset({ activePreset, onSelect }: ToolPickerPresetProps) {
  return (
    <div className="flex flex-wrap gap-2">
      {(Object.entries(TOOL_PRESETS) as [PresetKey, typeof TOOL_PRESETS[PresetKey]][]).map(
        ([key, preset]) => {
          const Icon = preset.icon
          const isActive = activePreset === key
          return (
            <button
              key={key}
              type="button"
              onClick={() => onSelect(key)}
              className={[
                'flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-xs font-medium transition-colors',
                isActive
                  ? 'border-[var(--color-accent)] bg-[var(--color-accent)]/10 text-[var(--color-accent)]'
                  : 'border-[var(--color-border)] bg-[var(--color-surface-2)] text-[var(--color-muted)] hover:text-[var(--color-secondary)] hover:border-[var(--color-secondary)]',
              ].join(' ')}
            >
              <Icon size={13} />
              {preset.label}
            </button>
          )
        }
      )}
    </div>
  )
}
