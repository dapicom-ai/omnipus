import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

const ROUTES = [
  '/',
  '/agents',
  '/chat',
  '/skills',
  '/command-center',
  '/settings',
  '/about',
];

test('all major routes pass axe serious/critical accessibility checks', async ({ page }) => {
  for (const route of ROUTES) {
    const response = await page.goto(route, { waitUntil: 'networkidle' });
    // A 404 page that passes axe is a false green — assert the route actually loaded
    expect(response, `Navigation to ${route} returned no response`).not.toBeNull();
    expect(
      response!.ok(),
      `Route ${route} returned HTTP ${response!.status()} — fix the route before asserting axe`,
    ).toBe(true);
    expect(response!.status()).toBe(200);

    await expectA11yClean(page);
  }
});
