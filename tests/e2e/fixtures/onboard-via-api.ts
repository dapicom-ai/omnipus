import { type APIRequestContext, request } from '@playwright/test';

const DEFAULT_PROVIDER_ID = 'openrouter';
// Opus 4.7 — chosen for reliability in tool-use and multi-step subagent tests.
// Cheaper models (Haiku, Sonnet) were too flaky at the "call spawn exactly twice"
// and "subagent executes ≥3 tools in sequence" assertions the E2E suite makes.
// Cost: higher per-run, but deterministic CI > $0.50 saved per run.
const DEFAULT_MODEL = 'anthropic/claude-opus-4.7';
const DEFAULT_USERNAME = 'admin';
const DEFAULT_PASSWORD = 'admin123';

export interface OnboardingOptions {
  baseURL: string;
  providerID?: string;
  apiKey?: string;
  model?: string;
  username?: string;
  password?: string;
}

/**
 * Call POST /api/v1/onboarding/complete to seed an admin user + provider
 * without navigating the UI wizard. Bypasses the "Continue button stays
 * disabled because no model was auto-selected" trap in the UI flow.
 *
 * Contract from pkg/gateway/rest_onboarding.go:
 *   - Endpoint is CSRF-exempt (see rest_onboarding.go:310).
 *   - Body: { provider: {id, api_key, model}, admin: {username, password} }.
 *   - 200 on success, 409 if already complete — both are treated as success.
 *   - Password must be ≥8 characters.
 *
 * The API key is sourced from OPENROUTER_API_KEY_CI (or falls back to
 * OPENROUTER_API_KEY for local runs). If neither is set, the call uses a
 * placeholder that will likely cause provider tests to fail — but onboarding
 * itself only stores the key, it does not validate it. Matches the upstream
 * UI flow behavior.
 */
export async function onboardViaAPI(opts: OnboardingOptions): Promise<void> {
  const apiKey =
    opts.apiKey ??
    process.env.OPENROUTER_API_KEY_CI ??
    process.env.OPENROUTER_API_KEY ??
    'sk-test-placeholder';

  const ctx: APIRequestContext = await request.newContext({ baseURL: opts.baseURL });
  try {
    const res = await ctx.post('/api/v1/onboarding/complete', {
      data: {
        provider: {
          id: opts.providerID ?? DEFAULT_PROVIDER_ID,
          api_key: apiKey,
          model: opts.model ?? DEFAULT_MODEL,
        },
        admin: {
          username: opts.username ?? DEFAULT_USERNAME,
          password: opts.password ?? DEFAULT_PASSWORD,
        },
      },
    });

    // 200 = fresh onboard; 409 = already complete on this $OMNIPUS_HOME (e.g.
    // second test shard hitting the same instance). Both mean "admin exists,
    // login will succeed" — no need to surface a failure.
    if (res.status() === 200 || res.status() === 409) {
      return;
    }

    const body = await res.text();
    throw new Error(
      `onboard-via-api: POST /api/v1/onboarding/complete returned ${res.status()}: ${body}`,
    );
  } finally {
    await ctx.dispose();
  }
}
