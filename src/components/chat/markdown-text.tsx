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

  // Lists — explicit styles since Tailwind v4 doesn't include @tailwindcss/typography prose by default
  ul: ({ children, ...props }) => (
    <ul {...props} style={{ listStyleType: 'disc' }} className="pl-6 my-2 space-y-1 text-[var(--color-secondary)]">{children}</ul>
  ),
  ol: ({ children, ...props }) => (
    <ol {...props} style={{ listStyleType: 'decimal' }} className="pl-6 my-2 space-y-1 text-[var(--color-secondary)]">{children}</ol>
  ),
  li: ({ children, ...props }) => (
    <li {...props} style={{ display: 'list-item' }} className="text-sm leading-relaxed">{children}</li>
  ),

  // Headings — sized distinctly from body text
  h1: ({ children, ...props }) => (
    <h1 {...props} className="text-xl font-bold text-[var(--color-secondary)] mt-5 mb-2 border-b border-[var(--color-border)] pb-1">{children}</h1>
  ),
  h2: ({ children, ...props }) => (
    <h2 {...props} className="text-lg font-semibold text-[var(--color-secondary)] mt-4 mb-2">{children}</h2>
  ),
  h3: ({ children, ...props }) => (
    <h3 {...props} className="text-base font-semibold text-[var(--color-secondary)] mt-3 mb-1">{children}</h3>
  ),

  // Paragraphs
  p: ({ children, ...props }) => (
    <p {...props} className="text-sm leading-relaxed my-1.5">{children}</p>
  ),

  // Blockquotes
  blockquote: ({ children, ...props }) => (
    <blockquote {...props} className="border-l-2 border-[var(--color-accent)]/50 pl-3 my-2 text-[var(--color-muted)] italic">{children}</blockquote>
  ),

  // Tables
  table: ({ children, ...props }) => (
    <div className="overflow-x-auto my-2">
      <table {...props} className="min-w-full text-xs border-collapse">{children}</table>
    </div>
  ),
  th: ({ children, ...props }) => (
    <th {...props} className="border border-[var(--color-border)] px-3 py-1.5 text-left font-semibold bg-[var(--color-surface-2)] text-[var(--color-secondary)]">{children}</th>
  ),
  td: ({ children, ...props }) => (
    <td {...props} className="border border-[var(--color-border)] px-3 py-1.5 text-[var(--color-secondary)]">{children}</td>
  ),

  // Horizontal rule
  hr: (props) => (
    <hr {...props} className="my-4 border-[var(--color-border)]" />
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
