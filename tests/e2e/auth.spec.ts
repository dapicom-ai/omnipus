import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { loginAs } from './fixtures/login';
import { expectA11yClean } from './fixtures/a11y';

// auth.spec.ts manages its own login flows — it tests the login paths themselves.
// Each test explicitly controls its session state; do not use global storageState here.
test.use({ storageState: { cookies: [], origins: [] } });

test('(a) valid credentials land on dashboard', async ({ page }) => {
  await loginAs(page, 'admin', 'admin123');

  // After loginAs succeeds, nav is visible (enforced by loginAs post-condition)
  await expect(page.locator('nav[aria-label="Main navigation"]')).toBeVisible({ timeout: 15_000 });
  await expect(page).not.toHaveURL(/login|onboarding/);

  await expectA11yClean(page);
});

test('(b) wrong password shows inline error and stays on /login', async ({ page }) => {
  await page.goto('/login');

  // Use the exact IDs from login.tsx:110 and :130
  await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
  await page.locator('#login-username').fill('admin');
  await page.locator('#login-password').fill('wrong-password-xyz');

  // Submit button exact text: "Sign in" (login.tsx:168)
  await page.getByRole('button', { name: 'Sign in' }).click();

  // Error display: login.tsx:150-153 renders a <div> with style={{ color: 'var(--color-error)' }}
  // when status === 'error'. No testid — match on the inline style.
  const errorEl = page.locator('div[style*="color: var(--color-error)"], div[style*="color-error"]').first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });

  // Must remain on /login
  expect(page.url()).toContain('login');
});

test.fixme(
  '(c) dev_mode_bypass = true shows red persistent banner on every route',
  async ({ page }) => {
    // SPA does not render a dev-mode banner when gateway.dev_mode_bypass is true.
    // The feature is not implemented. See tests/e2e/SPA-GAPS.md.
  },
);
