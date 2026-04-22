import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, act, fireEvent } from '@testing-library/react'
import { SaveStatus, useSaveStatus } from './SaveStatus'

// =====================================================================
// SaveStatus component — all 4 states
// =====================================================================

describe('SaveStatus component', () => {
  it('renders nothing when state is idle', () => {
    const { container } = render(<SaveStatus state="idle" />)
    expect(container.firstChild).toBeNull()
  })

  it('renders "Saving…" text with spinner when state is saving', () => {
    render(<SaveStatus state="saving" />)
    expect(screen.getByText(/saving/i)).toBeInTheDocument()
  })

  it('renders "Saved" text when state is saved', () => {
    render(<SaveStatus state="saved" />)
    expect(screen.getByText(/saved/i)).toBeInTheDocument()
  })

  it('renders "Save failed" text when state is error', () => {
    render(<SaveStatus state="error" />)
    expect(screen.getByText(/save failed/i)).toBeInTheDocument()
  })

  it('error state renders role="alert"', () => {
    render(<SaveStatus state="error" />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('error state shows errorMessage as title attribute', () => {
    render(<SaveStatus state="error" errorMessage="Connection refused" />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveAttribute('title', 'Connection refused')
  })

  it('saving state has aria-live="polite"', () => {
    render(<SaveStatus state="saving" />)
    const el = screen.getByText(/saving/i).closest('[aria-live]')
    expect(el).toHaveAttribute('aria-live', 'polite')
  })

  it('saved state has aria-live="polite"', () => {
    render(<SaveStatus state="saved" />)
    const el = screen.getByText(/saved/i).closest('[aria-live]')
    expect(el).toHaveAttribute('aria-live', 'polite')
  })
})

// =====================================================================
// useSaveStatus hook — auto-revert 'saved' → 'idle' after 2s
// =====================================================================

function HookTestHarness() {
  const { state, setState } = useSaveStatus()
  return (
    <div>
      <span data-testid="state">{state}</span>
      <button onClick={() => setState('saved')}>Set Saved</button>
      <button onClick={() => setState('error')}>Set Error</button>
      <button onClick={() => setState('saving')}>Set Saving</button>
    </div>
  )
}

describe('useSaveStatus hook', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('initial state is idle', () => {
    render(<HookTestHarness />)
    expect(screen.getByTestId('state').textContent).toBe('idle')
  })

  it('setting state to saving reflects immediately', () => {
    render(<HookTestHarness />)
    act(() => {
      fireEvent.click(screen.getByRole('button', { name: /set saving/i }))
    })
    expect(screen.getByTestId('state').textContent).toBe('saving')
  })

  it('setting state to error stays as error (no auto-revert)', () => {
    render(<HookTestHarness />)
    act(() => {
      fireEvent.click(screen.getByRole('button', { name: /set error/i }))
    })
    expect(screen.getByTestId('state').textContent).toBe('error')
    act(() => { vi.advanceTimersByTime(3000) })
    expect(screen.getByTestId('state').textContent).toBe('error')
  })

  it('state transitions saved → idle automatically after 2 seconds', () => {
    render(<HookTestHarness />)

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: /set saved/i }))
    })
    expect(screen.getByTestId('state').textContent).toBe('saved')

    act(() => { vi.advanceTimersByTime(2100) })

    expect(screen.getByTestId('state').textContent).toBe('idle')
  })

  it('state does NOT revert before 2 seconds have elapsed', () => {
    render(<HookTestHarness />)

    act(() => {
      fireEvent.click(screen.getByRole('button', { name: /set saved/i }))
    })
    expect(screen.getByTestId('state').textContent).toBe('saved')

    act(() => { vi.advanceTimersByTime(1900) })

    expect(screen.getByTestId('state').textContent).toBe('saved')
  })
})
