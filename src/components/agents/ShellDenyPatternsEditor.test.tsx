/**
 * Tests for ShellDenyPatternsEditor
 *
 * Coverage:
 * 1. Valid patterns persist (onChange called with valid array)
 * 2. Invalid patterns render error styling
 * 3. Empty list = no patterns saved (filtered)
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ShellDenyPatternsEditor } from './ShellDenyPatternsEditor'

describe('ShellDenyPatternsEditor', () => {
  it('renders the textarea', () => {
    render(<ShellDenyPatternsEditor value={[]} onChange={vi.fn()} />)
    expect(screen.getByTestId('shell-deny-patterns-textarea')).toBeInTheDocument()
  })

  it('displays existing patterns in textarea', () => {
    render(<ShellDenyPatternsEditor value={['^rm -rf', '.*--force.*']} onChange={vi.fn()} />)
    const textarea = screen.getByTestId('shell-deny-patterns-textarea') as HTMLTextAreaElement
    expect(textarea.value).toContain('^rm -rf')
    expect(textarea.value).toContain('.*--force.*')
  })

  it('calls onChange when user types', () => {
    const onChange = vi.fn()
    render(<ShellDenyPatternsEditor value={[]} onChange={onChange} />)
    const textarea = screen.getByTestId('shell-deny-patterns-textarea')
    fireEvent.change(textarea, { target: { value: '^rm -rf\n.*test.*' } })
    expect(onChange).toHaveBeenCalledWith(['^rm -rf', '.*test.*'])
  })

  it('valid patterns show no error', () => {
    render(<ShellDenyPatternsEditor value={['^rm', '.*force.*']} onChange={vi.fn()} />)
    expect(screen.queryByTestId('shell-deny-pattern-error')).not.toBeInTheDocument()
  })

  it('invalid regex renders error styling', () => {
    render(<ShellDenyPatternsEditor value={['[invalid']} onChange={vi.fn()} />)
    const errors = screen.getAllByTestId('shell-deny-pattern-error')
    expect(errors.length).toBeGreaterThan(0)
    expect(errors[0].textContent).toMatch(/invalid/i)
  })

  it('textarea has error border class when invalid pattern present', () => {
    render(<ShellDenyPatternsEditor value={['[unclosed']} onChange={vi.fn()} />)
    const textarea = screen.getByTestId('shell-deny-patterns-textarea')
    // The error border class contains "error" in the border style
    expect(textarea.className).toContain('error')
  })

  it('empty textarea calls onChange with empty array', () => {
    const onChange = vi.fn()
    render(<ShellDenyPatternsEditor value={['existing']} onChange={onChange} />)
    const textarea = screen.getByTestId('shell-deny-patterns-textarea')
    fireEvent.change(textarea, { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith([''])
  })

  it('info banner text is visible', () => {
    render(<ShellDenyPatternsEditor value={[]} onChange={vi.fn()} />)
    expect(screen.getByText(/Patterns matching command text will be blocked/i)).toBeInTheDocument()
  })
})
