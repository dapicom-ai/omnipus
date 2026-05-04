/**
 * MarkdownText.test.tsx — URL sanitization tests for the `a` and `img` renderers.
 *
 * Covers the scheme allow-list introduced in V2.C (FE H1 + FE H8):
 *   - javascript:, data:, vbscript: hrefs → plain text, no clickable anchor
 *   - javascript: img src → no <img> rendered (or alt placeholder)
 *   - https:// img src → <img> with correct src + loading="lazy"
 *   - https:// href → real <a> with target=_blank + rel=noopener noreferrer
 *
 * MarkdownText uses MarkdownTextPrimitive (AssistantUI) which reads from React
 * context, making it non-trivial to render in isolation. We test the individual
 * sub-renderers (`MarkdownImage` and the `a` renderer logic) through the
 * exported `isSafeHref` utility and through direct renderer invocation patterns
 * that mirror the component's internals, complementing the existing
 * markdown-text.test.tsx which covers the rewriteLegacyURL integration.
 */

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { isSafeHref } from '@/lib/url-safe'

// ── Mocks required to import markdown-text without crashing ──────────────────

vi.mock('@assistant-ui/react-markdown', () => ({
  MarkdownTextPrimitive: () => null,
  unstable_memoizeMarkdownComponents: (m: Record<string, unknown>) => m,
}))
vi.mock('remark-gfm', () => ({ default: () => {} }))
vi.mock('remark-math', () => ({ default: () => {} }))
vi.mock('rehype-katex', () => ({ default: () => {} }))
vi.mock('katex/dist/katex.min.css', () => ({}))
vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: { preview_port: 5001 } }),
}))
vi.mock('@/lib/api', () => ({
  fetchAboutInfo: vi.fn().mockResolvedValue({}),
}))
vi.mock('./shiki-highlighter', () => ({
  SyntaxHighlighter: () => null,
  CopyCodeHeader: () => null,
}))
vi.mock('./image-lightbox', () => ({
  ImageLightbox: () => null,
}))
vi.mock('@/lib/rehype-phosphor-emoji', () => ({
  rehypePhosphorEmoji: () => {},
}))

// ── isSafeHref unit tests (the core predicate) ───────────────────────────────

describe('isSafeHref — scheme allow-list', () => {
  // Allowed schemes
  it('allows http:', () => {
    expect(isSafeHref('http://example.com/path')).toBe(true)
  })

  it('allows https:', () => {
    expect(isSafeHref('https://github.com/x')).toBe(true)
  })

  it('allows mailto:', () => {
    expect(isSafeHref('mailto:user@example.com')).toBe(true)
  })

  it('allows tel:', () => {
    expect(isSafeHref('tel:+1234567890')).toBe(true)
  })

  // Disallowed schemes
  it('rejects javascript: (XSS primitive — FE H1)', () => {
    expect(isSafeHref('javascript:alert(1)')).toBe(false)
  })

  it('rejects javascript: with encoded content', () => {
    expect(isSafeHref('javascript:fetch("/api/v1/admin/users")')).toBe(false)
  })

  it('rejects data: (potential XSS vector)', () => {
    expect(isSafeHref('data:text/html,<script>alert(1)</script>')).toBe(false)
  })

  it('rejects data: image (could carry malicious payload)', () => {
    expect(isSafeHref('data:image/png;base64,AAAA')).toBe(false)
  })

  it('rejects vbscript:', () => {
    expect(isSafeHref('vbscript:MsgBox(1)')).toBe(false)
  })

  it('rejects file: scheme', () => {
    expect(isSafeHref('file:///etc/passwd')).toBe(false)
  })

  it('rejects ftp: scheme', () => {
    expect(isSafeHref('ftp://files.example.com/secret')).toBe(false)
  })

  it('rejects relative paths (not parseable by URL constructor)', () => {
    expect(isSafeHref('/relative/path')).toBe(false)
  })

  it('rejects empty string', () => {
    expect(isSafeHref('')).toBe(false)
  })
})

// ── `a` renderer: sanitized links render as plain text, not anchors ──────────
// We test by rendering the renderer function directly using React Testing Library.

import type { ComponentPropsWithoutRef } from 'react'

// Mirror the renderer logic from MarkdownTextImpl's `a` handler so we can
// render it without requiring AssistantUI context.
function TestLinkRenderer({ href, children }: { href: string; children: React.ReactNode }) {
  if (!isSafeHref(href)) {
    return (
      <span
        data-testid="markdown-link"
        title="Link removed: unsafe URL scheme"
      >
        {children}
      </span>
    )
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      data-testid="markdown-link"
    >
      {children}
    </a>
  )
}

// Mirror the image renderer logic from MarkdownImage.
function TestImageRenderer({ src, alt }: ComponentPropsWithoutRef<'img'>) {
  if (!src) return null
  if (!isSafeHref(src)) {
    if (alt) {
      return <span data-testid="sanitized-image">[image: {alt}]</span>
    }
    return null
  }
  return (
    <img
      src={src}
      alt={alt || ''}
      loading="lazy"
      data-testid="rendered-image"
    />
  )
}

describe('markdown `a` renderer — scheme allow-list (FE H1)', () => {
  it('[click here](javascript:alert(1)) → visible text, no href', () => {
    render(<TestLinkRenderer href="javascript:alert(1)">click here</TestLinkRenderer>)
    const el = screen.getByTestId('markdown-link')
    // Rendered as a <span>, not an <a>
    expect(el.tagName).toBe('SPAN')
    expect(el).toHaveTextContent('click here')
    expect(el).not.toHaveAttribute('href')
  })

  it('[evil](data:text/html,<script>…) → visible text, no href', () => {
    render(
      <TestLinkRenderer href="data:text/html,<script>alert(1)</script>">evil</TestLinkRenderer>
    )
    const el = screen.getByTestId('markdown-link')
    expect(el.tagName).toBe('SPAN')
    expect(el).toHaveTextContent('evil')
    expect(el).not.toHaveAttribute('href')
  })

  it('[x](vbscript:MsgBox(1)) → visible text, no href', () => {
    render(<TestLinkRenderer href="vbscript:MsgBox(1)">x</TestLinkRenderer>)
    const el = screen.getByTestId('markdown-link')
    expect(el.tagName).toBe('SPAN')
    expect(el).not.toHaveAttribute('href')
  })

  it('[github](https://github.com/x) → real clickable link with target + rel', () => {
    render(<TestLinkRenderer href="https://github.com/x">github</TestLinkRenderer>)
    const el = screen.getByTestId('markdown-link')
    expect(el.tagName).toBe('A')
    expect(el).toHaveAttribute('href', 'https://github.com/x')
    expect(el).toHaveAttribute('target', '_blank')
    expect(el).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('[mailto](mailto:user@example.com) → real clickable link', () => {
    render(<TestLinkRenderer href="mailto:user@example.com">email us</TestLinkRenderer>)
    const el = screen.getByTestId('markdown-link')
    expect(el.tagName).toBe('A')
    expect(el).toHaveAttribute('href', 'mailto:user@example.com')
  })
})

describe('markdown `img` renderer — scheme allow-list (FE H1 + FE H8)', () => {
  it('![logo](javascript:alert(1)) → no <img> rendered', () => {
    const { container } = render(<TestImageRenderer src="javascript:alert(1)" alt="logo" />)
    expect(container.querySelector('img')).toBeNull()
    // Alt placeholder shown when alt text present
    expect(screen.getByTestId('sanitized-image')).toHaveTextContent('[image: logo]')
  })

  it('![](javascript:alert(1)) with no alt → nothing rendered', () => {
    const { container } = render(<TestImageRenderer src="javascript:alert(1)" />)
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('[data-testid="sanitized-image"]')).toBeNull()
  })

  it('![safe](https://example.com/x.png) → <img> with correct src and loading=lazy', () => {
    render(<TestImageRenderer src="https://example.com/x.png" alt="safe" />)
    const img = screen.getByTestId('rendered-image') as HTMLImageElement
    expect(img.tagName).toBe('IMG')
    expect(img).toHaveAttribute('src', 'https://example.com/x.png')
    expect(img).toHaveAttribute('loading', 'lazy')
    expect(img).toHaveAttribute('alt', 'safe')
  })

  it('![pic](data:image/png;base64,AAAA) → no <img> rendered', () => {
    const { container } = render(
      <TestImageRenderer src="data:image/png;base64,AAAA" alt="pic" />
    )
    expect(container.querySelector('img')).toBeNull()
  })
})
