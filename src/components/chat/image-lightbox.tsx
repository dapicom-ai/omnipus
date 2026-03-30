// ImageLightbox — full-screen overlay for clicking images in markdown.
// Close on backdrop click or Escape key.

import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { X } from '@phosphor-icons/react'

interface ImageLightboxProps {
  src: string
  alt?: string
  onClose: () => void
}

export function ImageLightbox({ src, alt, onClose }: ImageLightboxProps) {
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [onClose])

  return createPortal(
    <div
      className="fixed inset-0 z-[200] flex items-center justify-center bg-black/85 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal
      aria-label={alt || 'Image preview'}
    >
      {/* Close button */}
      <button
        type="button"
        className="absolute top-4 right-4 w-9 h-9 flex items-center justify-center rounded-full bg-[var(--color-surface-2)] border border-[var(--color-border)] text-[var(--color-secondary)] hover:bg-[var(--color-surface-3)] transition-colors z-10"
        onClick={onClose}
        aria-label="Close image preview"
      >
        <X size={16} weight="bold" />
      </button>

      {/* Image — stop propagation so clicking the image itself doesn't close */}
      <img
        src={src}
        alt={alt || ''}
        className="max-w-[90vw] max-h-[90vh] rounded-lg object-contain shadow-2xl ring-1 ring-[var(--color-border)]"
        onClick={(e) => e.stopPropagation()}
      />

      {/* Alt caption */}
      {alt && (
        <p className="absolute bottom-6 left-1/2 -translate-x-1/2 text-xs text-[var(--color-muted)] bg-[var(--color-surface-2)]/80 px-3 py-1.5 rounded-full">
          {alt}
        </p>
      )}
    </div>,
    document.body,
  )
}
