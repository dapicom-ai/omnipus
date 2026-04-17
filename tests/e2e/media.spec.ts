import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('(a) screenshot inline render: Max screenshots example.com and renders an img with correct dimensions', async ({
  page,
}) => {
  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const maxOption = page.locator('[data-testid="agent-option-max"]');
  await expect(maxOption).toBeVisible({ timeout: 10_000 });
  await maxOption.click();

  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 10_000 });

  const countBefore = await assistantMessages(page).count();
  await input.fill('Please take a screenshot of example.com and show it to me');
  await input.press('Enter');

  await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 120_000 });

  // Wait for the media image to appear and be fully loaded
  const mediaImg = page.locator('img[src*="/api/v1/media/"]').first();
  await expect(mediaImg).toBeVisible({ timeout: 60_000 });

  const dimensions = await mediaImg.evaluate((img: HTMLImageElement) => ({
    naturalWidth: img.naturalWidth,
    naturalHeight: img.naturalHeight,
  }));

  expect(dimensions.naturalWidth).toBeGreaterThanOrEqual(600);
  expect(dimensions.naturalHeight).toBeGreaterThanOrEqual(300);

  await expectA11yClean(page);
});

test('(b) file-download fallback: large binary request triggers browser download dialog', async ({
  page,
}) => {
  await page.route('**/api/v1/media/download-test**', async (route) => {
    const buffer = Buffer.alloc(1024 * 1024 * 5);
    await route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'application/octet-stream',
        'Content-Disposition': 'attachment; filename="large-binary.bin"',
        'Content-Length': String(buffer.length),
      },
      body: buffer,
    });
  });

  const picker = agentPicker(page);
  await expect(picker).toBeVisible({ timeout: 15_000 });
  await picker.click();

  const maxOption = page.locator('[data-testid="agent-option-max"]');
  await expect(maxOption).toBeVisible({ timeout: 10_000 });
  await maxOption.click();

  const input = chatInput(page);
  await expect(input).toBeVisible({ timeout: 10_000 });

  const downloadPromise = page.waitForEvent('download', { timeout: 30_000 });

  const countBefore = await assistantMessages(page).count();
  await input.fill('Download the file at /api/v1/media/download-test and send it to me');
  await input.press('Enter');

  await expect(assistantMessages(page)).toHaveCount(countBefore + 1, { timeout: 60_000 });

  const downloadLink = page.locator('a[download], a[href*="/api/v1/media/"]').first();
  await expect(downloadLink).toBeVisible({ timeout: 30_000 });

  const [download] = await Promise.all([downloadPromise, downloadLink.click()]);
  expect(download.suggestedFilename()).toBeTruthy();
});
