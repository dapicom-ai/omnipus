'use client'

import * as React from 'react'
import { CaretUpDown, Check, Keyboard } from '@phosphor-icons/react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Input } from '@/components/ui/input'

interface ModelSelectorProps {
  models: string[]
  value: string
  onChange: (model: string) => void  // Named onChange (not onValueChange) since this supports free-text input, not just selection
  placeholder?: string
  disabled?: boolean
}

export function ModelSelector({ models, value, onChange, placeholder, disabled }: ModelSelectorProps) {
  const [open, setOpen] = React.useState(false)
  const [query, setQuery] = React.useState('')

  // Text input mode — no models available
  if (models.length === 0) {
    return (
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? 'Enter model slug (e.g. MiniMax-M2.7)'}
        disabled={disabled}
        className="font-mono text-sm"
      />
    )
  }

  // Combobox mode — searchable dropdown
  const displayValue = value || placeholder || 'Select a model...'
  const queryTrimmed = query.trim()
  const exactMatch = models.some((m) => m.toLowerCase() === queryTrimmed.toLowerCase())

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="flex w-full items-center justify-between h-10 rounded-md border px-3 py-2 text-sm transition-colors focus-visible:outline-none focus-visible:ring-1 disabled:cursor-not-allowed disabled:opacity-50"
          style={{
            borderColor: open ? 'var(--color-accent)' : 'var(--color-border)',
            backgroundColor: 'var(--color-surface-1)',
            color: value ? 'var(--color-secondary)' : 'var(--color-muted)',
          }}
        >
          <span className="truncate font-mono text-sm">{displayValue}</span>
          <CaretUpDown size={14} className="shrink-0 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0" align="start">
        <Command shouldFilter={true}>
          <CommandInput
            placeholder="Search models..."
            value={query}
            onValueChange={setQuery}
          />
          <CommandList>
            <CommandEmpty>No models found.</CommandEmpty>
            <CommandGroup>
              {models.map((model) => (
                <CommandItem
                  key={model}
                  value={model}
                  onSelect={() => {
                    onChange(model)
                    setOpen(false)
                    setQuery('')
                  }}
                >
                  <Check
                    size={14}
                    className="mr-2 shrink-0"
                    style={{ opacity: value === model ? 1 : 0, color: 'var(--color-accent)' }}
                  />
                  <span className="font-mono text-xs">{model}</span>
                </CommandItem>
              ))}
            </CommandGroup>
            {queryTrimmed && !exactMatch && (
              <CommandGroup>
                <CommandItem
                  value={`custom:${queryTrimmed}`}
                  onSelect={() => {
                    onChange(queryTrimmed)
                    setOpen(false)
                    setQuery('')
                  }}
                >
                  <Keyboard size={14} className="mr-2 shrink-0" style={{ color: 'var(--color-muted)' }} />
                  <span className="text-xs">
                    Use "<span className="font-mono" style={{ color: 'var(--color-accent)' }}>{queryTrimmed}</span>"
                  </span>
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
