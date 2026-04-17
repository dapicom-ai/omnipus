import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

const NAV_ITEMS = [
  { label: /agents/i, urlPattern: /agents/ },
  { label: /chat/i, urlPattern: /chat|^\/$/ },
  { label: /skills/i, urlPattern: /skills/ },
  { label: /command.?center/i, urlPattern: /command-center/ },
  { label: /settings/i, urlPattern: /settings/ },
];

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('(a) every nav item routes correctly', async ({ page }) => {
  const sidebar = page.locator('[data-testid="sidebar"]');
  await expect(sidebar).toBeVisible({ timeout: 10_000 });

  for (const item of NAV_ITEMS) {
    const navLink = sidebar.getByRole('link', { name: item.label }).first();
    await expect(navLink).toBeVisible({ timeout: 5_000 });
    await navLink.click();
    await expect(page).toHaveURL(item.urlPattern, { timeout: 10_000 });
  }

  await expectA11yClean(page);
});

test('(b) pinning sidebar persists across reload', async ({ page }) => {
  const pinBtn = page.locator('[data-testid="sidebar-pin"]');
  await expect(pinBtn).toBeVisible({ timeout: 8_000 });
  await pinBtn.click();

  const sidebar = page.locator('[data-testid="sidebar"]');
  await expect(sidebar).toBeVisible({ timeout: 5_000 });
  const widthBefore = await sidebar.boundingBox().then((b) => b?.width ?? 0);
  expect(widthBefore).toBeGreaterThan(50);

  await page.reload();
  await page.waitForLoadState('networkidle');

  const sidebarAfter = page.locator('[data-testid="sidebar"]');
  await expect(sidebarAfter).toBeVisible({ timeout: 10_000 });
  const widthAfter = await sidebarAfter.boundingBox().then((b) => b?.width ?? 0);
  expect(widthAfter).toBeGreaterThan(50);
});
