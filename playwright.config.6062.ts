// Playwright config override for port 6062 tests (isolated from shared port 6060 gateway).
// Used by the fix-cluster test runs to avoid auth-file race with the shared 6060 test suite.
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/global-setup.ts',
  globalTeardown: './tests/e2e/global-teardown.ts',
  timeout: 90_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 3 : 2,
  workers: 1,
  fullyParallel: false,
  reporter: [['html'], ['list']],
  use: {
    baseURL: 'http://localhost:6062',
    storageState: './tests/e2e/fixtures/.auth/admin-6062.json',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
});
