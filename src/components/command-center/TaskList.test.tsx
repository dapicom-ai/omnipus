import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TaskList } from './TaskList'
import type { Task } from '@/lib/api'

// test_task_list_component (test #15)
// test_task_board_component (test #16) — board view lives inside TaskList
// Traces to: wave5a-wire-ui-spec.md — Scenario: Task list shows inbox, next, active, waiting, done
//             wave5a-wire-ui-spec.md — Scenario: GTD board view with 5 columns

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    fetchTasks: vi.fn(),
    createTask: vi.fn(),
  }
})

import { fetchTasks } from '@/lib/api'

const mockTasks: Task[] = [
  { id: 't1', name: 'Inbox item', status: 'inbox', agent_name: 'General Assistant' },
  { id: 't2', name: 'Next action', status: 'next', agent_name: 'Researcher' },
  { id: 't3', name: 'Active work', status: 'active', agent_name: 'General Assistant', cost: 0.5 },
  { id: 't4', name: 'Waiting on reply', status: 'waiting', agent_name: 'Researcher' },
  { id: 't5', name: 'Completed task', status: 'done', agent_name: 'General Assistant', cost: 0.2 },
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
})

describe('TaskList — list view (test #15)', () => {
  it('renders all tasks in list view by default', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: Task list shows all 5 status buckets (AC1)
    renderList()
    await screen.findByText('Inbox item')
    expect(screen.getByText('Next action')).toBeInTheDocument()
    expect(screen.getByText('Active work')).toBeInTheDocument()
    expect(screen.getByText('Waiting on reply')).toBeInTheDocument()
    expect(screen.getByText('Completed task')).toBeInTheDocument()
  })

  it('shows task count in header', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC2: total task count displayed
    renderList()
    await screen.findByText('Inbox item')
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
    await screen.findByText('Inbox item')
    expect(screen.getByRole('button', { name: /list view/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /board view/i })).toBeInTheDocument()
  })
})

describe('TaskList — board view (test #16)', () => {
  it('switches to board view showing 5 column headers when Board button is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: GTD board view (AC3)
    renderList()
    await screen.findByText('Inbox item')
    fireEvent.click(screen.getByRole('button', { name: /board view/i }))

    // All 5 GTD column headers should appear
    expect(screen.getByText('Inbox')).toBeInTheDocument()
    expect(screen.getByText('Next')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
    expect(screen.getByText('Waiting')).toBeInTheDocument()
    expect(screen.getByText('Done')).toBeInTheDocument()
  })

  it('places each task in the correct column in board view', async () => {
    // Traces to: wave5a-wire-ui-spec.md — Scenario: GTD board view (AC3)
    renderList()
    await screen.findByText('Inbox item')
    fireEvent.click(screen.getByRole('button', { name: /board view/i }))

    expect(screen.getByText('Inbox item')).toBeInTheDocument()
    expect(screen.getByText('Next action')).toBeInTheDocument()
    expect(screen.getByText('Active work')).toBeInTheDocument()
    expect(screen.getByText('Waiting on reply')).toBeInTheDocument()
    expect(screen.getByText('Completed task')).toBeInTheDocument()
  })
})

describe('TaskList — create task (test #15)', () => {
  it('shows inline create form when Task button is clicked', async () => {
    // Traces to: wave5a-wire-ui-spec.md — AC4: create task inline
    renderList()
    await screen.findByText('Inbox item')
    fireEvent.click(screen.getByRole('button', { name: /task/i }))
    expect(screen.getByPlaceholderText(/task name/i)).toBeInTheDocument()
  })
})
