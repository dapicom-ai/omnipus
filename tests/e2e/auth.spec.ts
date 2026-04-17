import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { loginAs } from './fixtures/login';
import { expectA11yClean } from './fixtures/a11y';

// auth.spec.ts manages its own login flows — it tests the login paths themselves.
// Each test explicitly controls its session state; do not use global storageState here.
test.use({ storageState: { cookies: [], origins: [] } });

test('(a) valid credentials land on dashboard', async ({ page }) => {
  await loginAs(page, 'admin', 'admin123');

  await expect(page).not.toHaveURL(/login|onboarding/);
  await expect(page.locator('main, [role="main"], nav').first()).toBeVisible({ timeout: 15_000 });

  await expectA11yClean(page);
});

test('(b) wrong password shows inline error and stays on /login', async ({ page }) => {
  await page.goto('/login');

  const usernameInput = page
    .locator('input[name*="user" i], input[placeholder*="user" i], input[type="text"]')
    .first();
  await expect(usernameInput).toBeVisible({ timeout: 10_000 });
  await usernameInput.fill('admin');

  const passwordInput = page.locator('input[type="password"]').first();
  await passwordInput.fill('wrong-password-xyz');

  await page.getByRole('button', { name: /sign in|log in|login/i }).first().click();

  const errorEl = page.locator('[role="alert"], [class*="error"], [class*="invalid"]').first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });

  expect(page.url()).toContain('login');
});

test('(c) dev_mode_bypass = true shows red persistent banner on every route', async ({ page }) => {
  await page.route('**/api/v1/config**', async (route) => {
    const resp = await route.fetch();
    const body = await resp.json();
    body.gateway = body.gateway || {};
    body.gateway.dev_mode_bypass = true;
    await route.fulfill({ json: body });
  });

  await loginAs(page, 'admin', 'admin123');

  const banner = page.locator('[data-testid="dev-mode-banner"]');
  await expect(banner).toBeVisible({ timeout: 10_000 });

  // Verify it is actually styled red (color computed style)
  const color = await banner.evaluate((el) => {
    return window.getComputedStyle(el).color;
  });
  // Red channel dominant: rgb(r, g, b) where r >> g and r >> b
  const match = color.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/);
  expect(match).not.toBeNull();
  if (match) {
    const [, r, g, b] = match.map(Number);
    expect(r).toBeGreaterThan(g + 30);
    expect(r).toBeGreaterThan(b + 30);
  }

  await page.goto('/agents');
  await expect(page.locator('[data-testid="dev-mode-banner"]')).toBeVisible({ timeout: 10_000 });
});
