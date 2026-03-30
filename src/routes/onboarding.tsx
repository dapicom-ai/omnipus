import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { AnimatePresence, motion } from 'framer-motion'
import {
  ArrowRight,
  ArrowLeft,
  Eye,
  EyeSlash,
  SpinnerGap,
  CheckCircle,
  XCircle,
  ShieldCheck,
  Lightning,
  Cube,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { configureProvider, testProvider, completeOnboarding } from '@/lib/api'
import OmnipusAvatar from '@/assets/logo/omnipus-avatar.svg?url'
import { PROVIDER_HINTS } from '@/lib/constants'
import { useUiStore } from '@/store/ui'

// US-7: First-launch onboarding flow — full-screen, outside AppShell
// US-8: Provider setup with API key input + test connection

type Step = 1 | 2 | 3
type TestStatus = 'idle' | 'testing' | 'success' | 'error'

const PROVIDERS = [
  { id: 'anthropic', display_name: 'Anthropic' },
  { id: 'openrouter', display_name: 'OpenRouter' },
  { id: 'openai', display_name: 'OpenAI' },
  { id: 'google', display_name: 'Google Gemini' },
  { id: 'groq', display_name: 'Groq' },
]

const WELCOME_FEATURES = [
  { Icon: ShieldCheck, text: 'Kernel-level sandboxing — agents operate in security boundaries by default' },
  { Icon: Lightning, text: 'Zero-IPC channels — Discord, Slack, Telegram compiled into the binary' },
  { Icon: Cube, text: 'Single Go binary — no runtime dependencies, runs anywhere' },
]

const stepVariants = {
  enter: (direction: number) => ({
    x: direction > 0 ? 36 : -36,
    opacity: 0,
  }),
  center: { x: 0, opacity: 1 },
  exit: (direction: number) => ({
    x: direction > 0 ? -36 : 36,
    opacity: 0,
  }),
}

function OnboardingWizard() {
  const navigate = useNavigate()
  const { addToast } = useUiStore()

  const [step, setStep] = useState<Step>(1)
  const [direction, setDirection] = useState(1)
  const [selectedProvider, setSelectedProvider] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [showKey, setShowKey] = useState(false)
  const [testStatus, setTestStatus] = useState<TestStatus>('idle')
  const [testError, setTestError] = useState('')
  const [isSaving, setIsSaving] = useState(false)

  const providerDef = PROVIDERS.find((p) => p.id === selectedProvider)
  const providerHintText = selectedProvider ? PROVIDER_HINTS[selectedProvider] : undefined

  const goTo = (next: Step) => {
    setDirection(next > step ? 1 : -1)
    setStep(next)
  }

  const handleSelectProvider = (id: string) => {
    setSelectedProvider(id)
    setApiKey('')
    setTestStatus('idle')
    setTestError('')
  }

  const handleApiKeyChange = (k: string) => {
    setApiKey(k)
    if (testStatus !== 'idle') {
      setTestStatus('idle')
      setTestError('')
    }
  }

  const handleTest = async () => {
    if (!selectedProvider || !apiKey.trim()) return
    setTestStatus('testing')
    setTestError('')
    try {
      await configureProvider(selectedProvider, apiKey.trim())
      const result = await testProvider(selectedProvider)
      if (result.success) {
        setTestStatus('success')
      } else {
        setTestStatus('error')
        setTestError(result.error ?? 'Connection test failed')
      }
    } catch (err) {
      setTestStatus('error')
      setTestError((err as Error).message)
    }
  }

  const handleFinish = async () => {
    setIsSaving(true)
    try {
      await completeOnboarding()
    } catch {
      addToast({ message: 'Could not save onboarding state', variant: 'error' })
    } finally {
      setIsSaving(false)
    }
    navigate({ to: '/' })
  }

  // US-7: Skip option for advanced users
  const handleSkip = async () => {
    try {
      await completeOnboarding()
    } catch {
      addToast({ message: 'Could not save onboarding state', variant: 'error' })
    }
    navigate({ to: '/' })
  }

  return (
    <div
      className="min-h-screen flex flex-col items-center justify-center p-6 relative overflow-hidden"
      style={{ backgroundColor: 'var(--color-primary)', color: 'var(--color-secondary)' }}
    >
      {/* Atmospheric depth — subtle Forge Gold radial glow */}
      <div
        aria-hidden
        className="absolute inset-0 pointer-events-none"
        style={{
          background:
            'radial-gradient(ellipse 65% 55% at 50% 50%, rgba(212,175,55,0.055) 0%, transparent 68%)',
        }}
      />
      {/* Top edge accent line */}
      <div
        aria-hidden
        className="absolute top-0 left-0 right-0 h-px pointer-events-none"
        style={{
          background:
            'linear-gradient(90deg, transparent 0%, rgba(212,175,55,0.35) 50%, transparent 100%)',
        }}
      />

      {/* Step indicator */}
      <div className="flex items-center gap-2 mb-12 z-10" role="progressbar" aria-valuenow={step} aria-valuemin={1} aria-valuemax={3}>
        {([1, 2, 3] as Step[]).map((s) => (
          <motion.div
            key={s}
            animate={{
              width: s === step ? 24 : 8,
              backgroundColor:
                s === step
                  ? '#d4af37'
                  : s < step
                  ? 'rgba(212,175,55,0.45)'
                  : '#2d3748',
            }}
            transition={{ duration: 0.3, ease: 'easeInOut' }}
            className="h-2 rounded-full"
          />
        ))}
      </div>

      {/* Animated step content */}
      <div className="w-full max-w-md z-10">
        <AnimatePresence mode="wait" custom={direction}>
          {step === 1 && (
            <motion.div
              key="step1"
              custom={direction}
              variants={stepVariants}
              initial="enter"
              animate="center"
              exit="exit"
              transition={{ duration: 0.22, ease: 'easeInOut' }}
            >
              <WelcomeStep onGetStarted={() => goTo(2)} onSkip={handleSkip} />
            </motion.div>
          )}
          {step === 2 && (
            <motion.div
              key="step2"
              custom={direction}
              variants={stepVariants}
              initial="enter"
              animate="center"
              exit="exit"
              transition={{ duration: 0.22, ease: 'easeInOut' }}
            >
              <ProviderStep
                selectedProvider={selectedProvider}
                onSelect={handleSelectProvider}
                apiKey={apiKey}
                onApiKeyChange={handleApiKeyChange}
                showKey={showKey}
                onToggleShowKey={() => setShowKey((v) => !v)}
                testStatus={testStatus}
                testError={testError}
                onTest={handleTest}
                onBack={() => goTo(1)}
                onContinue={() => goTo(3)}
                providerHint={providerHintText}
              />
            </motion.div>
          )}
          {step === 3 && (
            <motion.div
              key="step3"
              custom={direction}
              variants={stepVariants}
              initial="enter"
              animate="center"
              exit="exit"
              transition={{ duration: 0.22, ease: 'easeInOut' }}
            >
              <DoneStep
                providerName={providerDef?.display_name ?? selectedProvider}
                isSaving={isSaving}
                onFinish={handleFinish}
              />
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </div>
  )
}

// ── Step 1: Welcome ────────────────────────────────────────────────────────────

function WelcomeStep({
  onGetStarted,
  onSkip,
}: {
  onGetStarted: () => void
  onSkip: () => void
}) {
  return (
    <div className="flex flex-col items-center text-center gap-8">
      {/* Mascot with Forge Gold glow halo */}
      <motion.div
        initial={{ scale: 0.75, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ duration: 0.5, ease: [0.34, 1.56, 0.64, 1] }}
        className="relative"
      >
        <div
          aria-hidden
          className="absolute rounded-full blur-3xl pointer-events-none"
          style={{
            inset: '-40%',
            background: 'rgba(212,175,55,0.14)',
          }}
        />
        <img
          src={OmnipusAvatar}
          alt="Omnipus — Master Tasker"
          className="relative h-28 w-28 sm:h-36 sm:w-36 drop-shadow-2xl"
        />
      </motion.div>

      <motion.div
        initial={{ y: 14, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.18, duration: 0.38 }}
      >
        <h1 className="font-headline text-4xl sm:text-5xl font-bold leading-tight mb-2"
          style={{ color: 'var(--color-secondary)' }}>
          Welcome to Omnipus
        </h1>
        <p className="font-headline text-base font-bold tracking-wide"
          style={{ color: 'var(--color-accent)' }}>
          Elite Simplicity. Sovereign Control.
        </p>
      </motion.div>

      <motion.div
        initial={{ y: 14, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.28, duration: 0.38 }}
        className="w-full space-y-2.5"
      >
        {WELCOME_FEATURES.map(({ Icon, text }, i) => (
          <div
            key={i}
            className="flex items-start gap-3 p-3 rounded-lg border text-left"
            style={{
              borderColor: 'var(--color-border)',
              backgroundColor: 'var(--color-surface-1)',
            }}
          >
            <Icon size={17} weight="duotone" className="shrink-0 mt-0.5"
              style={{ color: 'var(--color-accent)' }} />
            <p className="text-sm leading-snug" style={{ color: 'var(--color-muted)' }}>
              {text}
            </p>
          </div>
        ))}
      </motion.div>

      <motion.div
        initial={{ y: 14, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.38, duration: 0.38 }}
        className="w-full flex flex-col gap-3"
      >
        <Button
          className="w-full h-11 gap-2 font-headline font-bold text-base"
          onClick={onGetStarted}
        >
          Get Started
          <ArrowRight size={16} weight="bold" />
        </Button>
        <button
          type="button"
          onClick={onSkip}
          className="text-sm transition-colors underline underline-offset-2"
          style={{ color: 'var(--color-muted)' }}
          onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--color-secondary)')}
          onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--color-muted)')}
        >
          Skip — I know what I&apos;m doing
        </button>
      </motion.div>
    </div>
  )
}

// ── Step 2: Provider Setup ─────────────────────────────────────────────────────

function ProviderStep({
  selectedProvider,
  onSelect,
  apiKey,
  onApiKeyChange,
  showKey,
  onToggleShowKey,
  testStatus,
  testError,
  onTest,
  onBack,
  onContinue,
  providerHint,
}: {
  selectedProvider: string
  onSelect: (id: string) => void
  apiKey: string
  onApiKeyChange: (k: string) => void
  showKey: boolean
  onToggleShowKey: () => void
  testStatus: TestStatus
  testError: string
  onTest: () => void
  onBack: () => void
  onContinue: () => void
  providerHint?: string
}) {
  return (
    <div className="flex flex-col gap-6">
      <div>
        <h2 className="font-headline text-2xl font-bold mb-1"
          style={{ color: 'var(--color-secondary)' }}>
          Connect a provider
        </h2>
        <p className="text-sm" style={{ color: 'var(--color-muted)' }}>
          Omnipus needs an AI provider to power your agents.
        </p>
      </div>

      {/* Provider selection grid */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
        {PROVIDERS.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => onSelect(p.id)}
            className="px-3 py-2.5 rounded-lg border text-sm font-medium transition-all duration-150 text-left focus-visible:outline-none focus-visible:ring-2"
            style={
              selectedProvider === p.id
                ? {
                    borderColor: 'var(--color-accent)',
                    backgroundColor: 'rgba(212,175,55,0.09)',
                    color: 'var(--color-accent)',
                  }
                : {
                    borderColor: 'var(--color-border)',
                    backgroundColor: 'var(--color-surface-1)',
                    color: 'var(--color-secondary)',
                  }
            }
          >
            {p.display_name}
          </button>
        ))}
      </div>

      {/* API key — animates in when provider is selected */}
      <AnimatePresence>
        {selectedProvider && (
          <motion.div
            key="apikey"
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
            className="overflow-hidden"
          >
            <div className="space-y-4">
              <div>
                <label
                  htmlFor="onboarding-api-key"
                  className="text-xs font-medium mb-1.5 block"
                  style={{ color: 'var(--color-muted)' }}
                >
                  API Key
                </label>
                <div className="relative">
                  <Input
                    id="onboarding-api-key"
                    type={showKey ? 'text' : 'password'}
                    value={apiKey}
                    onChange={(e) => onApiKeyChange(e.target.value)}
                    placeholder={providerHint}
                    className="pr-9 font-mono text-sm"
                    autoComplete="off"
                    autoFocus
                  />
                  <button
                    type="button"
                    onClick={onToggleShowKey}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 transition-colors"
                    style={{ color: 'var(--color-muted)' }}
                    aria-label={showKey ? 'Hide API key' : 'Show API key'}
                  >
                    {showKey ? <EyeSlash size={14} /> : <Eye size={14} />}
                  </button>
                </div>
                <p className="text-[10px] mt-1.5 font-mono" style={{ color: 'var(--color-muted)' }}>
                  Stored encrypted with AES-256-GCM — never in plaintext
                </p>
              </div>

              {/* Test connection feedback */}
              {testStatus === 'success' && (
                <div className="flex items-center gap-2 text-sm" style={{ color: 'var(--color-success)' }}>
                  <CheckCircle size={14} weight="fill" />
                  <span>Connected successfully</span>
                </div>
              )}
              {testStatus === 'error' && (
                <div className="flex items-start gap-2 text-sm" style={{ color: 'var(--color-error)' }}>
                  <XCircle size={14} weight="fill" className="shrink-0 mt-0.5" />
                  <span>{testError || 'Connection failed — check your key and try again'}</span>
                </div>
              )}

              <Button
                variant="outline"
                className="w-full gap-2"
                onClick={onTest}
                disabled={!apiKey.trim() || testStatus === 'testing'}
              >
                {testStatus === 'testing' ? (
                  <>
                    <SpinnerGap size={13} className="animate-spin" />
                    Testing connection...
                  </>
                ) : testStatus === 'success' ? (
                  <>
                    <CheckCircle size={13} weight="fill" style={{ color: 'var(--color-success)' }} />
                    Test again
                  </>
                ) : (
                  'Test Connection'
                )}
              </Button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Navigation */}
      <div className="flex items-center gap-3 pt-2">
        <Button variant="ghost" className="gap-1.5" onClick={onBack}>
          <ArrowLeft size={14} />
          Back
        </Button>
        <Button
          className="flex-1 gap-2 font-headline font-bold"
          onClick={onContinue}
          disabled={testStatus !== 'success'}
        >
          Continue
          <ArrowRight size={14} weight="bold" />
        </Button>
      </div>
    </div>
  )
}

// ── Step 3: Done ───────────────────────────────────────────────────────────────

function DoneStep({
  providerName,
  isSaving,
  onFinish,
}: {
  providerName: string
  isSaving: boolean
  onFinish: () => void
}) {
  return (
    <div className="flex flex-col items-center text-center gap-8">
      <motion.div
        initial={{ scale: 0, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={{ duration: 0.48, ease: [0.34, 1.56, 0.64, 1] }}
      >
        <CheckCircle size={80} weight="fill" style={{ color: 'var(--color-accent)' }} />
      </motion.div>

      <motion.div
        initial={{ y: 14, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.2, duration: 0.38 }}
      >
        <h2 className="font-headline text-3xl font-bold mb-2"
          style={{ color: 'var(--color-secondary)' }}>
          You&apos;re all set
        </h2>
        <p className="text-sm" style={{ color: 'var(--color-muted)' }}>
          {providerName
            ? `${providerName} connected.`
            : 'Provider connected.'}{' '}
          Omnipus is ready to work.
        </p>
      </motion.div>

      <motion.div
        initial={{ y: 14, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ delay: 0.32, duration: 0.38 }}
        className="w-full"
      >
        <Button
          className="w-full h-11 gap-2 font-headline font-bold text-base"
          onClick={onFinish}
          disabled={isSaving}
        >
          {isSaving ? (
            <>
              <SpinnerGap size={16} className="animate-spin" />
              Loading...
            </>
          ) : (
            <>
              Start Exploring
              <ArrowRight size={16} weight="bold" />
            </>
          )}
        </Button>
      </motion.div>
    </div>
  )
}

export const Route = createFileRoute('/onboarding')({
  component: OnboardingWizard,
})
