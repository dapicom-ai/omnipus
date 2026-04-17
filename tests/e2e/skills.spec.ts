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

  // Button text is "Browse Skills" (skills.tsx:143)
  const browseBtn = page.getByRole('button', { name: /Browse Skills/i });
  await expect(browseBtn).toBeVisible({ timeout: 10_000 });
  await browseBtn.click();

  // SkillBrowser renders inside a Radix Dialog ([role="dialog"])
  const modal = page.locator('[role="dialog"]').first();
  await expect(modal).toBeVisible({ timeout: 10_000 });

  await expectA11yClean(page);
});

test.fixme(
  '(b) skill install with hash mismatch shows block dialog',
  async ({ page }) => {
    // The SPA's SkillBrowser component does not render a visible file input on the skills
    // page itself. Hash-mismatch error UI is not surfaced in the current SPA implementation.
    // See tests/e2e/SPA-GAPS.md — "Skill hash-mismatch error UI not implemented".
  },
);

test('(c) MCP server add with duplicate name returns 409 and inline error', async ({ page }) => {
  // Intercept before clicking — route must be set before the request fires
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

  // Navigate to the MCP Servers tab (skills.tsx tab structure)
  const mcpTab = page.locator('button[role="tab"]', { hasText: /MCP Servers|Servers/i });
  await expect(mcpTab).toBeVisible({ timeout: 8_000 });
  await mcpTab.click();

  // "Add Server" button (skills.tsx:199)
  const addServerBtn = page.getByRole('button', { name: /Add Server/i });
  await expect(addServerBtn).toBeVisible({ timeout: 8_000 });
  await addServerBtn.click();

  // McpServerModal opens as a dialog
  const modal = page.locator('[role="dialog"]').first();
  await expect(modal).toBeVisible({ timeout: 10_000 });

  // Fill in the name field
  const nameInput = modal.locator('input').first();
  await expect(nameInput).toBeVisible({ timeout: 10_000 });
  await nameInput.fill('existing-server');

  // Submit the form
  const submitBtn = modal.getByRole('button', { name: /add|save|create/i }).first();
  await expect(submitBtn).toBeVisible({ timeout: 5_000 });
  await submitBtn.click();

  // Expect an error to appear — either in [role="alert"] or text on the page
  const errorEl = page.locator('[role="alert"]').first();
  await expect(errorEl).toBeVisible({ timeout: 10_000 });
});
