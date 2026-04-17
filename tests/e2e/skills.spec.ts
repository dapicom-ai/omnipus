import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

const FAKE_SKILL_JSON = JSON.stringify({
  name: 'evil-skill',
  version: '1.0.0',
  description: 'A skill with a bad hash',
  hash: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  tools: [],
});

test.beforeEach(async ({ page }) => {
  await page.goto('/skills');
});

test('(a) Browse Skills modal opens', async ({ page }) => {
  await expect(page).toHaveURL(/skills/, { timeout: 10_000 });

  const browseBtn = page.getByRole('button', { name: /browse|browse skills|discover/i }).first();
  await expect(browseBtn).toBeVisible({ timeout: 10_000 });
  await browseBtn.click();

  const modal = page.locator('[role="dialog"]').first();
  await expect(modal).toBeVisible({ timeout: 10_000 });

  await expectA11yClean(page);
});

test('(b) skill install with hash mismatch shows block dialog', async ({ page }) => {
  await page.route('**/api/v1/skills/install**', async (route) => {
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'hash mismatch: skill integrity verification failed' }),
    });
  });

  const fileInput = page.locator('input[type="file"]').first();
  await expect(fileInput).toBeVisible({ timeout: 8_000 });
  await fileInput.setInputFiles({
    name: 'evil-skill.json',
    mimeType: 'application/json',
    buffer: Buffer.from(FAKE_SKILL_JSON),
  });

  const blockDialog = page
    .locator('[role="dialog"], [role="alert"]')
    .filter({ hasText: /hash|mismatch|integrity|blocked/i })
    .first();
  await expect(blockDialog).toBeVisible({ timeout: 15_000 });
});

test('(c) MCP server add with duplicate name returns 409 and inline error', async ({ page }) => {
  await page.route('**/api/v1/mcp/**', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'MCP server name already exists' }),
      });
    } else {
      await route.continue();
    }
  });

  const mcpTab = page.getByRole('tab', { name: /mcp|servers/i }).first();
  await expect(mcpTab).toBeVisible({ timeout: 8_000 });
  await mcpTab.click();

  const addServerBtn = page.getByRole('button', { name: /add server|add mcp|new server/i }).first();
  await expect(addServerBtn).toBeVisible({ timeout: 8_000 });
  await addServerBtn.click();

  const nameInput = page.locator('input[name*="name" i], input[placeholder*="name" i]').first();
  await expect(nameInput).toBeVisible({ timeout: 10_000 });
  await nameInput.fill('existing-server');

  const urlInput = page
    .locator('input[name*="url" i], input[placeholder*="url" i], input[type="url"]')
    .first();
  if (await urlInput.isVisible({ timeout: 3_000 })) {
    await urlInput.fill('http://localhost:9000');
  }

  const submitBtn = page.getByRole('button', { name: /add|save|submit/i }).first();
  await submitBtn.click();

  const errorEl = page.locator('[role="alert"], [data-testid="mcp-error"]').first();
  await expect(errorEl).toBeVisible({ timeout: 10_000 });
  await expect(errorEl).toContainText(/already exists|duplicate|409/i, { timeout: 5_000 });
});
