import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Card, CardHeader, CardTitle, CardContent } from './card'

// test_card_surface_color
// Traces to: wave0-brand-design-spec.md Scenario: Card uses elevated dark surface (US-2 AC3, FR-004)
describe('Card — elevated dark surface', () => {
  it('renders with surface-1 background CSS variable', () => {
    const { container } = render(<Card>Card content</Card>)
    const card = container.firstChild as HTMLElement
    // Card uses bg-[var(--color-surface-1)] (elevated surface ~#111113)
    expect(card.className).toContain('var(--color-surface-1)')
  })

  it('renders with Liquid Silver text CSS variable', () => {
    const { container } = render(<Card>Card content</Card>)
    const card = container.firstChild as HTMLElement
    // Card uses text-[var(--color-secondary)] = Liquid Silver
    expect(card.className).toContain('var(--color-secondary)')
  })

  it('renders with subtle border', () => {
    const { container } = render(<Card>Card content</Card>)
    const card = container.firstChild as HTMLElement
    // Card has border styling
    expect(card.className).toContain('border')
  })

  it('renders children correctly', () => {
    const { getByText } = render(
      <Card>
        <CardHeader>
          <CardTitle>Test Title</CardTitle>
        </CardHeader>
        <CardContent>Test Body</CardContent>
      </Card>
    )
    expect(getByText('Test Title')).toBeTruthy()
    expect(getByText('Test Body')).toBeTruthy()
  })

  it('CardTitle uses headline font class', () => {
    const { container } = render(<CardTitle>Title</CardTitle>)
    const title = container.firstChild as HTMLElement
    expect(title.className).toContain('font-headline')
  })
})
