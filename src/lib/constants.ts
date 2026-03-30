// Shared constants used across multiple components

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
