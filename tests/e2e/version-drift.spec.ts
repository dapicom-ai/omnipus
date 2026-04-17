import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test('mock stale build hash triggers "New version available" toast', async ({ page }) => {
  let requestCount = 0;
  await page.route('**/api/v1/version**', async (route) => {
    requestCount++;
    if (requestCount === 1) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          version: '0.1.0',
          commit: 'aabbccdd',
          build_hash: 'hash-original-build',
        }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          version: '0.1.1',
          commit: 'eeff1122',
          build_hash: 'hash-new-build-stale',
        }),
      });
    }
  });

  await page.goto('/');

  // Trigger the version check by focusing the window (simulates returning to the tab)
  await page.evaluate(() => {
    window.dispatchEvent(new Event('focus'));
  });

  // Wait for the version check round-trip to complete
  await page.waitForResponse('**/api/v1/version**', { timeout: 15_000 });

  const versionToast = page
    .locator('[data-testid="version-toast"], [role="status"]')
    .filter({ hasText: /new version|reload|update available/i })
    .first();
  await expect(versionToast).toBeVisible({ timeout: 15_000 });

  const reloadBtn = page.getByRole('button', { name: /reload/i }).first();
  await expect(reloadBtn).toBeVisible({ timeout: 5_000 });

  await expectA11yClean(page);
});
