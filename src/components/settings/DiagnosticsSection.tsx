import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ShieldWarning,
  ShieldCheck,
  Warning,
  XCircle,
  Info,
  ArrowCounterClockwise,
  SpinnerGap,
  CaretRight,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { fetchDoctorResults, runDoctor } from '@/lib/api'
import type { DoctorResult, DoctorIssue } from '@/lib/api'
import { useUiStore } from '@/store/ui'

function getSecurityLabel(score: number): string {
  if (score >= 90) return 'Excellent'
  if (score >= 67) return 'Good'
  if (score >= 34) return 'At risk'
  return 'Critical'
}

function getSecurityColor(score: number): string {
  if (score >= 67) return 'var(--color-success)'
  if (score >= 34) return 'var(--color-warning)'
  return 'var(--color-error)'
}

const SEVERITY_CONFIG = {
  high: {
    Icon: XCircle,
    label: 'High',
    color: 'var(--color-error)',
    borderColor: 'rgba(239,68,68,0.25)',
    bgColor: 'rgba(239,68,68,0.06)',
  },
  medium: {
    Icon: Warning,
    label: 'Medium',
    color: 'var(--color-warning)',
    borderColor: 'rgba(234,179,8,0.25)',
    bgColor: 'rgba(234,179,8,0.06)',
  },
  low: {
    Icon: Info,
    label: 'Low',
    color: 'var(--color-info)',
    borderColor: 'rgba(59,130,246,0.25)',
    bgColor: 'rgba(59,130,246,0.06)',
  },
} as const

export function DiagnosticsSection() {
  const { addToast } = useUiStore()
  const queryClient = useQueryClient()
  const [expandedIssue, setExpandedIssue] = useState<string | null>(null)

  const { data: lastResult, isLoading, isError } = useQuery({
    queryKey: ['doctor'],
    queryFn: fetchDoctorResults,
    retry: false,
  })

  const { mutate: doRun, isPending: isRunning } = useMutation({
    mutationFn: runDoctor,
    onSuccess: (result) => {
      queryClient.setQueryData(['doctor'], result)
      addToast({
        message: `Diagnostics complete — security score: ${result.score}/100`,
        variant: result.score >= 67 ? 'success' : 'error',
      })
    },
    onError: (err: Error) => addToast({ message: err.message, variant: 'error' }),
  })

  const result = lastResult as DoctorResult | null

  const issuesByGroup = result
    ? ({
        high: result.issues.filter((i) => i.severity === 'high'),
        medium: result.issues.filter((i) => i.severity === 'medium'),
        low: result.issues.filter((i) => i.severity === 'low'),
      } as const)
    : null

  const securityColor = result ? getSecurityColor(result.score) : undefined

  return (
    <section className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-xs font-semibold uppercase tracking-wider"
            style={{ color: 'var(--color-muted)' }}>
            Diagnostics
          </h3>
          {result && (
            <p className="text-[10px] mt-0.5" style={{ color: 'var(--color-muted)' }}>
              Last run:{' '}
              {new Date(result.checked_at).toLocaleString(undefined, {
                month: 'short',
                day: 'numeric',
                hour: '2-digit',
                minute: '2-digit',
              })}
            </p>
          )}
        </div>
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-3 text-xs gap-1.5 shrink-0"
          onClick={() => doRun()}
          disabled={isRunning}
        >
          {isRunning ? (
            <>
              <SpinnerGap size={11} className="animate-spin" />
              Running...
            </>
          ) : (
            <>
              <ArrowCounterClockwise size={11} />
              Run diagnostics
            </>
          )}
        </Button>
      </div>

      {isLoading ? (
        <div
          className="h-24 rounded-lg border animate-pulse"
          style={{
            borderColor: 'var(--color-border)',
            backgroundColor: 'var(--color-surface-1)',
          }}
        />
      ) : isError ? (
        <div
          className="flex items-center gap-2.5 p-4 rounded-lg border"
          style={{
            borderColor: 'rgba(239,68,68,0.25)',
            backgroundColor: 'rgba(239,68,68,0.06)',
          }}
        >
          <XCircle size={16} weight="fill" style={{ color: 'var(--color-error)' }} />
          <p className="text-sm" style={{ color: 'var(--color-error)' }}>
            Could not load diagnostics. Run diagnostics to check your security posture.
          </p>
        </div>
      ) : result ? (
        <div className="space-y-4">
          <div
            className="rounded-lg border p-4 space-y-3"
            style={{
              borderColor: 'var(--color-border)',
              backgroundColor: 'var(--color-surface-1)',
            }}
          >
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {result.score >= 67 ? (
                  <ShieldCheck size={15} weight="fill" style={{ color: securityColor }} />
                ) : (
                  <ShieldWarning size={15} weight="fill" style={{ color: securityColor }} />
                )}
                <span className="text-sm font-medium" style={{ color: 'var(--color-secondary)' }}>
                  Security Score
                </span>
              </div>
              <div className="flex items-baseline gap-1">
                <span className="text-2xl font-headline font-bold" style={{ color: securityColor }}>
                  {result.score}
                </span>
                <span className="text-xs" style={{ color: 'var(--color-muted)' }}>/100</span>
                <span className="text-xs font-semibold ml-1.5" style={{ color: securityColor }}>
                  {getSecurityLabel(result.score)}
                </span>
              </div>
            </div>

            <div>
              <div
                className="w-full h-2 rounded-full overflow-hidden"
                style={{ backgroundColor: 'var(--color-surface-3)' }}
              >
                <div
                  className="h-full rounded-full transition-all duration-700"
                  data-testid="progress-bar"
                  style={{
                    width: `${result.score}%`,
                    backgroundColor: securityColor,
                  }}
                />
              </div>
              <div className="flex justify-between mt-1">
                <span className="text-[10px]" style={{ color: 'var(--color-muted)' }}>
                  0
                </span>
                <span className="text-[10px]" style={{ color: 'var(--color-muted)' }}>
                  100
                </span>
              </div>
            </div>

            {issuesByGroup && (
              <div className="flex items-center gap-4 pt-0.5">
                <div className="flex items-center gap-1.5 text-xs"
                  style={{ color: 'var(--color-error)' }}>
                  <XCircle size={11} weight="fill" />
                  {issuesByGroup.high.length} high
                </div>
                <div className="flex items-center gap-1.5 text-xs"
                  style={{ color: 'var(--color-warning)' }}>
                  <Warning size={11} weight="fill" />
                  {issuesByGroup.medium.length} medium
                </div>
                <div className="flex items-center gap-1.5 text-xs"
                  style={{ color: 'var(--color-info)' }}>
                  <Info size={11} weight="fill" />
                  {issuesByGroup.low.length} low
                </div>
              </div>
            )}
          </div>

          {result.issues.length > 0 && issuesByGroup && (
            <div className="space-y-3">
              {(['high', 'medium', 'low'] as const).map((severity) => {
                const issues = issuesByGroup[severity]
                if (issues.length === 0) return null
                const cfg = SEVERITY_CONFIG[severity]

                return (
                  <div key={severity} className="space-y-1.5">
                    <div className="flex items-center gap-1.5">
                      <cfg.Icon size={11} weight="fill" style={{ color: cfg.color }} />
                      <span
                        className="text-[10px] font-semibold uppercase tracking-wider"
                        style={{ color: cfg.color }}
                      >
                        {cfg.label} — {issues.length} issue{issues.length !== 1 ? 's' : ''}
                      </span>
                    </div>
                    {issues.map((issue) => (
                      <IssueCard
                        key={issue.id}
                        issue={issue}
                        config={cfg}
                        expanded={expandedIssue === issue.id}
                        onToggle={() =>
                          setExpandedIssue(expandedIssue === issue.id ? null : issue.id)
                        }
                      />
                    ))}
                  </div>
                )
              })}
            </div>
          )}

          {result.issues.length === 0 && (
            <div
              className="flex items-center gap-2.5 p-4 rounded-lg border"
              style={{
                borderColor: 'rgba(16,185,129,0.25)',
                backgroundColor: 'rgba(16,185,129,0.06)',
              }}
            >
              <ShieldCheck size={16} weight="fill" style={{ color: 'var(--color-success)' }} />
              <p className="text-sm" style={{ color: 'var(--color-success)' }}>
                No issues found — your security configuration looks great.
              </p>
            </div>
          )}
        </div>
      ) : (
        <div
          className="flex flex-col items-center justify-center gap-3 py-8 rounded-lg border border-dashed text-center"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <ShieldWarning size={28} weight="duotone" style={{ color: 'var(--color-muted)' }} />
          <div>
            <p className="text-sm" style={{ color: 'var(--color-secondary)' }}>
              No diagnostics run yet
            </p>
            <p className="text-xs mt-0.5" style={{ color: 'var(--color-muted)' }}>
              Run the doctor to check your security posture
            </p>
          </div>
        </div>
      )}
    </section>
  )
}

function IssueCard({
  issue,
  config,
  expanded,
  onToggle,
}: {
  issue: DoctorIssue
  config: (typeof SEVERITY_CONFIG)[keyof typeof SEVERITY_CONFIG]
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-expanded={expanded}
      onClick={onToggle}
      onKeyDown={(e) => e.key === 'Enter' && onToggle()}
      className="rounded-lg border p-3 space-y-0 cursor-pointer transition-opacity"
      style={{
        borderColor: config.borderColor,
        backgroundColor: config.bgColor,
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <p className="text-sm font-medium" style={{ color: 'var(--color-secondary)' }}>
          {issue.title}
        </p>
        <CaretRight
          size={12}
          style={{ color: config.color }}
          className={`shrink-0 mt-0.5 transition-transform duration-200 ${expanded ? 'rotate-90' : ''}`}
        />
      </div>

      {expanded && (
        <div className="space-y-2 mt-2 pt-2">
          <p className="text-xs leading-relaxed" style={{ color: 'var(--color-muted)' }}>
            {issue.description}
          </p>
          <Separator style={{ opacity: 0.3 }} />
          <div>
            <p
              className="text-[10px] font-semibold uppercase tracking-wider mb-1"
              style={{ color: 'var(--color-muted)' }}
            >
              Recommendation
            </p>
            <p className="text-xs leading-relaxed" style={{ color: 'var(--color-secondary)' }}>
              {issue.recommendation}
            </p>
          </div>
          {issue.action_link && (
            <a
              href={issue.action_link}
              onClick={(e) => e.stopPropagation()}
              className="inline-flex items-center gap-1 text-xs font-medium hover:underline underline-offset-2"
              style={{ color: config.color }}
            >
              <CaretRight size={10} />
              {issue.action_label ?? 'Fix this'}
            </a>
          )}
        </div>
      )}
    </div>
  )
}
