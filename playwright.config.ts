import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/global-setup.ts',
  // T0.2: global teardown validates that no unauthorized skips occurred.
  // If any test called softSkip() without a valid allow-list entry, the teardown
  // reads soft-skips.json and fails the run. This prevents silent skip accumulation.
  globalTeardown: './tests/e2e/global-teardown.ts',
  timeout: 90_000,
  expect: { timeout: 15_000 },
  // retries: 3 in CI for real-LLM flakes — this is NOT a cover for real bugs;
  // flake must be investigated and fixed separately.
  retries: process.env.CI ? 3 : 0,
  // Single worker: shared gateway config/credentials cannot tolerate concurrent writes.
  // See CLAUDE.md concurrency model (single-writer goroutine + advisory flock).
  workers: 1,
  fullyParallel: false,
  reporter: [['html'], ['list']],
  use: {
    baseURL: process.env.OMNIPUS_URL || 'http://localhost:6060',
    storageState: './tests/e2e/fixtures/.auth/admin.json',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
});
