// MermaidDiagram — lazy-loads mermaid.js and renders Mermaid code as SVG.
// Dark-themed: transparent background, Liquid Silver text, Forge Gold lines.

import { useEffect, useRef, useState, memo } from 'react'
import DOMPurify from 'dompurify'

interface MermaidDiagramProps {
  code: string
}

// Module-level flag to avoid re-initialising mermaid on every render.
// NOTE: This flag persists across HMR reloads in development — if you need to
// re-initialise after a hot reload, reset it manually in the browser console.
let initialized = false

async function getMermaid() {
  const m = (await import('mermaid')).default
  if (!initialized) {
    m.initialize({
      startOnLoad: false,
      securityLevel: 'loose',
      suppressErrorRendering: true,
      theme: 'dark',
      themeVariables: {
        background: 'transparent',
        primaryTextColor: '#E2E8F0',
        lineColor: '#D4AF37',
        primaryColor: '#1a1a2e',
        secondaryColor: '#16213e',
        tertiaryColor: '#0f3460',
        edgeLabelBackground: '#0A0A0B',
        clusterBkg: '#0A0A0B',
        titleColor: '#E2E8F0',
        nodeBorder: '#D4AF37',
        mainBkg: '#1a1a2e',
        fontFamily: 'Inter, sans-serif',
      },
    })
    initialized = true
  }
  return m
}

function MermaidDiagramImpl({ code }: MermaidDiagramProps) {
  const [svg, setSvg] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [renderFailed, setRenderFailed] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const idRef = useRef(`mermaid-${Math.random().toString(36).slice(2)}`)

  useEffect(() => {
    let cancelled = false

    async function render() {
      try {
        const m = await getMermaid()

        // Create a temporary off-screen container for mermaid to render into.
        // This avoids the ESM worker MIME issue by using DOM-based rendering.
        const tempDiv = document.createElement('div')
        tempDiv.id = idRef.current
        tempDiv.style.position = 'absolute'
        tempDiv.style.left = '-9999px'
        tempDiv.style.top = '-9999px'
        document.body.appendChild(tempDiv)

        try {
          const { svg: rendered } = await m.render(idRef.current, code.trim())
          if (!cancelled) setSvg(rendered)
        } finally {
          // Clean up temp element
          tempDiv.remove()
          // Also remove any mermaid-injected elements with matching ID
          document.getElementById('d' + idRef.current)?.remove()
        }
      } catch (e) {
        if (!cancelled) {
          const msg = e instanceof Error ? e.message : String(e)
          // Suppress non-fatal MIME/worker errors — mermaid ESM workers fail over HTTP
          if (msg.includes('MIME') || msg.includes('is not a valid') || msg.includes('text/html')) {
            console.warn('[mermaid] Worker error (expected over HTTP):', msg)
            if (!cancelled) setRenderFailed(true)
            return
          }
          setError(msg)
        }
      }
    }

    setSvg(null)
    setError(null)
    setRenderFailed(false)
    render()
    return () => {
      cancelled = true
    }
  }, [code])

  if (error) {
    return (
      <div className="my-2 rounded-md border border-[var(--color-error)]/30 bg-[var(--color-surface-2)] px-3 py-2 text-xs text-[var(--color-error)] font-mono">
        Diagram error: {error}
      </div>
    )
  }

  // Fallback: mermaid worker failed (MIME error over HTTP) — show as styled code block
  if (renderFailed) {
    return (
      <pre className="my-2 rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)] p-4 text-xs font-mono text-[var(--color-secondary)] overflow-x-auto whitespace-pre-wrap">
        <div className="text-[10px] text-[var(--color-accent)] mb-2 font-sans">mermaid diagram (live preview requires HTTPS)</div>
        {code}
      </pre>
    )
  }

  if (!svg && !error) {
    return (
      <div className="my-2 flex items-center gap-2 text-xs text-[var(--color-muted)] px-1">
        <span className="inline-block w-1.5 h-1.5 rounded-full bg-[var(--color-accent)] animate-pulse" />
        Rendering diagram...
      </div>
    )
  }

  return (
    <div
      className="my-3 flex justify-center overflow-x-auto rounded-lg bg-[var(--color-surface-2)] border border-[var(--color-border)] p-4"
      // Sanitize SVG before injection to prevent XSS via crafted mermaid code
      dangerouslySetInnerHTML={{
        __html: DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true }, ADD_TAGS: ['foreignObject'] }),
      }}
    />
  )
}

export const MermaidDiagram = memo(MermaidDiagramImpl)
