import { chromium } from '@playwright/test';
import path from 'path';
import { fileURLToPath } from 'url';
import { loginAs } from './fixtures/login.js';

const AUTH_FILE = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  'fixtures/.auth/admin.json',
);

/**
 * Global setup: authenticate once and persist the storage state.
 *
 * Idempotent guard: if `nav[aria-label="Main navigation"]` is already visible
 * after navigation to `/`, we are already authenticated — save state and return.
 * This prevents accidental double-onboarding on test retries.
 */
async function globalSetup(): Promise<void> {
  const browser = await chromium.launch();
  const context = await browser.newContext({
    baseURL: process.env.OMNIPUS_URL || 'http://localhost:6060',
  });
  const page = await context.newPage();

  await page.goto('/');

  // Idempotency: if already authenticated, skip login flow
  const nav = page.locator('nav[aria-label="Main navigation"]');
  if (!(await nav.isVisible({ timeout: 3_000 }))) {
    await loginAs(page, 'admin', 'admin123');
  }

  await context.storageState({ path: AUTH_FILE });
  await browser.close();
}

export default globalSetup;
