import { test } from './fixtures/console-errors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.skip(
  'mock stale build hash triggers "New version available" toast',
  // blocked on #110: The SPA does not poll /api/v1/version and does not show a
  // "New version available" toast on build-hash change. Needs /api/v1/version polling
  // on window focus + non-blocking toast with data-testid="version-toast" on mismatch.
  // See tests/e2e/SPA-GAPS.md — "Version-drift toast not implemented".
  async ({ page }) => {},
);
