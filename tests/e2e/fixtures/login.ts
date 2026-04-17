import { type Page, expect } from '@playwright/test';

export interface Credentials {
  username: string;
  password: string;
}

/**
 * Return true when the user is already authenticated (nav is visible).
 * This is the single authoritative signal — no URL guessing.
 */
async function isAuthenticated(page: Page): Promise<boolean> {
  const nav = page.locator('nav[aria-label="Main navigation"]');
  return nav.isVisible();
}

/**
 * Complete the 4-step onboarding wizard with EXACT selectors from the SPA.
 *
 * Step 1 — "Get Started"
 * Step 2 — Pick OpenRouter → fill #onboarding-api-key → "Connect & Load Models"
 *           (on success the "Continue" button becomes enabled)
 * Step 3 — Fill #admin-username / #admin-password / #admin-password-confirm → "Create Account"
 * Step 4 — "Start Exploring"
 *
 * The API key is sourced from OPENROUTER_API_KEY_CI env var; tests will fail
 * with a real connection error if it is absent — that is intentional.
 */
async function completeOnboarding(page: Page, creds: Credentials): Promise<void> {
  const apiKey = process.env.OPENROUTER_API_KEY_CI ?? 'sk-test-placeholder';

  // ── Step 1 ────────────────────────────────────────────────────────────────
  await expect(page).toHaveURL(/onboarding/, { timeout: 15_000 });
  await page.getByRole('button', { name: 'Get Started' }).click();

  // ── Step 2 — Provider ─────────────────────────────────────────────────────
  // Click the OpenRouter provider button (exact display_name from AVAILABLE_PROVIDERS)
  await page.getByRole('button', { name: /OpenRouter/i }).click();

  // Enter the API key using the ID selector confirmed in onboarding.tsx:562
  await expect(page.locator('#onboarding-api-key')).toBeVisible({ timeout: 8_000 });
  await page.locator('#onboarding-api-key').fill(apiKey);

  // "Connect & Load Models" is the CTA before model selection (onboarding.tsx:609)
  await page.getByRole('button', { name: 'Connect & Load Models' }).click();

  // After a successful connection the "Continue" button appears (onboarding.tsx:662-669).
  // Wait for it to become enabled (disabled until testStatus==='success' && selectedModel).
  const continueBtn = page.getByRole('button', { name: 'Continue' });
  await expect(continueBtn).toBeEnabled({ timeout: 30_000 });
  await continueBtn.click();

  // ── Step 3 — Admin account ────────────────────────────────────────────────
  await expect(page.locator('#admin-username')).toBeVisible({ timeout: 10_000 });
  await page.locator('#admin-username').fill(creds.username);
  await page.locator('#admin-password').fill(creds.password);
  await page.locator('#admin-password-confirm').fill(creds.password);
  await page.getByRole('button', { name: 'Create Account' }).click();

  // ── Step 4 — Done ─────────────────────────────────────────────────────────
  await expect(page.getByRole('button', { name: 'Start Exploring' })).toBeVisible({ timeout: 15_000 });
  await page.getByRole('button', { name: 'Start Exploring' }).click();

  // Post-condition: nav is visible = authenticated
  await expect(page.locator('nav[aria-label="Main navigation"]')).toBeVisible({ timeout: 15_000 });
}

async function completeLoginForm(page: Page, creds: Credentials): Promise<void> {
  // Use the exact IDs confirmed in login.tsx:110 and :130
  await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
  await page.locator('#login-username').fill(creds.username);
  await page.locator('#login-password').fill(creds.password);

  // Submit button text is "Sign in" (login.tsx:168)
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page).not.toHaveURL(/login/, { timeout: 15_000 });
}

/**
 * Bring the page to an authenticated state.
 *
 * Idempotent: if the nav is already visible, returns immediately.
 * Detects onboarding vs login form and handles both paths.
 */
export async function loginAs(page: Page, username = 'admin', password = 'admin123'): Promise<void> {
  const creds: Credentials = { username, password };

  await page.goto('/');

  // Fast-path: already authenticated
  if (await isAuthenticated(page)) {
    return;
  }

  const url = page.url();

  if (url.includes('/onboarding')) {
    await completeOnboarding(page, creds);
    return;
  }

  // On the login form the URL is /login or the page shows #login-username
  const loginUsername = page.locator('#login-username');
  if (await loginUsername.isVisible({ timeout: 5_000 })) {
    await completeLoginForm(page, creds);
    return;
  }

  // Fallback: check for onboarding button on the root route (redirected)
  const getStartedBtn = page.getByRole('button', { name: 'Get Started' });
  if (await getStartedBtn.isVisible({ timeout: 5_000 })) {
    await completeOnboarding(page, creds);
    return;
  }

  // If we reach here without nav, something is wrong — propagate the failure
  await expect(page.locator('nav[aria-label="Main navigation"]')).toBeVisible({ timeout: 15_000 });
}
