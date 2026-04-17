import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('(a) Ray→Max→Jim chain: transcript shows all three agent labels', async ({ page }) => {
  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const rayOption = page.locator('[data-testid="agent-option-ray"]');
  await expect(rayOption).toBeVisible({ timeout: 10_000 });
  await rayOption.click();

  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 10_000 });

  const countBefore = await assistantMessages(page).count();
  await input.fill('Hand off to Max right now to continue this conversation');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 60_000 });

  const countAfterFirst = await assistantMessages(page).count();
  await input.fill('Hand off to Jim to wrap up this conversation');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(countAfterFirst + 1, { timeout: 60_000 });

  const transcript = page.locator('[data-testid="messages-list"]');
  await expect(transcript).toContainText(/ray/i, { timeout: 15_000 });
  await expect(transcript).toContainText(/max/i, { timeout: 5_000 });
  await expect(transcript).toContainText(/jim/i, { timeout: 5_000 });

  await expectA11yClean(page);
});

test('(b) collapsed subagent display: spawn output renders as collapsed block, expandable', async ({
  page,
}) => {
  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const avaOption = page.locator('[data-testid="agent-option-ava"]');
  await expect(avaOption).toBeVisible({ timeout: 10_000 });
  await avaOption.click();

  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 10_000 });

  const countBefore = await assistantMessages(page).count();
  await input.fill(
    'Spawn a subagent to research the history of Python programming language and return a summary',
  );
  await input.press('Enter');

  // Wait for the assistant response (subagent completes)
  await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 120_000 });

  const collapsedBlock = page.locator('[data-testid="subagent-collapsed"]').first();
  await expect(collapsedBlock).toBeVisible({ timeout: 10_000 });
  await collapsedBlock.click();

  const expandedContent = page.locator('[data-state="open"]').first();
  await expect(expandedContent).toBeVisible({ timeout: 10_000 });
});

test('(c) 6th-handoff refusal: chain of 5 handoffs triggers policy error on 6th', async ({
  page,
  request,
}) => {
  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const rayOption = page.locator('[data-testid="agent-option-ray"]');
  await expect(rayOption).toBeVisible({ timeout: 10_000 });
  await rayOption.click();

  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 10_000 });

  const handoffAgents = ['max', 'ava', 'jim', 'mia', 'ray'];

  let msgCount = 0;
  for (const agent of handoffAgents) {
    const countBefore = await assistantMessages(page).count();
    await input.fill(`Hand off to ${agent} now`);
    await input.press('Enter');
    await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 60_000 });
    msgCount++;
  }

  // Verify 5 handoffs happened via API before testing the 6th
  const sessionUrl = page.url();
  const sessionId = sessionUrl.split('/').pop();
  if (sessionId) {
    const meta = await request.get(`/api/v1/sessions/${sessionId}/meta`);
    if (meta.ok()) {
      const metaBody = await meta.json() as { handoff_depth: number };
      expect(metaBody.handoff_depth).toBe(5);
    }
  }

  // 6th handoff — must trigger policy error in UI
  const countBefore6 = await assistantMessages(page).count();
  await input.fill('Hand off to ray now — this is the 6th handoff');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(countBefore6 + 1, { timeout: 60_000 });

  const policyError = page
    .locator('[data-testid="policy-error"], [role="alert"]')
    .filter({ hasText: /policy|limit|depth|maximum|handoff/i })
    .first();
  await expect(policyError).toBeVisible({ timeout: 30_000 });
});
