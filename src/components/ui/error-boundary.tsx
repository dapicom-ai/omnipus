import { Component, type ReactNode } from 'react'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('[ErrorBoundary] Caught error:', error, info)
  }

  render() {
    if (this.state.hasError) {
      return this.props.fallback ?? (
        <div className="flex flex-col items-center justify-center p-8 gap-3 text-sm" style={{ color: 'var(--color-muted)' }}>
          <p style={{ color: 'var(--color-error)' }}>Something went wrong</p>
          <p className="text-xs">{this.state.error?.message}</p>
          <button
            onClick={() => this.setState({ hasError: false, error: null })}
            className="px-3 py-1.5 rounded-md text-xs border transition-colors"
            style={{ borderColor: 'var(--color-border)', color: 'var(--color-secondary)' }}
          >
            Try again
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
