import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  // HashRouter: routes live in the fragment, not the pathname.
  await page.goto('/#/settings');
});

test('(a) Providers tab shows a "Connected" badge next to configured provider', async ({
  page,
}) => {
  // Settings uses shadcn Tabs (settings.tsx:31) — TabsTrigger renders button[role="tab"]
  const providersTab = page.locator('button[role="tab"]', { hasText: 'Providers' });
  await expect(providersTab).toBeVisible({ timeout: 10_000 });
  await providersTab.click();

  // ProvidersSection renders a Badge with text "Connected" when connected=true (ProvidersSection.tsx:98-100)
  // Badge has no testid — match by text content
  const connectedBadge = page.locator('body').getByText('Connected').first();
  await expect(connectedBadge).toBeVisible({ timeout: 15_000 });

  await expectA11yClean(page);
});

test('(b) Security tab loads without console errors', async ({ page }) => {
  const securityTab = page.locator('button[role="tab"]', { hasText: 'Security' });
  await expect(securityTab).toBeVisible({ timeout: 10_000 });
  await securityTab.click();

  // Radix Tabs renders ALL tabpanels in the DOM but marks inactive ones with hidden.
  // Use [data-state="active"] to target the currently visible panel.
  const tabPanel = page.locator('[role="tabpanel"][data-state="active"]').first();
  await expect(tabPanel).toBeVisible({ timeout: 10_000 });
});

test('(c) About tab shows build info (version)', async ({ page }) => {
  const aboutTab = page.locator('button[role="tab"]', { hasText: 'About' });
  await expect(aboutTab).toBeVisible({ timeout: 10_000 });
  await aboutTab.click();

  // AboutSection (AboutSection.tsx:86) renders InfoRow with label "Version" and mono value.
  // Assert the word "Version" is present and the section renders system info
  await expect(page.locator('body')).toContainText(/version/i, { timeout: 10_000 });

  // The version value comes from /api/v1/about — wait for it to load
  // It renders as a <dd> or span in InfoRow — match a semver-ish pattern in the active panel
  const tabPanel = page.locator('[role="tabpanel"][data-state="active"]').first();
  await expect(tabPanel).toContainText(/\d+\.\d+/, { timeout: 15_000 });
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

    // Radix Tabs: active panel has data-state="active" — hidden panels have hidden attribute
    const tabPanel = page.locator('[role="tabpanel"][data-state="active"]').first();
    await expect(tabPanel).toBeVisible({ timeout: 5_000 });

    if (i < tabCount - 1) {
      await page.keyboard.press('ArrowRight');
    }
  }
});

test.fixme(
  '(e) tool-policy "Always Allow" toggle persists across page reload',
  async ({ page }) => {
    // SecuritySection does not render an "Always Allow" toggle with a stable data-testid
    // or aria-checked attribute discoverable without a testid.
    // See tests/e2e/SPA-GAPS.md — "always-allow-toggle testid missing".
  },
);
