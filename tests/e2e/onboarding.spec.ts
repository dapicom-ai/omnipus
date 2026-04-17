import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Onboarding tests reset to a fresh unauthenticated tenant for every test.
test.use({ storageState: { cookies: [], origins: [] } });

test('(a) full happy path: welcome through admin account creation to completion', async ({
  page,
}) => {
  await page.goto('/');

  await expect(page).toHaveURL(/onboarding|^http:\/\/[^/]+\/$/, { timeout: 10_000 });

  const heading = page.locator('h1, h2').first();
  await expect(heading).toBeVisible({ timeout: 8_000 });

  const nextBtn = page.getByRole('button', { name: /get started|next|continue/i }).first();
  await expect(nextBtn).toBeVisible({ timeout: 10_000 });
  await nextBtn.click();

  const providerStep = page.locator('[data-testid*="provider"], h1, h2').first();
  await expect(providerStep).toBeVisible({ timeout: 10_000 });

  const providerBtn = page.getByRole('button', { name: /openrouter|anthropic|openai/i }).first();
  if (await providerBtn.isVisible({ timeout: 5_000 })) {
    await providerBtn.click();
  } else {
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const apiKeyStep = page
    .locator('input[type="password"], input[name*="key" i], input[placeholder*="key" i]')
    .first();
  if (await apiKeyStep.isVisible({ timeout: 5_000 })) {
    await apiKeyStep.fill('sk-or-test-key-for-e2e-testing');
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const modelStep = page.locator('select, [role="combobox"], [data-testid*="model"]').first();
  if (await modelStep.isVisible({ timeout: 5_000 })) {
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const userInput = page
    .locator('input[name*="user" i], input[placeholder*="user" i], input[id*="user" i], input[type="text"]')
    .first();
  await expect(userInput).toBeVisible({ timeout: 10_000 });
  await userInput.fill('admin');

  const passInput = page.locator('input[type="password"]').first();
  await passInput.fill('admin123');

  await page.getByRole('button', { name: /create|finish|done|complete/i }).first().click();

  await expect(page).not.toHaveURL(/onboarding/, { timeout: 30_000 });

  await expectA11yClean(page);
});

test('(b) invalid API key shows inline error on the provider step', async ({ page }) => {
  await page.goto('/');

  const nextBtn = page.getByRole('button', { name: /get started|next|continue/i }).first();
  await expect(nextBtn).toBeVisible({ timeout: 8_000 });
  await nextBtn.click();

  const providerBtn = page.getByRole('button', { name: /openrouter|anthropic|openai/i }).first();
  await expect(providerBtn).toBeVisible({ timeout: 8_000 });
  await providerBtn.click();

  const apiKeyInput = page
    .locator('input[type="password"], input[name*="key" i], input[placeholder*="key" i]')
    .first();
  await expect(apiKeyInput).toBeVisible({ timeout: 8_000 });
  await apiKeyInput.fill('invalid-key-xyz-123');

  const validateBtn = page
    .getByRole('button', { name: /validate|verify|test|next|continue/i })
    .first();
  await validateBtn.click();

  const errorEl = page
    .locator('[role="alert"], [data-testid="api-key-error"]')
    .first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });
});

test('(c) "skip to login" works when admin already exists', async ({ page }) => {
  await page.goto('/');

  // This link only appears when onboarding detects an existing admin
  const skipLink = page
    .getByRole('link', { name: /skip|already have|sign in|log in/i })
    .first();
  await expect(skipLink).toBeVisible({ timeout: 8_000 });
  await skipLink.click();

  await expect(page).toHaveURL(/login/, { timeout: 15_000 });
  await expect(page.locator('input[type="password"]').first()).toBeVisible({ timeout: 10_000 });
});

test('(d) provider timeout on API-key validation triggers retry UI', async ({ page }) => {
  await page.goto('/');

  const nextBtn = page.getByRole('button', { name: /get started|next|continue/i }).first();
  await expect(nextBtn).toBeVisible({ timeout: 8_000 });
  await nextBtn.click();

  const providerBtn = page.getByRole('button', { name: /openrouter|anthropic|openai/i }).first();
  await expect(providerBtn).toBeVisible({ timeout: 8_000 });
  await providerBtn.click();

  const apiKeyInput = page
    .locator('input[type="password"], input[name*="key" i], input[placeholder*="key" i]')
    .first();
  await expect(apiKeyInput).toBeVisible({ timeout: 8_000 });

  await page.route('**/api/v1/providers/**', async (route) => {
    await new Promise<void>((resolve) => setTimeout(resolve, 35_000));
    await route.abort('timedout');
  });

  await apiKeyInput.fill('sk-or-timeout-test-key');
  await page.getByRole('button', { name: /validate|verify|test|next|continue/i }).first().click();

  const retryEl = page
    .locator('[data-testid="retry-button"], [class*="retry"], [role="alert"]')
    .first();
  await expect(retryEl).toBeVisible({ timeout: 45_000 });
});

test('(e) admin username collision surfaces inline error on last step', async ({ page }) => {
  await page.goto('/');

  const nextBtn = page.getByRole('button', { name: /get started|next|continue/i }).first();
  await expect(nextBtn).toBeVisible({ timeout: 8_000 });
  await nextBtn.click();

  const providerBtn = page.getByRole('button', { name: /openrouter|anthropic|openai|skip/i }).first();
  if (await providerBtn.isVisible({ timeout: 5_000 })) {
    await providerBtn.click();
  }

  const apiKeyInput = page
    .locator('input[type="password"], input[name*="key" i]')
    .first();
  if (await apiKeyInput.isVisible({ timeout: 5_000 })) {
    await apiKeyInput.fill('sk-or-test');
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const modelStep = page.locator('select, [role="combobox"]').first();
  if (await modelStep.isVisible({ timeout: 5_000 })) {
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  await page.route('**/api/v1/auth/setup**', async (route) => {
    await route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'username already exists' }),
    });
  });

  const userInput = page
    .locator('input[name*="user" i], input[type="text"]')
    .first();
  await expect(userInput).toBeVisible({ timeout: 8_000 });
  await userInput.fill('admin');

  const passInput = page.locator('input[type="password"]').first();
  await passInput.fill('admin123');

  await page.getByRole('button', { name: /create|finish|done|complete/i }).first().click();

  const errorEl = page
    .locator('[role="alert"], [data-testid="setup-error"]')
    .first();
  await expect(errorEl).toBeVisible({ timeout: 15_000 });
});
