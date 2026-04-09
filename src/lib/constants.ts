// Shared constants and utilities used across multiple components

/** Generate a unique ID. Uses crypto.randomUUID() in secure contexts (HTTPS),
 *  falls back to a timestamp+random string for HTTP contexts. */
export function generateId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  // Fallback for non-secure contexts (HTTP)
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

/** Avatar color palette for agent creation and display. */
export const AVATAR_COLORS = ['#22C55E', '#3B82F6', '#A855F7', '#EAB308', '#F97316', '#EF4444', '#6B7280', '#D4AF37']

/** Hint text for API key input fields, keyed by provider ID. */
export const PROVIDER_HINTS: Record<string, string> = {
  anthropic: 'Starts with sk-ant-...',
  openai: 'Starts with sk-...',
  groq: 'Starts with gsk_...',
  openrouter: 'Starts with sk-or-v1-...',
}
