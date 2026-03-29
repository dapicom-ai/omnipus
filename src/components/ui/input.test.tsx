import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Input } from './input'

// test_input_focus_ring
// Traces to: wave0-brand-design-spec.md Scenario: Input shows Forge Gold focus ring (US-2 AC4, FR-004)
describe('Input — Forge Gold focus ring', () => {
  it('renders with Forge Gold focus ring class', () => {
    const { container } = render(<Input />)
    const input = container.querySelector('input')
    expect(input).not.toBeNull()
    // Input uses focus-visible:ring-[var(--color-accent)] = Forge Gold
    expect(input!.className).toContain('var(--color-accent)')
  })

  it('renders with dark background CSS variable', () => {
    const { container } = render(<Input />)
    const input = container.querySelector('input')
    // Input uses bg-[var(--color-surface-1)] = dark surface
    expect(input!.className).toContain('var(--color-surface-1)')
  })

  it('renders with Liquid Silver text CSS variable', () => {
    const { container } = render(<Input />)
    const input = container.querySelector('input')
    // Input uses text-[var(--color-secondary)] = Liquid Silver
    expect(input!.className).toContain('var(--color-secondary)')
  })

  it('renders as an input element', () => {
    const { container } = render(<Input placeholder="Enter text" />)
    const input = container.querySelector('input')
    expect(input).not.toBeNull()
    expect(input!.getAttribute('placeholder')).toBe('Enter text')
  })

  it('is disabled when disabled prop is set', () => {
    const { container } = render(<Input disabled />)
    expect(container.querySelector('input')).toBeDisabled()
  })
})
