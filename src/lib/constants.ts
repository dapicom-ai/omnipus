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
export const AVATAR_COLORS = [
  '#D4AF37', '#10B981', '#3B82F6', '#8B5CF6',
  '#EF4444', '#F97316', '#EC4899', '#06B6D4',
]

/** Hint text for API key input fields, keyed by provider ID. */
export const PROVIDER_HINTS: Record<string, string> = {
  anthropic: 'Starts with sk-ant-...',
  openai: 'Starts with sk-...',
  google: 'API key from Google AI Studio',
  groq: 'Starts with gsk_...',
  openrouter: 'Starts with sk-or-v1-...',
}
