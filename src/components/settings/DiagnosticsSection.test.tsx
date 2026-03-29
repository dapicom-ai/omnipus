import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// Wave 5b spec tests — DiagnosticsSection frontend tests
// Traces to: wave5b-system-agent-spec.md — US-10 Doctor diagnostics UI BDD scenarios

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchDoctorResults: vi.fn(),
    runDoctor: vi.fn(),
  }
})

// Mock store
vi.mock('@/store/ui', () => ({
  useUiStore: vi.fn(() => ({ addToast: vi.fn() })),
}))

import { fetchDoctorResults, runDoctor } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { DiagnosticsSection } from './DiagnosticsSection'
import type { DoctorResult } from '@/lib/api'

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function renderSection() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <DiagnosticsSection />
    </QueryClientProvider>
  )
}

const mockAddToast = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useUiStore).mockReturnValue({ addToast: mockAddToast } as never)
})

// =====================================================================
// Scenario: Empty state — no diagnostics run yet (US-10 AC1)
// Traces to: wave5b-system-agent-spec.md — Scenario: Empty state before doctor run
// =====================================================================

describe('DiagnosticsSection — empty state', () => {
  it('shows empty state message when no diagnostics have been run', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: No run yet shows placeholder (US-10 AC1)
    // BDD: Given no doctor results, When DiagnosticsSection renders, Then empty state shown
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)

    renderSection()

    await waitFor(() => {
      expect(screen.getByText(/no diagnostics run yet/i)).toBeInTheDocument()
    })
  })

  it('shows run diagnostics button in all states', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Run diagnostics button always visible
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)

    renderSection()

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /run diagnostics/i })).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Scenario: Display last results (US-10 AC2, AC3)
// Traces to: wave5b-system-agent-spec.md — Scenario: Doctor results display
// =====================================================================

describe('DiagnosticsSection — results display', () => {
  const fullResult: DoctorResult = {
    score: 42,
    checked_at: '2026-03-29T10:00:00Z',
    issues: [
      {
        id: 'issue-high-1',
        severity: 'high',
        title: 'Landlock not enabled',
        description: 'Kernel filesystem sandbox is disabled.',
        recommendation: 'Enable Landlock in your kernel configuration.',
        action_link: 'https://docs.omnipus.ai/security',
        action_label: 'Learn more',
      },
      {
        id: 'issue-med-1',
        severity: 'medium',
        title: 'seccomp filter loose',
        description: 'The seccomp filter allows some risky syscalls.',
        recommendation: 'Tighten the seccomp profile.',
      },
      {
        id: 'issue-low-1',
        severity: 'low',
        title: 'Audit log rotation not configured',
        description: 'Logs may grow unbounded.',
        recommendation: 'Configure log rotation.',
      },
    ],
  }

  it('renders risk score when results are loaded', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Risk score gauge displayed (US-10 AC2)
    // BDD: Given doctor result with score=42, Then score "42" is shown
    vi.mocked(fetchDoctorResults).mockResolvedValue(fullResult)

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument()
    })
  })

  it('displays the last run timestamp', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Last run time displayed (US-10 AC2)
    vi.mocked(fetchDoctorResults).mockResolvedValue(fullResult)

    renderSection()

    await waitFor(() => {
      expect(screen.getByText(/last run:/i)).toBeInTheDocument()
    })
  })

  it('groups issues by severity: high, medium, low', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Issues grouped by severity (US-10 AC3)
    // BDD: Given results with issues of all 3 severities, Then groups HIGH, MEDIUM, LOW visible
    vi.mocked(fetchDoctorResults).mockResolvedValue(fullResult)

    renderSection()

    await waitFor(() => {
      expect(screen.getByText(/high — 1 issue/i)).toBeInTheDocument()
      expect(screen.getByText(/medium — 1 issue/i)).toBeInTheDocument()
      expect(screen.getByText(/low — 1 issue/i)).toBeInTheDocument()
    })
  })

  it('displays issue titles in the issues list', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Issue titles visible
    vi.mocked(fetchDoctorResults).mockResolvedValue(fullResult)

    renderSection()

    await waitFor(() => {
      expect(screen.getByText('Landlock not enabled')).toBeInTheDocument()
      expect(screen.getByText('seccomp filter loose')).toBeInTheDocument()
      expect(screen.getByText('Audit log rotation not configured')).toBeInTheDocument()
    })
  })

  it('shows risk score label based on score value', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Risk label changes with score (US-10 AC2)
    // Dataset from spec: score≤10 → Excellent, ≤33 → Low, ≤66 → Medium, ≤85 → High, >85 → Critical
    const cases: Array<{ score: number; label: string }> = [
      { score: 5, label: 'Excellent' },
      { score: 20, label: 'Low risk' },
      { score: 50, label: 'Medium risk' },
      { score: 75, label: 'High risk' },
      { score: 95, label: 'Critical' },
    ]

    for (const { score, label } of cases) {
      const client = makeClient()
      vi.mocked(fetchDoctorResults).mockResolvedValue({ score, issues: [], checked_at: '2026-01-01T00:00:00Z' })
      const { unmount } = render(
        <QueryClientProvider client={client}>
          <DiagnosticsSection />
        </QueryClientProvider>
      )

      await waitFor(() => {
        expect(screen.getAllByText(label).length).toBeGreaterThan(0)
      })
      unmount()
    }
  })

  it('shows all clear message when no issues', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: All clear message when score is 0
    vi.mocked(fetchDoctorResults).mockResolvedValue({
      score: 0,
      issues: [],
      checked_at: '2026-01-01T00:00:00Z',
    })

    renderSection()

    await waitFor(() => {
      expect(screen.getByText(/no issues found/i)).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Scenario: Issue card expand/collapse (US-10 AC4)
// Traces to: wave5b-system-agent-spec.md — Scenario: Issue card details expandable
// =====================================================================

describe('DiagnosticsSection — issue card interaction', () => {
  const resultWithIssue: DoctorResult = {
    score: 60,
    checked_at: '2026-03-29T12:00:00Z',
    issues: [
      {
        id: 'landlock-disabled',
        severity: 'high',
        title: 'Landlock not enabled',
        description: 'Kernel filesystem sandbox is disabled.',
        recommendation: 'Enable Landlock in kernel config.',
        action_link: 'https://docs.omnipus.ai/landlock',
        action_label: 'Enable Landlock',
      },
    ],
  }

  it('clicking an issue card expands it to show description and recommendation', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Issue card expand shows details (US-10 AC4)
    // BDD: Given issue card visible, When clicked, Then description + recommendation shown
    vi.mocked(fetchDoctorResults).mockResolvedValue(resultWithIssue)

    renderSection()

    await waitFor(() => screen.getByText('Landlock not enabled'))

    // Issue description and recommendation not shown initially (collapsed)
    expect(screen.queryByText(/kernel filesystem sandbox is disabled/i)).not.toBeInTheDocument()

    // Click to expand
    fireEvent.click(screen.getByText('Landlock not enabled'))

    await waitFor(() => {
      expect(screen.getByText(/kernel filesystem sandbox is disabled/i)).toBeInTheDocument()
      expect(screen.getByText(/enable landlock in kernel config/i)).toBeInTheDocument()
    })
  })

  it('shows action link when present', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Action link visible in expanded card
    vi.mocked(fetchDoctorResults).mockResolvedValue(resultWithIssue)

    renderSection()

    await waitFor(() => screen.getByText('Landlock not enabled'))
    fireEvent.click(screen.getByText('Landlock not enabled'))

    await waitFor(() => {
      expect(screen.getByRole('link', { name: /enable landlock/i })).toBeInTheDocument()
    })
  })

  it('clicking expanded card collapses it', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Issue card collapse
    vi.mocked(fetchDoctorResults).mockResolvedValue(resultWithIssue)

    renderSection()

    await waitFor(() => screen.getByText('Landlock not enabled'))

    // Expand
    fireEvent.click(screen.getByText('Landlock not enabled'))
    await waitFor(() => screen.getByText(/kernel filesystem sandbox is disabled/i))

    // Collapse
    fireEvent.click(screen.getByText('Landlock not enabled'))
    await waitFor(() => {
      expect(screen.queryByText(/kernel filesystem sandbox is disabled/i)).not.toBeInTheDocument()
    })
  })
})

// =====================================================================
// Scenario: Run diagnostics button (US-10 AC5)
// Traces to: wave5b-system-agent-spec.md — Scenario: Run diagnostics updates results
// =====================================================================

describe('DiagnosticsSection — run diagnostics', () => {
  it('clicking Run diagnostics calls runDoctor API', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Doctor run triggered from UI (US-10 AC5)
    // BDD: Given DiagnosticsSection, When Run diagnostics clicked, Then runDoctor API called
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)
    vi.mocked(runDoctor).mockResolvedValue({
      score: 10,
      issues: [],
      checked_at: new Date().toISOString(),
    })

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run diagnostics/i }))

    fireEvent.click(screen.getByRole('button', { name: /run diagnostics/i }))

    await waitFor(() => {
      expect(runDoctor).toHaveBeenCalledOnce()
    })
  })

  it('shows toast with risk score after successful run', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Doctor run shows toast on completion
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)
    vi.mocked(runDoctor).mockResolvedValue({
      score: 25,
      issues: [],
      checked_at: new Date().toISOString(),
    })

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run diagnostics/i }))
    fireEvent.click(screen.getByRole('button', { name: /run diagnostics/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ message: expect.stringContaining('25') })
      )
    })
  })

  it('updates displayed results after run completes', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Results update after run (US-10 AC5)
    const newResult: DoctorResult = {
      score: 15,
      issues: [],
      checked_at: new Date().toISOString(),
    }

    vi.mocked(fetchDoctorResults).mockResolvedValue(null)
    vi.mocked(runDoctor).mockResolvedValue(newResult)

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run diagnostics/i }))
    fireEvent.click(screen.getByRole('button', { name: /run diagnostics/i }))

    await waitFor(() => {
      // Score from the new result is shown
      expect(screen.getByText('15')).toBeInTheDocument()
    })
  })

  it('shows error toast if run diagnostics fails', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Doctor run error handling
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)
    vi.mocked(runDoctor).mockRejectedValue(new Error('doctor unavailable'))

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run diagnostics/i }))
    fireEvent.click(screen.getByRole('button', { name: /run diagnostics/i }))

    await waitFor(() => {
      expect(mockAddToast).toHaveBeenCalledWith(
        expect.objectContaining({ message: 'doctor unavailable', variant: 'error' })
      )
    })
  })

  it('run diagnostics button is disabled while running', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Run button disabled during execution
    vi.mocked(fetchDoctorResults).mockResolvedValue(null)
    // Never resolve — stay in "running" state
    vi.mocked(runDoctor).mockReturnValue(new Promise(() => {}))

    renderSection()

    await waitFor(() => screen.getByRole('button', { name: /run diagnostics/i }))
    fireEvent.click(screen.getByRole('button', { name: /run diagnostics/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /running/i })).toBeDisabled()
    })
  })
})
