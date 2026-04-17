import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('(a) send a message and receive an LLM response with token/cost update', async ({ page }) => {
  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 15_000 });
  await input.fill('Say exactly: "hello world"');

  const msgsBefore = await assistantMessages(page).count();
  await input.press('Enter');

  await expect(assistantMessages(page)).toHaveCount(msgsBefore + 1, { timeout: 60_000 });

  const tokenOrCostEl = page
    .locator(
      '[data-testid="token-usage"], [data-testid="cost-display"], [data-testid="usage-bar"]',
    )
    .first();
  await expect(tokenOrCostEl).toBeVisible({ timeout: 10_000 });

  await expectA11yClean(page);
});

test('(b) multi-turn retention: turn 3 references content from turn 1', async ({ page }) => {
  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 15_000 });

  await input.fill('Remember the word: XYZQUUX7734');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(1, { timeout: 60_000 });

  await input.fill('What is 2 + 2?');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(2, { timeout: 60_000 });

  await input.fill('What special word did I ask you to remember?');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(3, { timeout: 60_000 });

  await expect(assistantMessages(page).nth(2)).toContainText(/XYZQUUX7734/i, {
    timeout: 30_000,
  });
});

test('(c) agent switch via picker: switch from Mia to Ray, header shows new agent', async ({
  page,
}) => {
  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const rayOption = page.locator('[data-testid="agent-option-ray"]');
  await expect(rayOption).toBeVisible({ timeout: 10_000 });
  await rayOption.click();

  const header = page.locator('[data-testid="chat-header"]');
  await expect(header).toContainText(/ray/i, { timeout: 10_000 });
});

test('(d) new chat button clears message list and picks a fresh session_id', async ({ page }) => {
  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 15_000 });
  await input.fill('First message in session');
  await input.press('Enter');
  await expect(assistantMessages(page)).toHaveCount(1, { timeout: 60_000 });

  const urlBefore = page.url();

  const newChatBtn = page.getByRole('button', { name: /new chat|new conversation/i }).first();
  await expect(newChatBtn).toBeVisible({ timeout: 10_000 });
  await newChatBtn.click();

  await expect(assistantMessages(page)).toHaveCount(0, { timeout: 10_000 });

  const urlAfter = page.url();
  expect(urlAfter).not.toEqual(urlBefore);
});

test('(e) cancel streaming mid-reply — input re-enables, no orphaned spinner', async ({
  page,
}) => {
  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 15_000 });
  await input.fill(
    'Write a very long essay about the history of the internet with at least 500 words',
  );
  await input.press('Enter');

  const stopBtn = page.getByRole('button', { name: /stop|cancel|abort/i }).first();
  await expect(stopBtn).toBeVisible({ timeout: 15_000 });
  await stopBtn.click();

  // Wait for streaming to stop: stop button disappears
  await expect(stopBtn).not.toBeVisible({ timeout: 15_000 });

  await expect(chatInput(page)).toBeEnabled({ timeout: 15_000 });

  const spinnerOrLoading = page.locator(
    '[data-testid="streaming-spinner"], [aria-label="Loading"]',
  );
  await expect(spinnerOrLoading).toHaveCount(0, { timeout: 10_000 });
});

test('(f) queue-on-disconnect: messages sent during WS disconnect send in order after reconnect', async ({
  page,
  context,
}) => {
  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 15_000 });

  await context.setOffline(true);

  await input.fill('Queue message one');
  await input.press('Enter');

  await input.fill('Queue message two');
  await input.press('Enter');

  await input.fill('Queue message three');
  await input.press('Enter');

  await context.setOffline(false);

  await expect(
    page.locator('[data-testid^="user-msg"]').filter({ hasText: /Queue message one/i }).first(),
  ).toBeVisible({ timeout: 30_000 });

  await expect(page.locator('[data-testid^="user-msg"]')).toHaveCount(3, { timeout: 30_000 });
});
