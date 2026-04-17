import { type Page, expect } from '@playwright/test';

export interface Credentials {
  username: string;
  password: string;
}

type LoginPhase = { kind: 'onboarding' } | { kind: 'login-form' } | { kind: 'authenticated' };

async function detectPhase(page: Page): Promise<LoginPhase> {
  const url = page.url();
  if (url.includes('onboarding')) {
    return { kind: 'onboarding' };
  }
  if (url.includes('login')) {
    return { kind: 'login-form' };
  }
  const passwordInput = page.locator('input[type="password"]');
  const pwCount = await passwordInput.count();
  if (pwCount > 0 && (await passwordInput.first().isVisible())) {
    const onboardingIndicator = page.locator('[data-testid="onboarding"], [class*="onboarding"]');
    if ((await onboardingIndicator.count()) > 0 && (await onboardingIndicator.first().isVisible())) {
      return { kind: 'onboarding' };
    }
    return { kind: 'login-form' };
  }
  return { kind: 'authenticated' };
}

async function completeOnboarding(page: Page, creds: Credentials): Promise<void> {
  await expect(page).toHaveURL(/onboarding/, { timeout: 15_000 });

  const nextBtn = page.getByRole('button', { name: /get started|next|continue/i }).first();
  await expect(nextBtn).toBeVisible({ timeout: 10_000 });
  await nextBtn.click();

  const providerBtn = page.getByRole('button', { name: /openrouter|anthropic|openai/i }).first();
  if (await providerBtn.isVisible({ timeout: 5_000 })) {
    await providerBtn.click();
  }

  const skipOrNextBtn = page.getByRole('button', { name: /next|continue|skip/i }).first();
  if (await skipOrNextBtn.isVisible({ timeout: 5_000 })) {
    await skipOrNextBtn.click();
  }

  const apiKeyInput = page
    .locator('input[type="password"], input[name*="key" i], input[placeholder*="key" i]')
    .first();
  if (await apiKeyInput.isVisible({ timeout: 5_000 })) {
    await apiKeyInput.fill('sk-test-placeholder');
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const modelSelect = page.locator('select, [role="combobox"]').first();
  if (await modelSelect.isVisible({ timeout: 5_000 })) {
    await page.getByRole('button', { name: /next|continue|skip/i }).first().click();
  }

  const usernameInput = page
    .locator('input[name*="user" i], input[placeholder*="user" i], input[id*="user" i]')
    .first();
  await expect(usernameInput).toBeVisible({ timeout: 15_000 });
  await usernameInput.fill(creds.username);

  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5_000 });
  await passwordInput.fill(creds.password);

  await page.getByRole('button', { name: /create|finish|done|complete/i }).first().click();

  await expect(page).not.toHaveURL(/onboarding/, { timeout: 30_000 });
}

async function completeLoginForm(page: Page, creds: Credentials): Promise<void> {
  const usernameInput = page
    .locator('input[name*="user" i], input[placeholder*="user" i], input[id*="user" i], input[type="text"]')
    .first();
  await expect(usernameInput).toBeVisible({ timeout: 10_000 });
  await usernameInput.fill(creds.username);

  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5_000 });
  await passwordInput.fill(creds.password);

  await page.getByRole('button', { name: /sign in|log in|login/i }).first().click();

  await expect(page).not.toHaveURL(/login/, { timeout: 15_000 });
}

export async function loginAs(page: Page, username = 'admin', password = 'admin123'): Promise<void> {
  const creds: Credentials = { username, password };

  await page.goto('/');

  const phase = await detectPhase(page);

  if (phase.kind === 'onboarding') {
    await completeOnboarding(page, creds);
  } else if (phase.kind === 'login-form') {
    await completeLoginForm(page, creds);
  }

  // Post-condition contract: must be authenticated after return
  await expect(page).not.toHaveURL(/\/(login|onboarding)/, { timeout: 15_000 });
  await expect(
    page.locator('[data-testid="user-menu"], nav').first(),
  ).toBeVisible({ timeout: 15_000 });
}
