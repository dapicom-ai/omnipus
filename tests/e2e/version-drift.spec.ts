import { test } from './fixtures/console-errors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.fixme(
  'mock stale build hash triggers "New version available" toast',
  async ({ page }) => {
    // The SPA does not poll /api/v1/version and does not show a "New version available"
    // toast on build-hash change. There is no version-drift mechanism implemented in the
    // frontend. See tests/e2e/SPA-GAPS.md — "Version-drift toast not implemented".
  },
);
