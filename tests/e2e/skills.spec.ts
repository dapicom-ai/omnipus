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
  // HashRouter: routes live in the fragment, not the pathname.
  await page.goto('/#/skills');
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
  // Intercept before clicking — route must be set before the request fires.
  // The API endpoint is /api/v1/mcp-servers (not /api/v1/mcp/*) — use exact path pattern.
  await page.route('**/api/v1/mcp-servers', async (route) => {
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

  // Fill required fields — pressSequentially() required; fill() doesn't fire React onChange.
  // McpServerModal requires BOTH name AND command to enable the submit button:
  // canSubmit = name.trim().length > 0 && command.trim().length > 0 (McpServerModal.tsx:55)
  const inputs = modal.locator('input');
  await expect(inputs.first()).toBeVisible({ timeout: 10_000 });
  await inputs.nth(0).pressSequentially('existing-server'); // Name field
  await inputs.nth(1).pressSequentially('npx existing-server'); // Command field

  // Submit the form — button text is "Add server" (confirmed from McpServerModal.tsx:115)
  const submitBtn = modal.getByRole('button', { name: /add server/i }).first();
  await expect(submitBtn).toBeEnabled({ timeout: 5_000 });
  await submitBtn.click();

  // McpServerModal calls addToast({ message: err.message, variant: 'error' }) on error
  // (McpServerModal.tsx:44 — no role="alert", error surfaces as a toast notification).
  // The api.ts request() helper throws new Error(`${status}: ${body}`) so the message is
  // "409: {\"error\": \"MCP server name already exists\"}" — match on the status code.
  const errorToast = page.locator('text=409').first();
  await expect(errorToast).toBeVisible({ timeout: 10_000 });
});
