import { defineConfig } from '@playwright/test';
import path from 'path';

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: require.resolve('./tests/e2e/global-setup'),
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
    storageState: path.join(__dirname, 'tests/e2e/fixtures/.auth/admin.json'),
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
});
