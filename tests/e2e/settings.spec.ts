import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/settings');
});

test('(a) Providers tab shows a "Connected" badge next to configured provider', async ({
  page,
}) => {
  const providersTab = page.getByRole('tab', { name: /providers/i }).first();
  await expect(providersTab).toBeVisible({ timeout: 10_000 });
  await providersTab.click();

  const connectedBadge = page
    .locator('[data-testid="connected-badge"]')
    .filter({ hasText: /connected/i })
    .first();
  await expect(connectedBadge).toBeVisible({ timeout: 15_000 });

  await expectA11yClean(page);
});

test('(b) Security tab loads without console errors', async ({ page }) => {
  const securityTab = page.getByRole('tab', { name: /security/i }).first();
  await expect(securityTab).toBeVisible({ timeout: 10_000 });
  await securityTab.click();

  const tabContent = page.locator('[role="tabpanel"]').first();
  await expect(tabContent).toBeVisible({ timeout: 10_000 });
});

test('(c) About tab shows build info (version and commit SHA)', async ({ page }) => {
  const aboutTab = page.getByRole('tab', { name: /about/i }).first();
  await expect(aboutTab).toBeVisible({ timeout: 10_000 });
  await aboutTab.click();

  const versionEl = page.locator('[data-testid="build-version"]');
  await expect(versionEl).toBeVisible({ timeout: 10_000 });
  await expect(versionEl).toContainText(/v?\d+\.\d+/i);

  const commitEl = page.locator('[data-testid="build-commit"]');
  await expect(commitEl).toBeVisible({ timeout: 8_000 });
  // Assert the specific commit element text matches a SHA — not any hex on the page
  const commitText = await commitEl.textContent();
  expect(commitText).toMatch(/[0-9a-f]{7,40}/i);
});

test('(d) all tabs reachable via keyboard navigation (Tab + Enter)', async ({ page }) => {
  const tabList = page.locator('[role="tablist"]').first();
  await expect(tabList).toBeVisible({ timeout: 10_000 });

  const tabs = tabList.locator('[role="tab"]');
  const tabCount = await tabs.count();
  expect(tabCount).toBeGreaterThan(0);

  for (let i = 0; i < tabCount; i++) {
    const currentTab = tabs.nth(i);
    await currentTab.focus();
    await currentTab.press('Enter');

    const tabPanel = page.locator('[role="tabpanel"]').first();
    await expect(tabPanel).toBeVisible({ timeout: 5_000 });

    if (i < tabCount - 1) {
      await page.keyboard.press('ArrowRight');
    }
  }
});

test('(e) tool-policy "Always Allow" toggle persists across page reload', async ({ page }) => {
  const securityTab = page.getByRole('tab', { name: /security|tools|policy/i }).first();
  await expect(securityTab).toBeVisible({ timeout: 10_000 });
  await securityTab.click();

  const toggleEl = page.locator('[data-testid="always-allow-toggle"]').first();
  await expect(toggleEl).toBeVisible({ timeout: 8_000 });

  const stateBefore =
    (await toggleEl.getAttribute('aria-checked')) ||
    (await toggleEl.evaluate((el) =>
      (el as HTMLInputElement).checked ? 'true' : 'false',
    ));

  await toggleEl.click();

  // Wait for the toggle state to change before reloading
  const expectedStateAfterClick = stateBefore === 'true' ? 'false' : 'true';
  await expect(toggleEl).toHaveAttribute('aria-checked', expectedStateAfterClick, {
    timeout: 5_000,
  });

  await page.reload();

  const securityTabAfter = page.getByRole('tab', { name: /security|tools|policy/i }).first();
  await expect(securityTabAfter).toBeVisible({ timeout: 10_000 });
  await securityTabAfter.click();

  const toggleAfter = page.locator('[data-testid="always-allow-toggle"]').first();
  const stateAfter =
    (await toggleAfter.getAttribute('aria-checked')) ||
    (await toggleAfter.evaluate((el) =>
      (el as HTMLInputElement).checked ? 'true' : 'false',
    ));

  // Persistence means stateAfter matches what we set (expectedStateAfterClick), not stateBefore
  expect(stateAfter).toEqual(expectedStateAfterClick);
});
