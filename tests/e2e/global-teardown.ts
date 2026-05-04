/**
 * global-teardown.ts — T0.2: Fail the run if unauthorized skips occurred.
 *
 * Reads test-results/soft-skips.json and checks for entries where allowed=false.
 * These indicate tests that called softSkip() without a valid SKIP_ALLOWLIST entry —
 * which means softSkip() already threw an error and failed the test. The teardown
 * is an additional safety net that summarizes unauthorized skips in the run output.
 *
 * Note: because softSkip() now throws when not allow-listed, the test itself will
 * already be marked FAILED before teardown runs. The teardown provides a summary
 * for CI log readability.
 */

import * as fs from 'fs';
import * as path from 'path';

interface SkipEntry {
  test: string;
  reason: string;
  ts: number;
  allowed: boolean;
  issue?: string;
  until?: string;
}

async function globalTeardown(): Promise<void> {
  const filePath = path.resolve('test-results', 'soft-skips.json');
  if (!fs.existsSync(filePath)) {
    // No skips recorded — nothing to check.
    return;
  }

  let entries: SkipEntry[] = [];
  try {
    const raw = fs.readFileSync(filePath, 'utf-8').trim();
    if (raw) entries = JSON.parse(raw) as SkipEntry[];
  } catch {
    // If the file is corrupt, warn and continue — the individual test failures
    // are the canonical signal.
    console.warn('[skip-tracking teardown] Could not parse soft-skips.json');
    return;
  }

  const unauthorized = entries.filter((e) => !e.allowed);
  const authorized = entries.filter((e) => e.allowed);

  if (authorized.length > 0) {
    console.log(
      `[skip-tracking] ${authorized.length} authorized skip(s) this run:`,
    );
    for (const e of authorized) {
      console.log(`  - "${e.test}" (${e.issue ?? 'no issue'}, until ${e.until ?? 'no date'})`);
    }
  }

  if (unauthorized.length > 0) {
    console.error(
      `\n[skip-tracking] UNAUTHORIZED SKIPS DETECTED (${unauthorized.length}):`,
    );
    for (const e of unauthorized) {
      console.error(`  - "${e.test}": ${e.reason}`);
    }
    console.error(
      '\nThese tests called softSkip() without a valid SKIP_ALLOWLIST entry. ' +
      'The tests above should already be marked FAILED in the test report. ' +
      'To resolve: fix the underlying issue, or add an entry to SKIP_ALLOWLIST ' +
      'in tests/e2e/fixtures/skip-tracking.ts with a GitHub issue URL and target date.\n',
    );
    // Note: we do NOT throw here because softSkip() already caused the individual
    // tests to fail. Throwing in teardown would produce a confusing second failure
    // message. The FAILED tests in the report are the canonical signal.
  }
}

export default globalTeardown;
