import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  // HashRouter: routes live in the fragment, not the pathname.
  await page.goto('/#/command-center');
});

test('(a) all section cards load without console errors', async ({ page }) => {
  await expect(page).toHaveURL(/command-center/, { timeout: 10_000 });

  // CommandCenterScreen wraps everything in a scrollable div — wait for the route to render
  // StatusBar, RateLimitStatusCard, AttentionSection, AgentSummarySection are all rendered
  // (command-center.tsx:43-60). Assert the page has content in main.
  const main = page.locator('main');
  await expect(main).toBeVisible({ timeout: 15_000 });

  // No error alerts in the task section — tasksError banner has specific text (command-center.tsx:52-56)
  const taskErrorBanner = page.locator('text=Failed to load tasks');
  await expect(taskErrorBanner).toHaveCount(0, { timeout: 10_000 });

  await expectA11yClean(page);
});

test.skip(
  '(b) approval-queue: policy=ask tool call triggers approval modal and Approve routes it through',
  // blocked on #106: ExecApprovalBlock has no data-testid="approval-modal".
  // The block renders inside ChatScreen as a custom component without a stable selector.
  // Needs data-testid="approval-modal" + deterministic policy=ask trigger in test setup.
  // See tests/e2e/SPA-GAPS.md — "Approval modal testid missing".
  async ({ page }) => {},
);
