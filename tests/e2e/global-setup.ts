import { chromium } from '@playwright/test';
import path from 'path';
import { fileURLToPath } from 'url';
import { loginAs } from './fixtures/login.js';
import { onboardViaAPI } from './fixtures/onboard-via-api.js';

const AUTH_FILE = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  'fixtures/.auth/admin.json',
);

// T0.4: Preflight — fail fast if required environment variables are missing.
// OPENROUTER_API_KEY_CI is required. Its absence is a CI configuration failure,
// not a per-test skip condition. Tests that need a real LLM will fail anyway;
// this preflight surfaces the root cause immediately instead of after 60s timeouts.
function preflightCheck(): void {
  if (!process.env.OPENROUTER_API_KEY_CI) {
    throw new Error(
      '[E2E preflight] OPENROUTER_API_KEY_CI is not set.\n' +
      'This environment variable is REQUIRED for the E2E suite.\n' +
      'Tests that require a live LLM will fail without it, and soft-skipping\n' +
      'on its absence is no longer permitted (see tests/e2e/README.md).\n\n' +
      'To fix:\n' +
      '  export OPENROUTER_API_KEY_CI="<your OpenRouter key>"\n' +
      'Or in CI: add OPENROUTER_API_KEY_CI to your secrets and inject it\n' +
      'as an env var in the Playwright workflow step (see .github/workflows/pr.yml).',
    );
  }
}

/**
 * Global setup: authenticate once and persist the storage state.
 *
 * Idempotency guard: if the banner landmark is already visible after
 * navigation to `/`, we are already authenticated — save state and return.
 * This prevents accidental double-onboarding on test retries.
 *
 * TOKEN MIGRATION: The SPA stores omnipus_auth_token in sessionStorage (XSS
 * protection), but Playwright's storageState only captures localStorage.
 * After successful login we copy the token from sessionStorage to localStorage
 * so it is included in the storageState snapshot and survives across test contexts.
 *
 * NOTE: nav[aria-label="Main navigation"] is NOT used here — the sidebar is an
 * overlay drawer and only renders while open. The top-level <header> (implicit
 * ARIA role "banner") is the canonical auth indicator.
 */
async function globalSetup(): Promise<void> {
  // T0.4: Run preflight checks before any browser/gateway interaction.
  preflightCheck();

  const baseURL = process.env.OMNIPUS_URL || 'http://localhost:6060';

  // Seed the admin user + provider via the REST onboarding endpoint so the
  // browser flow enters straight into the login form rather than the 4-step
  // wizard. The wizard's "Continue" button stays disabled when no model is
  // auto-selected in CI, which was the local-run blocker. The API call is
  // idempotent: 200 or 409 both mean "admin exists, proceed".
  await onboardViaAPI({ baseURL });

  const browser = await chromium.launch();
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();

  await page.goto('/');

  // Idempotency: if already authenticated, skip login flow
  if (!(await page.getByRole('banner').isVisible({ timeout: 3_000 }))) {
    await loginAs(page, 'admin', 'admin123');
  }

  // Copy the auth token from sessionStorage to localStorage so storageState captures it.
  // The SPA stores omnipus_auth_token in sessionStorage for XSS protection, but
  // Playwright storageState cannot capture sessionStorage. We mirror it to localStorage
  // at setup time so authenticated tests can bootstrap from storageState.
  await page.evaluate(() => {
    const token = sessionStorage.getItem('omnipus_auth_token');
    if (token) {
      localStorage.setItem('omnipus_auth_token', token);
    }
  });

  await context.storageState({ path: AUTH_FILE });
  await browser.close();
}

export default globalSetup;
