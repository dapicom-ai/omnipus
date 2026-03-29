import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Button } from './button'

// test_button_default_variant_colors
// Traces to: wave0-brand-design-spec.md Scenario: Default button uses Forge Gold (US-2 AC1, FR-004)
describe('Button — default variant (Forge Gold)', () => {
  it('renders with Forge Gold background CSS variable class', () => {
    const { container } = render(<Button>Click me</Button>)
    const btn = container.querySelector('button')
    expect(btn).not.toBeNull()
    // The default variant uses bg-[var(--color-accent)] which maps to Forge Gold.
    expect(btn!.className).toContain('var(--color-accent)')
  })

  it('renders with Deep Space Black text class', () => {
    const { container } = render(<Button>Click me</Button>)
    const btn = container.querySelector('button')
    expect(btn!.className).toContain('var(--color-primary)')
  })

  it('renders button text', () => {
    render(<Button>Click me</Button>)
    expect(screen.getByRole('button')).toHaveTextContent('Click me')
  })

  it('default variant has no explicit variant class for destructive', () => {
    const { container } = render(<Button>Default</Button>)
    // Must NOT use the error color for default variant.
    expect(container.querySelector('button')!.className).not.toContain('var(--color-error)')
  })
})

// test_button_destructive_variant
// Traces to: wave0-brand-design-spec.md Scenario: Destructive button uses Ruby (US-2 AC2, FR-004)
describe('Button — destructive variant (Ruby / #EF4444)', () => {
  it('renders with error color CSS variable class', () => {
    const { container } = render(<Button variant="destructive">Delete</Button>)
    const btn = container.querySelector('button')
    expect(btn).not.toBeNull()
    // Destructive variant uses bg-[var(--color-error)] which maps to Ruby (#EF4444).
    expect(btn!.className).toContain('var(--color-error)')
  })

  it('does not use Forge Gold class for destructive variant', () => {
    const { container } = render(<Button variant="destructive">Delete</Button>)
    // Destructive must NOT use the accent (Forge Gold) background.
    expect(container.querySelector('button')!.className).not.toContain(
      'bg-[var(--color-accent)]'
    )
  })
})

describe('Button — disabled state', () => {
  it('is disabled when disabled prop is set', () => {
    render(<Button disabled>Disabled</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })
})
