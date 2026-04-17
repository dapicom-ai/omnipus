import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/command-center');
});

test('(a) all section cards load without console errors', async ({ page }) => {
  await expect(page).toHaveURL(/command-center/, { timeout: 10_000 });

  const mainContent = page
    .locator('main, [role="main"], [data-testid="command-center"]')
    .first();
  await expect(mainContent).toBeVisible({ timeout: 15_000 });

  const cards = page.locator('[data-testid^="card"], section, article');
  await expect(cards.first()).toBeVisible({ timeout: 10_000 });

  const errorAlerts = page.locator('[role="alert"][class*="error"], [data-testid*="error"]');
  expect(await errorAlerts.count()).toBe(0);

  await expectA11yClean(page);
});

test('(b) approval-queue: policy=ask tool call triggers approval modal and Approve routes it through', async ({
  page,
}) => {
  await page.goto('/');

  const chatInput = page.locator('[data-testid="chat-input"]');
  await expect(chatInput).toBeVisible({ timeout: 15_000 });
  await chatInput.fill('Use the exec tool to run: echo "approval test"');
  await chatInput.press('Enter');

  const approvalModal = page.locator('[data-testid="approval-modal"]');
  await expect(approvalModal).toBeVisible({ timeout: 30_000 });

  const approveBtn = approvalModal.getByRole('button', { name: /approve|allow/i }).first();
  await expect(approveBtn).toBeVisible({ timeout: 5_000 });
  await approveBtn.click();

  await expect(approvalModal).not.toBeVisible({ timeout: 10_000 });

  const toolResult = page.locator('[data-testid^="tool-result"]').first();
  await expect(toolResult).toBeVisible({ timeout: 30_000 });
});
