import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Gauge, ArrowClockwise, Warning } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { fetchRateLimits } from '@/lib/api'
import { useUiStore } from '@/store/ui'
import { cn } from '@/lib/utils'

function costColor(ratio: number): string {
  if (ratio >= 0.8) return 'bg-red-500'
  if (ratio >= 0.5) return 'bg-amber-500'
  if (ratio > 0) return 'bg-emerald-500'
  return 'bg-[var(--color-muted)]'
}

function formatLimit(value: number, unit: string): string {
  return value > 0 ? `${value}/${unit}` : 'No limit'
}

interface DailyCostBarProps {
  cap: number
  cost: number
}

function DailyCostBar({ cap, cost }: DailyCostBarProps) {
  const rawRatio = cap > 0 ? cost / cap : 0
  const clampedPct = Math.round(Math.min(rawRatio, 1) * 100)
  const exceeded = cap > 0 && cost > cap

  return (
    <>
      <div className="flex items-center justify-between text-[10px] text-[var(--color-muted)]">
        <span>Daily cost</span>
        <span className={cn(exceeded && 'text-red-400 font-semibold')}>
          ${cost.toFixed(2)} / ${cap.toFixed(2)}
          {exceeded && ' — Exceeded'}
        </span>
      </div>
      {/* Render a custom bar so we can change fill colour without a prop chain */}
      <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-[var(--color-surface-3)]">
        <div
          className={cn('h-full rounded-full transition-all duration-500', costColor(rawRatio))}
          style={{ width: `${clampedPct}%` }}
        />
      </div>
    </>
  )
}

export function RateLimitStatusCard() {
  const addToast = useUiStore((s) => s.addToast)
  const warnedRef = useRef<'none' | 'warning' | 'exceeded'>('none')

  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ['rate-limits'],
    queryFn: fetchRateLimits,
    refetchInterval: 10_000,
  })

  useEffect(() => {
    if (!data || !data.enabled || data.daily_cost_cap <= 0) return
    const ratio = data.daily_cost_usd / data.daily_cost_cap

    // Reset the warned state when the ratio drops back below the warning
    // threshold. This handles UTC day rollover (cost resets to 0), cap raises,
    // and any other reason the ratio would fall — without this reset the toast
    // only fires once per component lifetime, which means day 2+ gets no
    // warnings even when blowing through the cap again.
    if (ratio < 0.8 && warnedRef.current !== 'none') {
      warnedRef.current = 'none'
      return
    }

    if (ratio >= 1.0 && warnedRef.current !== 'exceeded') {
      warnedRef.current = 'exceeded'
      addToast({
        message: `Daily cost cap exceeded: $${data.daily_cost_usd.toFixed(2)} / $${data.daily_cost_cap.toFixed(2)}`,
        variant: 'error',
      })
    } else if (ratio >= 0.8 && ratio < 1.0 && warnedRef.current === 'none') {
      warnedRef.current = 'warning'
      addToast({
        message: `Approaching daily cost cap: $${data.daily_cost_usd.toFixed(2)} / $${data.daily_cost_cap.toFixed(2)} (${Math.round(ratio * 100)}%)`,
        variant: 'default',
      })
    }
  }, [data, addToast])

  function renderBody() {
    if (isError) {
      return (
        <div className="flex items-center gap-1.5 text-xs text-[var(--color-error)]">
          <Warning size={12} />
          <span>Could not load rate limit status.</span>
          <button
            type="button"
            onClick={() => void refetch()}
            className="underline hover:no-underline ml-1"
          >
            Retry
          </button>
        </div>
      )
    }

    if (isLoading) {
      return (
        <div className="space-y-2">
          <div className="h-2 w-full rounded-full bg-[var(--color-surface-2)] animate-pulse" />
          <div className="flex gap-4">
            <div className="h-3 w-24 rounded bg-[var(--color-surface-2)] animate-pulse" />
            <div className="h-3 w-24 rounded bg-[var(--color-surface-2)] animate-pulse" />
          </div>
        </div>
      )
    }

    if (!data?.enabled) {
      return (
        <p className="text-xs text-[var(--color-muted)]">
          Rate limiting is disabled. Set limits in sandbox.rate_limits.
        </p>
      )
    }

    return (
      <div className="space-y-2.5">
        <div className="space-y-1">
          <DailyCostBar cap={data.daily_cost_cap} cost={data.daily_cost_usd} />
        </div>

        {/* Per-agent limits */}
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-[10px] text-[var(--color-muted)]">
          <span>
            LLM calls:{' '}
            <span className="text-[var(--color-secondary)]">
              {formatLimit(data.max_agent_llm_calls_per_hour, 'hour')}
            </span>
          </span>
          <span>
            Tool calls:{' '}
            <span className="text-[var(--color-secondary)]">
              {formatLimit(data.max_agent_tool_calls_per_minute, 'minute')}
            </span>
          </span>
        </div>
      </div>
    )
  }

  return (
    <div className="border-b border-[var(--color-border)] px-4 py-3">
      {/* Header */}
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-1.5">
          <Gauge size={13} className="text-[var(--color-muted)]" />
          <span className="text-xs font-semibold text-[var(--color-secondary)]">Rate Limits</span>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 text-[var(--color-muted)] hover:text-[var(--color-secondary)]"
          onClick={() => void refetch()}
          disabled={isFetching}
          aria-label="Refresh rate limit status"
          title="Refresh"
        >
          <ArrowClockwise size={12} className={cn(isFetching && 'animate-spin')} />
        </Button>
      </div>

      {renderBody()}
    </div>
  )
}
