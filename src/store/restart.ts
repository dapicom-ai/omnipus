import { useQuery } from '@tanstack/react-query'
import { fetchPendingRestart, type PendingRestartEntry } from '@/lib/api'

export const PENDING_RESTART_QUERY_KEY = ['pending-restart'] as const

export const PENDING_RESTART_POLL_INTERVAL = 10_000

// ApiError extends Error with an optional HTTP status code, parsed from the
// "NNN: <text>" error message format that api.ts throws on non-2xx responses.
export interface ApiError extends Error {
  status?: number
}

// parseApiError extracts the HTTP status code from an error thrown by
// api.ts request(). The format is "<status>: <body text>".
function parseApiError(err: unknown): ApiError {
  if (err instanceof Error) {
    const match = /^(\d{3}):/.exec(err.message)
    const apiErr: ApiError = Object.assign(Object.create(Error.prototype), err)
    if (match) {
      apiErr.status = parseInt(match[1], 10)
    }
    return apiErr
  }
  return Object.assign(new Error(String(err)), { status: undefined })
}

export function usePendingRestart(): {
  entries: PendingRestartEntry[]
  isLoading: boolean
  isError: boolean
  error: ApiError | null
  refetch: () => Promise<unknown>
} {
  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: PENDING_RESTART_QUERY_KEY,
    queryFn: fetchPendingRestart,
    refetchInterval: PENDING_RESTART_POLL_INTERVAL,
  })

  return {
    entries: data ?? [],
    isLoading,
    isError,
    error: error ? parseApiError(error) : null,
    refetch,
  }
}
