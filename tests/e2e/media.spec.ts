import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test.fixme(
  '(a) screenshot inline render: Max screenshots example.com and renders an img',
  async ({ page }) => {
    // Reason: requires LLM to respond and invoke the browser screenshot tool.
    // Local gateway returns 401 from OpenRouter ("Missing Authentication header") —
    // no valid API key is configured. Requires OPENROUTER_API_KEY_CI in CI.
    // See tests/e2e/SPA-GAPS.md — "LLM chat tests require valid OpenRouter API key".

    // Select Max agent via the agent picker dropdown
    const picker = agentPicker(page);
    await expect(picker).toBeVisible({ timeout: 15_000 });
    await picker.click();

    // Find Max in the dropdown items (Radix DropdownMenuItem)
    const maxItem = page.locator('[role="menuitem"]').filter({ hasText: /max/i }).first();
    await expect(maxItem).toBeVisible({ timeout: 10_000 });
    await maxItem.click();

    const input = chatInput(page);
    await expect(input).toBeVisible({ timeout: 10_000 });

    const countBefore = await assistantMessages(page).count();
    await input.fill('Please take a screenshot of example.com and show it to me');
    await input.press('Enter');

    await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 120_000 });

    // InlineMedia in ChatScreen renders img tags for image media (ChatScreen.tsx:219)
    const mediaImg = page.locator('img[src*="/api/v1/media/"]').first();
    await expect(mediaImg).toBeVisible({ timeout: 60_000 });

    const dimensions = await mediaImg.evaluate((img: HTMLImageElement) => ({
      naturalWidth: img.naturalWidth,
      naturalHeight: img.naturalHeight,
    }));

    expect(dimensions.naturalWidth).toBeGreaterThanOrEqual(600);
    expect(dimensions.naturalHeight).toBeGreaterThanOrEqual(300);

    await expectA11yClean(page);
  },
);

test.fixme(
  '(b) file-download fallback: large binary request triggers browser download dialog',
  async ({ page }) => {
    // Driving a file download via LLM instruction is non-deterministic.
    // The download link (InlineMedia <a download> in ChatScreen.tsx:226-237) only
    // appears when the agent returns a non-image media frame.
    // This test cannot be made deterministic without a dedicated mock tool that
    // returns a file media frame. See tests/e2e/SPA-GAPS.md — "Download test requires mock media tool".
  },
);
