import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TaskList } from './TaskList'
import type { Task } from '@/lib/api'

// test_task_list_component (test #15)
// test_task_board_component (test #16) — board view lives inside TaskList
// Traces to: wave5a-wire-ui-spec.md — Scenario: Task list shows execution statuses
//             wave5a-wire-ui-spec.md — Scenario: Board view with queued/running/completed/failed columns

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchTasks: vi.fn(),
    fetchAgents: vi.fn(),
    createTask: vi.fn(),
    startTask: vi.fn(),
  }
})

import { fetchTasks, fetchAgents } from '@/lib/api'

const mockTasks: Task[] = [
  { id: 't1', title: 'Queued item',    prompt: 'Do X',    status: 'queued',    priority: 3, trigger_type: 'manual', agent_name: 'General Assistant' },
  { id: 't2', title: 'Running item',   prompt: 'Do Y',    status: 'running',   priority: 2, trigger_type: 'manual', agent_name: 'Researcher' },
  { id: 't3', title: 'Completed item', prompt: 'Do Z',    status: 'completed', priority: 1, trigger_type: 'manual', agent_name: 'General Assistant', result: 'Done successfully' },
  { id: 't4', title: 'Failed item',    prompt: 'Do W',    status: 'failed',    priority: 4, trigger_type: 'manual', agent_name: 'Researcher', result: 'Error occurred' },
  { id: 't5', title: 'Assigned item',  prompt: 'Do V',    status: 'assigned',  priority: 5, trigger_type: 'manual', agent_name: 'General Assistant' },
]

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
}

function renderList() {
  return render(
    <QueryClientProvider client={makeClient()}>
      <TaskList />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.mocked(fetchTasks).mockResolvedValue(mockTasks)
  vi.mocked(fetchAgents).mockResolvedValue([])
})

describe('TaskList — list view (test #15)', () => {
  it('renders all tasks in list view by default', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Task list shows all status buckets (AC1)
    renderList()
    await screen.findByText('Queued item')
    expect(screen.getByText('Running item')).toBeInTheDocument()
    expect(screen.getByText('Completed item')).toBeInTheDocument()
    expect(screen.getByText('Failed item')).toBeInTheDocument()
    expect(screen.getByText('Assigned item')).toBeInTheDocument()
  })

  it('shows task count in header', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC2: total task count displayed
    renderList()
    await screen.findByText('Queued item')
    expect(screen.getByText(/\(5\)/)).toBeInTheDocument()
  })

  it('shows empty state when no tasks', async () => {
    // Dataset: Task List row 4 — empty list
    vi.mocked(fetchTasks).mockResolvedValue([])
    renderList()
    await screen.findByText(/no tasks yet/i)
    expect(screen.getByText(/no tasks yet/i)).toBeInTheDocument()
  })

  it('shows list view and board view toggle buttons', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC3: list/board view toggle
    renderList()
    await screen.findByText('Queued item')
    expect(screen.getByRole('button', { name: /list view/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /board view/i })).toBeInTheDocument()
  })
})

describe('TaskList — board view (test #16)', () => {
  it('switches to board view showing 4 column headers when Board button is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Execution board view (AC3)
    renderList()
    await screen.findByText('Queued item')
    fireEvent.click(screen.getByRole('button', { name: /board view/i }))

    // All 4 execution column headers should appear
    expect(screen.getByText('Queued')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('Completed')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
  })

  it('places each task in the correct column in board view', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Execution board view (AC3)
    renderList()
    await screen.findByText('Queued item')
    fireEvent.click(screen.getByRole('button', { name: /board view/i }))

    expect(screen.getByText('Queued item')).toBeInTheDocument()
    expect(screen.getByText('Running item')).toBeInTheDocument()
    expect(screen.getByText('Completed item')).toBeInTheDocument()
    expect(screen.getByText('Failed item')).toBeInTheDocument()
  })
})

describe('TaskList — create task (test #15)', () => {
  it('shows inline create form when Task button is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: create task inline
    renderList()
    await screen.findByText('Queued item')
    fireEvent.click(screen.getByRole('button', { name: /task/i }))
    expect(screen.getByPlaceholderText(/title/i)).toBeInTheDocument()
  })
})
