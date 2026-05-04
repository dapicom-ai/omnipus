/**
 * skip-tracking.ts — Runtime skip governance for the Playwright E2E suite.
 *
 * T0.2: Flipped from record-and-continue to record-and-FAIL.
 *
 * When a test calls softSkip() at runtime, this module:
 *   1. Records the skip into test-results/soft-skips.json (for audit).
 *   2. Checks whether the test's title appears in SKIP_ALLOWLIST.
 *   3. If NOT allow-listed → throws an error so the test fails loudly.
 *      The run will be marked FAILED, not SKIPPED.
 *   4. If allow-listed → calls test.skip() and records the entry normally.
 *
 * === How to add a skip to the allow-list ===
 *
 * Add an entry to SKIP_ALLOWLIST below:
 *   { test: "<exact test title>", issue: "<GitHub issue URL>", until: "YYYY-MM-DD" }
 *
 * Rules:
 *   - `test` must be the exact string passed as the first argument to test().
 *   - `issue` must be a GitHub issue URL (not a tag, not a keyword).
 *   - `until` is the target date by which the skip should be resolved and removed.
 *     After this date, CI will treat the entry as expired and fail the run.
 *
 * Deletion criterion: once the underlying issue is resolved and the test passes
 * reliably, remove the entry from SKIP_ALLOWLIST and delete the test.skip() call
 * (or replace it with a real assertion).
 *
 * === What does NOT belong in the allow-list ===
 *
 * - Tests skipped because of a missing env var (use a preflight that fails fast).
 * - Tests skipped because of "LLM non-determinism" — non-determinism is a design
 *   flaw; deterministic scenario providers (T4.1) are the fix.
 * - Tests skipped because an implementation is missing — use expect(false).toBe(true)
 *   with a BLOCKED message so CI shows red, not skipped.
 *
 * === Deprecated softSkip pattern ===
 *
 * Do NOT call softSkip() for permanent/intentional skips. Those must either:
 *   (a) Be added to SKIP_ALLOWLIST with an issue + target date, or
 *   (b) Be promoted to a failing test with expect(false).toBe(true).
 */

import * as fs from 'fs';
import * as path from 'path';

// ── Allow-list ─────────────────────────────────────────────────────────────────
//
// Each entry exempts one test from the record-and-fail rule.
// Format: { test: "<exact title>", issue: "<GitHub URL>", until: "YYYY-MM-DD" }
//
// Empty by default. Add entries only for genuinely tracked issues with a deadline.

export const SKIP_ALLOWLIST: { test: string; issue: string; until: string }[] = [
  // Example (remove when resolved):
  // {
  //   test: '(a) Ray→Max→Jim chain: transcript shows all three agent labels',
  //   issue: 'https://github.com/dapicom-ai/omnipus/issues/111',
  //   until: '2026-07-01',
  // },
];

// ── Internal types ─────────────────────────────────────────────────────────────

interface SkipEntry {
  test: string;
  reason: string;
  ts: number;
  allowed: boolean;
  issue?: string;
  until?: string;
}

// ── Implementation ─────────────────────────────────────────────────────────────

/**
 * Record a skip and either fail the test (if not allow-listed) or call test.skip().
 *
 * IMPORTANT: If the test's title does NOT appear in SKIP_ALLOWLIST, this function
 * throws an error with a clear message. The test will be marked FAILED, not SKIPPED.
 * This is intentional — it prevents silent drift back into soft-skip culture.
 *
 * @param t      - the Playwright `test` object
 * @param reason - human-readable reason for the skip attempt
 */
export function softSkip(
  t: { info: () => { title: string }; skip: () => void },
  reason: string,
): void {
  const title = t.info().title;

  // Find a matching allow-list entry.
  const entry = SKIP_ALLOWLIST.find((e) => e.test === title);

  // Check expiry: if the entry has a `until` date in the past, treat as expired.
  let expired = false;
  if (entry) {
    const until = new Date(entry.until);
    if (!isNaN(until.getTime()) && until < new Date()) {
      expired = true;
    }
  }

  const allowed = Boolean(entry) && !expired;

  // Build the record.
  const record: SkipEntry = {
    test: title,
    reason,
    ts: Date.now(),
    allowed,
    ...(entry ? { issue: entry.issue, until: entry.until } : {}),
  };

  // Append to test-results/soft-skips.json (best-effort — non-fatal write failure).
  try {
    const dir = path.resolve('test-results');
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    const filePath = path.join(dir, 'soft-skips.json');
    let existing: SkipEntry[] = [];
    if (fs.existsSync(filePath)) {
      try {
        const raw = fs.readFileSync(filePath, 'utf-8').trim();
        if (raw) existing = JSON.parse(raw) as SkipEntry[];
      } catch {
        existing = [];
      }
    }
    existing.push(record);
    fs.writeFileSync(filePath, JSON.stringify(existing, null, 2), 'utf-8');
  } catch (writeErr) {
    console.warn('[skip-tracking] Failed to write skip entry:', writeErr);
  }

  if (!allowed) {
    // Not in the allow-list (or allow-list entry expired) → fail loudly.
    const expiredMsg = expired && entry
      ? ` (allow-list entry expired ${entry.until} — issue ${entry.issue})`
      : '';
    throw new Error(
      `[skip-tracking] UNAUTHORIZED SKIP${expiredMsg}\n` +
      `  test:   "${title}"\n` +
      `  reason: "${reason}"\n\n` +
      `This test called softSkip() without a valid allow-list entry. ` +
      `Either fix the underlying issue or add an entry to SKIP_ALLOWLIST in ` +
      `tests/e2e/fixtures/skip-tracking.ts with a GitHub issue URL and target date.\n` +
      `Do NOT use test.skip() or softSkip() to suppress test failures without tracking them.`,
    );
  }

  if (expired && entry) {
    // Expired allow-list entry — also fail loudly.
    throw new Error(
      `[skip-tracking] EXPIRED ALLOW-LIST ENTRY\n` +
      `  test:   "${title}"\n` +
      `  issue:  "${entry.issue}"\n` +
      `  until:  "${entry.until}" (past)\n\n` +
      `The allow-list entry for this test has passed its target date. ` +
      `Either resolve the underlying issue (${entry.issue}) or update the 'until' date ` +
      `with justification. Do not silently extend deadlines.`,
    );
  }

  // Allow-listed and not expired → skip normally.
  t.skip();
}
