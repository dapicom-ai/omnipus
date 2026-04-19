import { expect, chromium } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { loginAs } from './fixtures/login';
import { expectA11yClean } from './fixtures/a11y';
import path from 'path';
import { fileURLToPath } from 'url';

const AUTH_FILE = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  'fixtures/.auth/admin.json',
);

// auth.spec.ts manages its own login flows — it tests the login paths themselves.
// Each test explicitly controls its session state; do not use global storageState here.
test.use({ storageState: { cookies: [], origins: [] } });

test('(a) valid credentials land on dashboard', async ({ page }) => {
  await loginAs(page, 'admin', 'admin123');

  // After loginAs succeeds, the banner landmark is visible (enforced by loginAs post-condition).
  // The AppShell renders a plain <header> with implicit ARIA role "banner".
  // The sidebar nav is NOT the auth indicator — it only renders while the overlay drawer is open.
  await expect(page.getByRole('banner')).toBeVisible({ timeout: 15_000 });
  await expect(page).not.toHaveURL(/\/#\/(login|onboarding)/);

  await expectA11yClean(page);
});

test('(b) wrong password shows inline error and stays on /login', async ({ page }) => {
  // HashRouter: login is at /#/login
  await page.goto('/#/login');

  // Use the exact IDs from login.tsx:110 and :130
  await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
  // pressSequentially() required — fill() does not trigger React onChange on these inputs
  await page.locator('#login-username').pressSequentially('admin');
  await page.locator('#login-password').pressSequentially('wrong-password-xyz');

  // Submit button exact text: "Sign in" (login.tsx:168)
  await page.getByRole('button', { name: 'Sign in' }).click();

  // Error display: login.tsx:150-153 renders a <div> with style={{ color: 'var(--color-error)' }}
  // when status === 'error'. No testid — match on the inline style.
  const errorEl = page.locator('div[style*="color: var(--color-error)"], div[style*="color-error"]').first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });

  // Must remain on login route (HashRouter: /#/login)
  expect(page.url()).toMatch(/login/);
});

test.skip(
  '(c) dev_mode_bypass = true shows red persistent banner on every route',
  // blocked on #104: The SPA does not render a persistent red banner when
  // gateway.dev_mode_bypass is true. AppShell only shows a connectionError banner.
  // Needs data-testid="dev-mode-banner". See tests/e2e/SPA-GAPS.md.
  async ({ page }) => {},
);

/**
 * Token rotation recovery: auth tests do fresh logins which generate a new token
 * for the `admin` user, invalidating the previous token stored in admin.json.
 * After auth tests complete, re-login and update the shared storageState so all
 * subsequent spec files (chat, command-center, settings, etc.) get a valid token.
 *
 * This is necessary because the gateway issues a new token per login and the old
 * token becomes invalid. Without this, all post-auth tests fail with 401.
 */
test.afterAll(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext({
    baseURL: process.env.OMNIPUS_URL || 'http://localhost:6060',
  });
  const page = await context.newPage();
  await page.goto('/');
  await loginAs(page, 'admin', 'admin123');
  // Mirror token to localStorage for storageState capture (same as global-setup.ts)
  await page.evaluate(() => {
    const token = sessionStorage.getItem('omnipus_auth_token');
    if (token) {
      localStorage.setItem('omnipus_auth_token', token);
    }
  });
  await context.storageState({ path: AUTH_FILE });
  await browser.close();
});
