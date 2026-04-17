import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/agents');
});

test('(a) roster loads with 5 core agents (Mia/Jim/Ava/Ray/Max) plus any custom', async ({
  page,
}) => {
  await expect(page).toHaveURL(/agents/, { timeout: 10_000 });

  for (const name of ['mia', 'jim', 'ava', 'ray', 'max']) {
    await expect(page.locator('body')).toContainText(new RegExp(name, 'i'), { timeout: 15_000 });
  }

  const agentCards = page.locator('[data-testid^="agent-card"]');
  await expect(agentCards.first()).toBeVisible({ timeout: 10_000 });
  expect(await agentCards.count()).toBeGreaterThanOrEqual(5);

  await expectA11yClean(page);
});

test('(b) profile accordion expands all sections', async ({ page }) => {
  const firstAgent = page.locator('[data-testid^="agent-card"]').first();
  await expect(firstAgent).toBeVisible({ timeout: 10_000 });
  await firstAgent.click();

  await expect(page).toHaveURL(/\/agents\//, { timeout: 10_000 });

  for (const section of ['identity', 'personality', 'capabilities', 'tools', 'history']) {
    const trigger = page
      .locator(`[data-testid="accordion-${section}"]`)
      .first();
    await expect(trigger).toBeVisible({ timeout: 5_000 });
    await trigger.click();
  }

  const expandedContent = page.locator('[data-state="open"]').first();
  await expect(expandedContent).toBeVisible({ timeout: 10_000 });
});

test('(c) create-agent modal via Ava produces a new roster card', async ({ page, request }) => {
  const avaCard = page.locator('[data-testid^="agent-card"]').filter({ hasText: /ava/i }).first();
  await expect(avaCard).toBeVisible({ timeout: 10_000 });
  await avaCard.click();

  await expect(page).toHaveURL(/\/agents\//, { timeout: 10_000 });

  const uniqueName = `PennyTest-${test.info().testId.slice(-8)}`;

  const chatInput = page.locator('[data-testid="chat-input"]');
  await expect(chatInput).toBeVisible({ timeout: 10_000 });
  await chatInput.fill(`create an agent named ${uniqueName} with prompt 'test'`);
  await chatInput.press('Enter');

  // Wait for assistant to confirm agent creation before navigating away
  await expect(
    page.locator('[data-testid^="assistant-msg"]').first(),
  ).toBeVisible({ timeout: 60_000 });

  await page.goto('/agents');

  await expect(page.locator('body')).toContainText(new RegExp(uniqueName, 'i'), {
    timeout: 15_000,
  });
});

test.afterAll(async ({ request }) => {
  // Clean up any PennyTest agents created by test (c) across all runs
  const resp = await request.get('/api/v1/agents');
  if (!resp.ok()) return;
  const data = await resp.json() as { id: string; name: string }[];
  for (const agent of data) {
    if (/^PennyTest/i.test(agent.name)) {
      await request.delete(`/api/v1/agents/${agent.id}`);
    }
  }
});

test('(d) locked fields render read-only on core agents', async ({ page }) => {
  const jimCard = page.locator('[data-testid^="agent-card"]').filter({ hasText: /jim/i }).first();
  await expect(jimCard).toBeVisible({ timeout: 10_000 });
  await jimCard.click();

  await expect(page).toHaveURL(/\/agents\//, { timeout: 10_000 });

  const nameInput = page.locator('[data-testid="agent-name-input"]');
  await expect(nameInput).toBeVisible({ timeout: 8_000 });
  const isReadOnly = await nameInput.evaluate(
    (el) => (el as HTMLInputElement).readOnly || (el as HTMLInputElement).disabled,
  );
  expect(isReadOnly).toBe(true);
});

test('(e) deleted agent URL returns branded 404 with "Back to Agents" link', async ({ page }) => {
  await page.goto('/agents/nonexistent-deleted-agent-xyz');

  await expect(page.locator('body')).toContainText(/not found|404|does not exist/i, {
    timeout: 10_000,
  });

  const backLink = page.getByRole('link', { name: /back to agents/i }).first();
  await expect(backLink).toBeVisible({ timeout: 10_000 });
  await backLink.click();
  await expect(page).toHaveURL(/\/agents/, { timeout: 10_000 });
});

test('(f) name collision on Create Agent surfaces server 409 error in UI', async ({ page }) => {
  const createBtn = page.getByRole('button', { name: /create agent|new agent/i }).first();
  await expect(createBtn).toBeVisible({ timeout: 10_000 });
  await createBtn.click();

  const nameInput = page.locator('input[name*="name" i], input[placeholder*="name" i]').first();
  await expect(nameInput).toBeVisible({ timeout: 10_000 });
  await nameInput.fill('Mia');

  await page.route('**/api/v1/agents**', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'agent name already exists' }),
      });
    } else {
      await route.continue();
    }
  });

  const submitBtn = page.getByRole('button', { name: /create|save|submit/i }).first();
  await submitBtn.click();

  const serverError = page.locator('[role="alert"], [data-testid="form-error"]').first();
  await expect(serverError).toBeVisible({ timeout: 10_000 });
});

test('(g) session with deleted agent shows read-only transcript and "Agent removed" banner', async ({
  page,
}) => {
  await page.route('**/api/v1/sessions/ghost-session**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'ghost-session',
        agent_id: 'deleted-agent-xyz',
        agent_removed: true,
        messages: [{ role: 'user', content: 'Hello', timestamp: new Date().toISOString() }],
      }),
    });
  });

  await page.goto('/chat/ghost-session');

  await expect(page.locator('[data-testid="agent-removed-banner"]')).toBeVisible({
    timeout: 10_000,
  });

  const inputEl = page.locator('[data-testid="chat-input"]');
  await expect(inputEl).toBeVisible({ timeout: 5_000 });
  const isDisabled = await inputEl.evaluate(
    (el) => (el as HTMLTextAreaElement).disabled,
  );
  expect(isDisabled).toBe(true);
});
