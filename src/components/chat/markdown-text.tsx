// MarkdownText — AssistantUI-aware markdown renderer.
// Reads text from MessagePrimitive context (no children prop needed).
// Uses MarkdownTextPrimitive from @assistant-ui/react-markdown with:
//   • Shiki syntax highlighting (vitesse-dark) + Mermaid diagram rendering
//   • Copy button on code blocks
//   • remark-gfm (tables, strikethrough, task lists)
//   • remark-math + rehype-katex (LaTeX/math rendering)
//   • rehype-phosphor-emoji (emoji → Phosphor icons)
//   • Image lightbox (click to expand)
//   • Sovereign Deep styling for inline code and links

import { memo, useState } from 'react'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeKatex from 'rehype-katex'
import 'katex/dist/katex.min.css'
import {
  MarkdownTextPrimitive,
  unstable_memoizeMarkdownComponents as memoizeMarkdownComponents,
} from '@assistant-ui/react-markdown'
import { SyntaxHighlighter, CopyCodeHeader } from './shiki-highlighter'
import { ImageLightbox } from './image-lightbox'
import { rehypePhosphorEmoji } from '@/lib/rehype-phosphor-emoji'
import * as PhosphorIcons from '@phosphor-icons/react'
import type { ComponentPropsWithoutRef } from 'react'

// ── Phosphor icon span renderer ───────────────────────────────────────────────
// Renders <span data-phosphor-icon="IconName"> as the corresponding Phosphor icon.

function PhosphorEmojiSpan({ 'data-phosphor-icon': iconName, children, ...props }: ComponentPropsWithoutRef<'span'> & { 'data-phosphor-icon'?: string }) {
  if (iconName && iconName in PhosphorIcons) {
    const Icon = (PhosphorIcons as Record<string, React.ComponentType<{ size?: number; weight?: string; className?: string }>>)[iconName]
    return <Icon size={14} weight="regular" className="inline-block align-middle text-[var(--color-accent)] mx-0.5" />
  }
  return <span {...props}>{children}</span>
}

// ── Image renderer with lightbox ──────────────────────────────────────────────

function MarkdownImage({ src, alt }: ComponentPropsWithoutRef<'img'>) {
  const [open, setOpen] = useState(false)
  if (!src) return null
  return (
    <>
      <img
        src={src}
        alt={alt || ''}
        className="max-w-full rounded-md cursor-zoom-in border border-[var(--color-border)] hover:border-[var(--color-accent)]/50 transition-colors"
        onClick={() => setOpen(true)}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => e.key === 'Enter' && setOpen(true)}
        aria-label={alt ? `View: ${alt}` : 'View image'}
      />
      {open && <ImageLightbox src={src} alt={alt} onClose={() => setOpen(false)} />}
    </>
  )
}

// ── Component map ─────────────────────────────────────────────────────────────
// memoizeMarkdownComponents wraps each renderer with React.memo and compares
// the AST node for bailout — this is performance-critical for streaming.

const markdownComponents = memoizeMarkdownComponents({
  // Shiki-powered block code (replaces default <pre><code> rendering).
  // Also handles language="mermaid" by routing to MermaidDiagram.
  SyntaxHighlighter,

  // Language label + copy button above each code block
  CodeHeader: CopyCodeHeader,

  // Inline code (distinct from block code, which goes through SyntaxHighlighter)
  code: ({ children, ...props }) => (
    <code
      {...props}
      className="font-mono text-[11px] bg-[var(--color-surface-2)] px-1.5 py-0.5 rounded text-[var(--color-accent)]"
    >
      {children}
    </code>
  ),

  // Links open in new tab
  a: ({ href, children, ...props }) => (
    <a
      {...props}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-[var(--color-accent)] underline underline-offset-2 hover:opacity-80 transition-opacity"
    >
      {children}
    </a>
  ),

  // Span renderer: intercepts data-phosphor-icon spans from rehypePhosphorEmoji
  span: PhosphorEmojiSpan,

  // Images: click-to-expand lightbox
  img: MarkdownImage,
})

// ── MarkdownText component ────────────────────────────────────────────────────
// Usage: <MarkdownText /> inside MessagePrimitive.Parts (reads text from context)

function MarkdownTextImpl() {
  return (
    <MarkdownTextPrimitive
      remarkPlugins={[remarkGfm, remarkMath]}
      rehypePlugins={[rehypeKatex, rehypePhosphorEmoji]}
      className="prose-sm prose-invert max-w-none text-[var(--color-secondary)] leading-relaxed"
      components={markdownComponents}
      smooth
    />
  )
}

export const MarkdownText = memo(MarkdownTextImpl)
