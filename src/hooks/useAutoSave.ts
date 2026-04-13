import { useCallback, useEffect, useRef, useState } from 'react'

export type AutoSaveStatus = 'idle' | 'saving' | 'saved' | 'error'

interface UseAutoSaveOptions {
  /** Debounce delay in ms. Default: 500 */
  debounceMs?: number
  /** If true, auto-save is disabled (e.g., for locked agents) */
  disabled?: boolean
}

interface UseAutoSaveResult {
  status: AutoSaveStatus
  error: string | undefined
  /** Call this to trigger an immediate save (no debounce) */
  saveNow: () => void
}

/**
 * useAutoSave — debounced auto-save hook.
 *
 * Watches `data` for changes (deep compare via JSON.stringify).
 * After the debounce period, calls `saveFn` with the current data.
 * Skips the initial render (loading data is not a change).
 *
 * Usage:
 *   const { status } = useAutoSave(formData, (data) => updateAgent(id, data))
 */
export function useAutoSave<T>(
  data: T,
  saveFn: (data: T) => Promise<unknown>,
  options?: UseAutoSaveOptions,
): UseAutoSaveResult {
  const { debounceMs = 500, disabled = false } = options ?? {}
  const [status, setStatus] = useState<AutoSaveStatus>('idle')
  const [error, setError] = useState<string>()

  // Track whether initial hydration has happened.
  const initializedRef = useRef(false)
  const previousJsonRef = useRef<string>('')
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const latestDataRef = useRef<T>(data)
  const saveFnRef = useRef(saveFn)
  saveFnRef.current = saveFn
  latestDataRef.current = data

  const doSave = useCallback(async () => {
    if (disabled) return
    setStatus('saving')
    setError(undefined)
    try {
      await saveFnRef.current(latestDataRef.current)
      setStatus('saved')
      // Fade back to idle after 2s
      setTimeout(() => setStatus((s) => (s === 'saved' ? 'idle' : s)), 2000)
    } catch (err) {
      setStatus('error')
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [disabled])

  useEffect(() => {
    if (disabled) return

    const json = JSON.stringify(data)

    // Skip first render (initial load).
    if (!initializedRef.current) {
      initializedRef.current = true
      previousJsonRef.current = json
      return
    }

    // Skip if data hasn't changed.
    if (json === previousJsonRef.current) return
    previousJsonRef.current = json

    // Clear previous debounce timer.
    if (timerRef.current) clearTimeout(timerRef.current)

    // Set new debounce timer.
    timerRef.current = setTimeout(doSave, debounceMs)

    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [data, debounceMs, disabled, doSave])

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [])

  return { status, error, saveNow: doSave }
}
