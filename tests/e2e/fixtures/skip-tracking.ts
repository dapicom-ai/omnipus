/**
 * W3-12: Soft-skip helper for LLM non-determinism cases.
 *
 * Records each skip into test-results/soft-skips.json so CI can track the
 * skip rate per test and alert when a test is flaking too frequently.
 *
 * Usage:
 *   softSkip(test, 'LLM did not call spawn — non-deterministic')
 *
 * Do NOT use for permanent/intentional skips — those should remain as
 * `test.skip(title, body)` at file scope. This helper is only for
 * in-test guards that fire due to real-LLM non-determinism.
 */

import * as fs from 'fs';
import * as path from 'path';

interface SkipEntry {
  test: string;
  reason: string;
  ts: number;
}

/**
 * Record a skip into test-results/soft-skips.json and call test.skip().
 *
 * @param t - the Playwright `test` object (used for test.info() and test.skip())
 * @param reason - human-readable reason for the skip (e.g. 'LLM did not spawn')
 */
export function softSkip(t: { info: () => { title: string }; skip: () => void }, reason: string): void {
  const entry: SkipEntry = {
    test: t.info().title,
    reason,
    ts: Date.now(),
  };

  // Append to test-results/soft-skips.json. Create the file if it does not
  // exist, append a new JSON line for each skip (newline-delimited JSON).
  // Failures here are non-fatal — the skip still fires even if the write fails.
  try {
    const dir = path.resolve('test-results');
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    const filePath = path.join(dir, 'soft-skips.json');
    // Read existing entries (if any) and append.
    let existing: SkipEntry[] = [];
    if (fs.existsSync(filePath)) {
      try {
        const raw = fs.readFileSync(filePath, 'utf-8').trim();
        if (raw) {
          existing = JSON.parse(raw) as SkipEntry[];
        }
      } catch {
        // If the file is corrupt, start fresh rather than crashing the test.
        existing = [];
      }
    }
    existing.push(entry);
    fs.writeFileSync(filePath, JSON.stringify(existing, null, 2), 'utf-8');
  } catch (writeErr) {
    // Non-fatal — log and continue so the skip still fires.
    console.warn('[skip-tracking] Failed to write soft-skip entry', writeErr);
  }

  t.skip();
}
