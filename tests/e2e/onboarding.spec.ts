import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Onboarding tests run with a clean session — no pre-auth state.
test.use({ storageState: { cookies: [], origins: [] } });

// Each test navigates to / which redirects to /onboarding on a fresh install
// OR when dev_mode_bypass is enabled without existing users.

test.fixme('(a) full happy path: welcome through admin account creation to completion', async ({
  page,
}) => {
  // Reason: CI boots gateway with onboarding pre-completed via API; /onboarding
  // is not reachable. UI onboarding flow needs a per-test fresh gateway instance,
  // not yet wired into the Playwright job. API-level onboarding is covered by
  // pkg/gateway/api_e2e_test.go TestOnboardingToFirstChat (PR-A).
  const apiKey = process.env.OPENROUTER_API_KEY_CI ?? 'sk-test-placeholder';

  await page.goto('/');
  await expect(page).toHaveURL(/onboarding/, { timeout: 10_000 });

  // Step 1 — Welcome (onboarding.tsx:442-447)
  await expect(page.getByRole('button', { name: 'Get Started' })).toBeVisible({ timeout: 10_000 });
  await page.getByRole('button', { name: 'Get Started' }).click();

  // Step 2 — Provider pick: OpenRouter button (display_name from AVAILABLE_PROVIDERS)
  await expect(page.getByRole('button', { name: /OpenRouter/i })).toBeVisible({ timeout: 10_000 });
  await page.getByRole('button', { name: /OpenRouter/i }).click();

  // API key input (onboarding.tsx:562, id="onboarding-api-key")
  await expect(page.locator('#onboarding-api-key')).toBeVisible({ timeout: 8_000 });
  await page.locator('#onboarding-api-key').fill(apiKey);

  // "Connect & Load Models" CTA (onboarding.tsx:609)
  await page.getByRole('button', { name: 'Connect & Load Models' }).click();

  // Continue is enabled only after testStatus==='success' + model selected
  const continueBtn = page.getByRole('button', { name: 'Continue' });
  await expect(continueBtn).toBeEnabled({ timeout: 30_000 });
  await continueBtn.click();

  // Step 3 — Admin credentials (onboarding.tsx:749-795)
  await expect(page.locator('#admin-username')).toBeVisible({ timeout: 10_000 });
  await page.locator('#admin-username').fill('admin');
  await page.locator('#admin-password').fill('admin123');
  await page.locator('#admin-password-confirm').fill('admin123');
  await page.getByRole('button', { name: 'Create Account' }).click();

  // Step 4 — Done (onboarding.tsx:896-910)
  await expect(page.getByRole('button', { name: 'Start Exploring' })).toBeVisible({
    timeout: 15_000,
  });
  await page.getByRole('button', { name: 'Start Exploring' }).click();

  // Post-condition: navigated to chat, nav is visible
  await expect(page.locator('nav[aria-label="Main navigation"]')).toBeVisible({ timeout: 15_000 });

  await expectA11yClean(page);
});

test.fixme('(b) invalid API key shows inline error on the provider step', async ({ page }) => {
  // Reason: same as (a) — onboarding pre-completed by CI; requires per-test fresh gateway.
  await page.goto('/');
  await expect(page).toHaveURL(/onboarding/, { timeout: 10_000 });

  // Step 1
  await page.getByRole('button', { name: 'Get Started' }).click();

  // Step 2 — pick provider and enter bad key
  await expect(page.getByRole('button', { name: /OpenRouter/i })).toBeVisible({ timeout: 8_000 });
  await page.getByRole('button', { name: /OpenRouter/i }).click();

  await expect(page.locator('#onboarding-api-key')).toBeVisible({ timeout: 8_000 });
  await page.locator('#onboarding-api-key').fill('invalid-key-xyz-123');

  await page.getByRole('button', { name: 'Connect & Load Models' }).click();

  // On error testStatus==='error' renders a div styled with color-error (onboarding.tsx:587-591).
  // The element contains an XCircle icon + error message text — match by colour style.
  const errorEl = page.locator('div[style*="color-error"], div[style*="var(--color-error)"]').first();
  await expect(errorEl).toBeVisible({ timeout: 20_000 });
});

test.fixme('(c) "Skip — I know what I\'m doing" navigates to login when admin already exists', async ({
  page,
}) => {
  // Reason: onboarding pre-completed; /onboarding unreachable in current CI flow.
  await page.goto('/');
  await expect(page).toHaveURL(/onboarding/, { timeout: 10_000 });

  // The Skip button is always rendered on Step 1 (onboarding.tsx:449-458).
  // When an admin already exists, clicking it should land on /login.
  // If no admin exists yet it still navigates to /login per the SPA routing.
  const skipBtn = page.getByRole('button', { name: "Skip — I know what I'm doing" });
  await expect(skipBtn).toBeVisible({ timeout: 8_000 });
  await skipBtn.click();

  await expect(page).toHaveURL(/login/, { timeout: 15_000 });
  await expect(page.locator('#login-password')).toBeVisible({ timeout: 10_000 });
});

test.fixme('(d) provider timeout on API-key validation triggers retry UI', async ({ page }) => {
  // Reason: onboarding pre-completed; requires per-test fresh gateway.
  await page.goto('/');
  await expect(page).toHaveURL(/onboarding/, { timeout: 10_000 });

  // Step 1
  await page.getByRole('button', { name: 'Get Started' }).click();

  // Step 2
  await expect(page.getByRole('button', { name: /OpenRouter/i })).toBeVisible({ timeout: 8_000 });
  await page.getByRole('button', { name: /OpenRouter/i }).click();

  await expect(page.locator('#onboarding-api-key')).toBeVisible({ timeout: 8_000 });

  // Intercept provider API calls to simulate a timeout
  await page.route('**/api/v1/providers/**', async (route) => {
    await new Promise<void>((resolve) => setTimeout(resolve, 35_000));
    await route.abort('timedout');
  });

  await page.locator('#onboarding-api-key').fill('sk-or-timeout-test-key');
  await page.getByRole('button', { name: 'Connect & Load Models' }).click();

  // After timeout, testStatus==='error' — the button text changes to "Retry Connection"
  // (onboarding.tsx:606-608)
  const retryBtn = page.getByRole('button', { name: 'Retry Connection' });
  await expect(retryBtn).toBeVisible({ timeout: 45_000 });
});

test.fixme('(e) admin username collision surfaces inline error on last step', async ({ page }) => {
  // Reason: onboarding pre-completed; requires per-test fresh gateway.
  await page.goto('/');
  await expect(page).toHaveURL(/onboarding/, { timeout: 10_000 });

  // Navigate to Step 3 via mocked provider flow
  await page.getByRole('button', { name: 'Get Started' }).click();

  // Mock provider connection to succeed so we can reach Step 3 quickly
  await page.route('**/api/v1/providers/**', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, models: ['openai/gpt-4o'] }),
      });
    } else {
      await route.continue();
    }
  });

  await page.getByRole('button', { name: /OpenRouter/i }).click();
  await expect(page.locator('#onboarding-api-key')).toBeVisible({ timeout: 8_000 });
  await page.locator('#onboarding-api-key').fill('sk-or-mock-key');
  await page.getByRole('button', { name: 'Connect & Load Models' }).click();

  const continueBtn = page.getByRole('button', { name: 'Continue' });
  await expect(continueBtn).toBeEnabled({ timeout: 15_000 });
  await continueBtn.click();

  // Now on Step 3 — mock setup endpoint to return 409
  await page.route('**/api/v1/auth/setup**', async (route) => {
    await route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'username already exists' }),
    });
  });

  await expect(page.locator('#admin-username')).toBeVisible({ timeout: 8_000 });
  await page.locator('#admin-username').fill('admin');
  await page.locator('#admin-password').fill('admin123');
  await page.locator('#admin-password-confirm').fill('admin123');
  await page.getByRole('button', { name: 'Create Account' }).click();

  // Error rendered in AdminCredentialsStep (onboarding.tsx:816-820)
  // style={{ color: 'var(--color-error)' }}
  const errorEl = page.locator('div[style*="color-error"]').first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });
});
