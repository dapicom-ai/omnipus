import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

// Wave 5b spec tests — OnboardingWizard frontend tests
// Traces to: wave5b-system-agent-spec.md — US-7 Onboarding Flow BDD scenarios

// Mock TanStack Router navigate
const mockNavigate = vi.fn()
vi.mock('@tanstack/react-router', () => ({
  createFileRoute: (_path: string) => (opts: { component: React.ComponentType }) => opts,
  useNavigate: () => mockNavigate,
}))

// Mock Framer Motion — strip all animations so AnimatePresence doesn't keep
// exit elements in the DOM during state transitions.
vi.mock('framer-motion', () => {
  const React = require('react')
  return {
    motion: new Proxy(
      {},
      {
        get: (_target: object, prop: string) => {
          return React.forwardRef(
            ({ children, ...props }: Record<string, unknown>, ref: unknown) =>
              React.createElement(prop as string, { ...props, ref }, children)
          )
        },
      }
    ),
    AnimatePresence: ({ children }: { children: React.ReactNode }) => children,
  }
})

// Mock API calls
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    configureProvider: vi.fn(),
    testProvider: vi.fn(),
    completeOnboarding: vi.fn(),
  }
})

// Mock SVG import
vi.mock('@/assets/logo/omnipus-avatar.svg?url', () => ({ default: '/test-avatar.svg' }))

import { configureProvider, testProvider, completeOnboarding } from '@/lib/api'

// Dynamically import the component AFTER mocks are in place
// The Route export is the component wrapped in createFileRoute — import the inner component
// by importing the module and rendering OnboardingWizard directly.
// Since the route component is not exported by name, we render via the Route.component.

async function renderWizard() {
  // Dynamic import after mocks are set
  const mod = await import('./onboarding')
  const Component = ((mod.Route as unknown) as { component: React.ComponentType }).component
  return render(<Component />)
}

beforeEach(() => {
  vi.clearAllMocks()
  mockNavigate.mockResolvedValue(undefined)
  vi.mocked(completeOnboarding).mockResolvedValue(undefined)
})

// =====================================================================
// Scenario: Step navigation (US-7 AC1, AC2, AC3)
// Traces to: wave5b-system-agent-spec.md — Scenario: Onboarding step navigation
// =====================================================================

describe('OnboardingWizard — step navigation', () => {
  it('renders step 1 (Welcome) by default with Get Started and Skip buttons', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: First-launch shows welcome screen
    // BDD: Given fresh install, When onboarding route loads, Then step 1 Welcome is shown
    await renderWizard()

    expect(screen.getByText('Welcome to Omnipus')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /get started/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /skip/i })).toBeInTheDocument()
  })

  it('advances to step 2 (Provider) when Get Started is clicked', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Get Started advances to provider step
    // BDD: Given step 1, When Get Started clicked, Then step 2 shown with provider selection
    await renderWizard()

    fireEvent.click(screen.getByRole('button', { name: /get started/i }))

    await waitFor(() => {
      expect(screen.getByText(/connect a provider/i)).toBeInTheDocument()
    })
  })

  it('shows step progress indicator with 3 dots', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Step indicator shows progress
    // BDD: Given wizard at any step, Then progressbar with 3 steps is visible
    await renderWizard()

    const progressbar = screen.getByRole('progressbar')
    expect(progressbar).toBeInTheDocument()
    expect(progressbar).toHaveAttribute('aria-valuenow', '1')
    expect(progressbar).toHaveAttribute('aria-valuemin', '1')
    expect(progressbar).toHaveAttribute('aria-valuemax', '3')
  })

  it('step 2 Back button returns to step 1', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Back navigation in wizard
    await renderWizard()

    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    fireEvent.click(screen.getByRole('button', { name: /back/i }))

    await waitFor(() => {
      expect(screen.getByText('Welcome to Omnipus')).toBeInTheDocument()
    })
  })
})

// =====================================================================
// Scenario: Provider selection (US-8 AC1, AC3)
// Traces to: wave5b-system-agent-spec.md — Scenario: Provider setup in onboarding
// =====================================================================

describe('OnboardingWizard — provider selection', () => {
  it('shows all 5 providers in step 2', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Provider selection UI
    // BDD: Given step 2, Then Anthropic, OpenRouter, OpenAI, Google Gemini, Groq are shown
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    expect(screen.getByRole('button', { name: /anthropic/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /openrouter/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /openai/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /google gemini/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /groq/i })).toBeInTheDocument()
  })

  it('shows API key input when a provider is selected', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: API key input appears after provider selection
    // BDD: Given step 2, When Anthropic selected, Then API key input is shown
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))

    await waitFor(() => {
      expect(screen.getByLabelText('API Key')).toBeInTheDocument()
    })
  })

  it('shows placeholder hint for the selected provider', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Provider-specific placeholder hints
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))

    await waitFor(() => {
      const input = screen.getByLabelText('API Key')
      expect(input).toHaveAttribute('placeholder', 'sk-ant-api03-...')
    })
  })

  it('API key input defaults to password type (hidden)', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: API key is masked by default (US-8 AC3)
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))
    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))

    await waitFor(() => {
      const input = screen.getByLabelText('API Key')
      expect(input).toHaveAttribute('type', 'password')
    })
  })

  it('show/hide toggle reveals the API key', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Toggle API key visibility
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))
    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))

    await waitFor(() => screen.getByLabelText('API Key'))
    const toggleBtn = screen.getByRole('button', { name: /show api key/i })
    fireEvent.click(toggleBtn)

    expect(screen.getByLabelText('API Key')).toHaveAttribute('type', 'text')
  })
})

// =====================================================================
// Scenario: Test connection (US-8 AC4, AC5)
// Traces to: wave5b-system-agent-spec.md — Scenario: Test connection success/failure
// =====================================================================

describe('OnboardingWizard — test connection', () => {
  async function goToProviderStep() {
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))
    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))
    await waitFor(() => screen.getByLabelText('API Key'))
    fireEvent.change(screen.getByLabelText('API Key'), {
      target: { value: 'sk-ant-api03-test' },
    })
  }

  it('Test Connection button is disabled when API key is empty', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Test Connection disabled without key
    await renderWizard()
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))
    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))
    await waitFor(() => screen.getByLabelText('API Key'))

    const testBtn = screen.getByRole('button', { name: /test connection/i })
    expect(testBtn).toBeDisabled()
  })

  it('shows success feedback on successful test connection', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Test connection success (US-8 AC4)
    // BDD: Given API key entered, When test succeeds, Then "Connected successfully" shown
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: true })

    await goToProviderStep()

    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    await waitFor(() => {
      expect(screen.getByText(/connected successfully/i)).toBeInTheDocument()
    })
  })

  it('shows error feedback on failed test connection', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Test connection failure (US-8 AC5)
    // BDD: Given API key entered, When test fails, Then error message shown
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: false, error: 'Invalid API key' })

    await goToProviderStep()

    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    await waitFor(() => {
      expect(screen.getByText(/invalid api key/i)).toBeInTheDocument()
    })
  })

  it('Continue button is disabled until test is successful', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Continue gated on successful test
    // BDD: Given step 2, Until testStatus=success, Then Continue is disabled
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: false, error: 'Bad key' })

    await goToProviderStep()

    // Before test: Continue is disabled
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))
    await waitFor(() => screen.getByText(/bad key/i))

    // After failed test: Continue still disabled
    expect(screen.getByRole('button', { name: /continue/i })).toBeDisabled()
  })

  it('Continue button enabled after successful test', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Continue enabled after success
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: true })

    await goToProviderStep()
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /continue/i })).not.toBeDisabled()
    })
  })
})

// =====================================================================
// Scenario: Skip onboarding (US-7 AC8)
// Traces to: wave5b-system-agent-spec.md — Scenario: Skip for advanced users
// =====================================================================

describe('OnboardingWizard — skip', () => {
  it('clicking Skip calls completeOnboarding and navigates to root', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Skip for advanced users (US-7 AC8)
    // BDD: Given step 1, When Skip clicked, Then completeOnboarding called, navigate to /
    await renderWizard()

    fireEvent.click(screen.getByRole('button', { name: /skip/i }))

    await waitFor(() => {
      expect(completeOnboarding).toHaveBeenCalledOnce()
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/' })
    })
  })

  it('skip works even if completeOnboarding fails', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Skip must not block navigation
    vi.mocked(completeOnboarding).mockRejectedValue(new Error('network error'))

    await renderWizard()

    fireEvent.click(screen.getByRole('button', { name: /skip/i }))

    // Navigation must still happen despite API failure
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/' })
    })
  })
})

// =====================================================================
// Scenario: Finish onboarding (US-7 AC9)
// Traces to: wave5b-system-agent-spec.md — Scenario: Onboarding finish persists state
// =====================================================================

describe('OnboardingWizard — finish', () => {
  it('finishing calls completeOnboarding and navigates to root', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Onboarding completion (US-7 AC9)
    // BDD: Given step 3, When Start Exploring clicked, Then completeOnboarding called, navigate to /
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: true })

    await renderWizard()

    // Step 1 → 2
    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    // Select provider, enter key, test
    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))
    await waitFor(() => screen.getByLabelText('API Key'))
    fireEvent.change(screen.getByLabelText('API Key'), {
      target: { value: 'sk-ant-api03-test' },
    })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))
    await waitFor(() => screen.getByText(/connected successfully/i))

    // Step 2 → 3
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => screen.getByText(/you.re all set/i))

    // Finish
    fireEvent.click(screen.getByRole('button', { name: /start exploring/i }))

    await waitFor(() => {
      expect(completeOnboarding).toHaveBeenCalledOnce()
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/' })
    })
  })

  it('displays provider name on completion step', async () => {
    // Traces to: wave5b-system-agent-spec.md — Scenario: Step 3 shows provider name
    vi.mocked(configureProvider).mockResolvedValue({} as never)
    vi.mocked(testProvider).mockResolvedValue({ success: true })

    await renderWizard()

    fireEvent.click(screen.getByRole('button', { name: /get started/i }))
    await waitFor(() => screen.getByText(/connect a provider/i))

    fireEvent.click(screen.getByRole('button', { name: /anthropic/i }))
    await waitFor(() => screen.getByLabelText('API Key'))
    fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'sk-test' } })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))
    await waitFor(() => screen.getByText(/connected successfully/i))

    fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    await waitFor(() => {
      expect(screen.getByText(/anthropic connected/i)).toBeInTheDocument()
    })
  })
})
