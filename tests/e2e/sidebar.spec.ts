import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

// Nav item definitions with exact hrefs from Sidebar.tsx NAV_ITEMS + settings Link
const NAV_ITEMS = [
  { href: '/', urlPattern: /^\/$|^.*\/$/ },
  { href: '/command-center', urlPattern: /command-center/ },
  { href: '/agents', urlPattern: /agents/ },
  { href: '/skills', urlPattern: /skills/ },
  { href: '/settings', urlPattern: /settings/ },
] as const;

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('(a) every nav item routes correctly', async ({ page }) => {
  // Open sidebar first (it is in overlay mode by default)
  const hamburger = page.locator('#sidebar-hamburger');
  await expect(hamburger).toBeVisible({ timeout: 10_000 });
  await hamburger.click();

  // Wait for the nav to appear
  const nav = page.locator('nav[aria-label="Main navigation"]');
  await expect(nav).toBeVisible({ timeout: 5_000 });

  for (const item of NAV_ITEMS) {
    // Re-open sidebar if it closed (overlay mode closes on nav click)
    if (!(await nav.isVisible())) {
      await hamburger.click();
      await expect(nav).toBeVisible({ timeout: 5_000 });
    }

    const link = nav.locator(`a[href="${item.href}"]`).first();
    await expect(link).toBeVisible({ timeout: 5_000 });
    await link.click();
    await expect(page).toHaveURL(item.urlPattern, { timeout: 10_000 });
  }

  await expectA11yClean(page);
});

test('(b) pinning sidebar persists across reload', async ({ page }) => {
  // Open the sidebar first
  const hamburger = page.locator('#sidebar-hamburger');
  await expect(hamburger).toBeVisible({ timeout: 10_000 });
  await hamburger.click();

  const nav = page.locator('nav[aria-label="Main navigation"]');
  await expect(nav).toBeVisible({ timeout: 5_000 });

  // Pin toggle button: aria-pressed attribute (Sidebar.tsx:185)
  // title is "Pin sidebar" when not pinned, "Unpin sidebar" when pinned
  const pinBtn = page.locator('button[aria-pressed][title="Pin sidebar"]');
  await expect(pinBtn).toBeVisible({ timeout: 8_000 });
  await pinBtn.click();

  // After pinning, nav should remain visible (pinned = permanent aside)
  await expect(nav).toBeVisible({ timeout: 5_000 });

  await page.reload();
  await page.waitForLoadState('networkidle');

  // After reload, if the sidebar is pinned it is rendered as an <aside> element
  const pinnedSidebar = page.locator('aside[aria-label="Main navigation"]');
  await expect(pinnedSidebar).toBeVisible({ timeout: 10_000 });
});
