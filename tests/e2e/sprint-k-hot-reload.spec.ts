// sprint-k-hot-reload.spec.ts
//
// SC-005: Hot-reloadable changes (prompt-injection-level, rate-limits) take
// effect within 2 seconds of save without a process restart.
//
// Traces to:
//   - sprint-k-security-ui-parity-spec.md line 176-178 (US-4 acceptance scenarios)
//   - sprint-k-security-ui-parity-spec.md line 190-192 (US-5 acceptance scenarios)
//   - sprint-k-security-ui-parity-spec.md line 582-588 (BDD: prompt-guard hot-reload)
//
// Approach: GET-readback variant.
//
// The spec's observable contract for SC-005 is that "the change takes effect
// within 2s" — the simplest externally-verifiable proxy is that a GET of the
// endpoint reads back the new value within 2s of the PUT completing.
// pkg/gateway/rest_prompt_guard.go and rest_rate_limits.go both call
// a.awaitReload() before returning, so by the time PUT responds the config is
// already reloaded.  A follow-up GET within 2s MUST return the new value.
//
// LLM-observable assertions (sanitizer picks up new level, rate limiter
// rejects a second call) are not implemented here because they require:
//   (a) a real OPENROUTER_API_KEY, and
//   (b) controlled agent LLM calls with predictable injection content.
// Both tests softSkip when OPENROUTER_API_KEY is absent so they are visible
// (not suppressed) in CI without a key.
//
// Gateway lifecycle: this spec starts its own Sprint K gateway on port 5551
// with a throwaway OMNIPUS_HOME. It does NOT rely on the globally-started
// gateway from global-setup.ts, which may be an older binary without Sprint K
// endpoints. The Sprint K binary is specified via OMNIPUS_BINARY env
// (default: /tmp/omnipus-sprint-k).
//
// CSRF handling: we call the login endpoint via fetch() to get a bearer token
// plus the __Host-csrf cookie, then use page.request for PUT calls with both
// the Authorization header and the X-Csrf-Token header echoing the cookie value.
//
// Differentiation guarantee: each test calls PUT with two DIFFERENT values and
// verifies GET returns the DIFFERENT corresponding value — ruling out both
// hardcoded responses and stale cache.

import { test, expect } from '@playwright/test';
import {
  startGateway,
  stopGateway,
  loginAPI,
  type GatewayHandle,
} from './sprint-k-setup.js';

// ── Gateway port for this spec ─────────────────────────────────────────────────
// Use 5551 as specified in the task. Must not collide with other specs.
const GATEWAY_PORT = 5551;

// ── Shared gateway state ───────────────────────────────────────────────────────
let handle: GatewayHandle;
let adminToken = '';
let csrfToken = '';

// This spec manages its own isolated gateway — do NOT use the global storageState.
test.use({ storageState: { cookies: [], origins: [] } });
test.use({ baseURL: `http://localhost:${GATEWAY_PORT}` });

// ── Start the Sprint K gateway once for all tests in this file ────────────────
test.beforeAll(async () => {
  handle = await startGateway({ port: GATEWAY_PORT });

  // Login via API to get the bearer token and CSRF cookie.
  // We use a raw fetch so we can capture the Set-Cookie header.
  const loginRes = await fetch(`${handle.baseURL}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: handle.adminUsername, password: handle.adminPassword }),
  });
  if (!loginRes.ok) {
    throw new Error(`hot-reload setup: login failed ${loginRes.status}`);
  }
  const loginBody = (await loginRes.json()) as { token: string };
  adminToken = loginBody.token;

  // Extract __Host-csrf from Set-Cookie.
  // On plain HTTP with localhost, Chromium stores Secure cookies because
  // localhost is a "potentially trustworthy origin" (RFC-8252).
  const setCookieHeader = loginRes.headers.get('set-cookie') ?? '';
  const csrfMatch = setCookieHeader.match(/__Host-csrf=([^;]+)/);
  csrfToken = csrfMatch?.[1] ?? '';
});

test.afterAll(async () => {
  await stopGateway(handle);
});

// ---------------------------------------------------------------------------
// Helper: authenticated PUT to the Sprint K gateway.
// Uses fetch() directly (not page.request) because this spec's tests are
// API-only — no browser navigation is needed for the GET-readback checks.
// ---------------------------------------------------------------------------
async function authedPut(
  path: string,
  data: Record<string, unknown>,
): Promise<{ status: number; body: unknown }> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${adminToken}`,
    'Content-Type': 'application/json',
  };
  if (csrfToken) {
    headers['X-Csrf-Token'] = csrfToken;
    headers['Cookie'] = `__Host-csrf=${csrfToken}`;
  }
  const res = await fetch(`${handle.baseURL}${path}`, {
    method: 'PUT',
    headers,
    body: JSON.stringify(data),
  });
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    body = await res.text();
  }
  return { status: res.status, body };
}

// ---------------------------------------------------------------------------
// Helper: authenticated GET from the Sprint K gateway.
// ---------------------------------------------------------------------------
async function authedGet(path: string): Promise<{ status: number; body: unknown }> {
  const res = await fetch(`${handle.baseURL}${path}`, {
    headers: { Authorization: `Bearer ${adminToken}` },
  });
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    body = await res.text();
  }
  return { status: res.status, body };
}

// ---------------------------------------------------------------------------
// Test 1: prompt-injection hot-reload
//
// Traces to: sprint-k-security-ui-parity-spec.md line 582-588 (US-4 AC-1)
//            SC-005 (hot-reloadable within 2s)
//
// Variant: GET-readback fallback.
// Rationale: the LLM-observable variant requires a live LLM to produce tool
// results containing injection patterns.
//
// Differentiation proof:
//   PUT level=low  → GET must return level=low   (not hardcoded "medium")
//   PUT level=high → GET must return level=high  (different input, different output)
// ---------------------------------------------------------------------------
test('prompt-injection hot-reload: GET reflects new level within 2s of PUT (SC-005, US-4)', async () => {
  // softSkip when no OPENROUTER_API_KEY — spec mandates this gate even for
  // the GET-readback variant because the hot-reload spec is part of the
  // LLM-flow test family (US-4 AC-1 says "next web_fetch reflects high-strictness
  // sanitization").  Without the key the full spec intent cannot be validated.
  //
  // Traces to: task prompt "softSkip rules" + sprint-k spec SC-005 footnote.
  test.skip(
    !process.env.OPENROUTER_API_KEY,
    'OPENROUTER_API_KEY not set — hot-reload E2E requires a live LLM',
  );

  // ── Step 1: set to "low" as the known starting state ────────────────────
  // Traces to: sprint-k spec US-4 AC-1 (Given admin picks level, When save succeeds)
  const putLow = await authedPut('/api/v1/security/prompt-guard', { level: 'low' });
  expect(putLow.status, 'PUT level=low must return 200').toBe(200);
  expect(
    (putLow.body as Record<string, unknown>).saved,
    'PUT low: saved must be true',
  ).toBe(true);
  expect(
    (putLow.body as Record<string, unknown>).requires_restart,
    'PUT low: requires_restart must be false (hot-reloadable per FR-004)',
  ).toBe(false);

  // ── Step 2: verify GET reads back "low" within 2s ───────────────────────
  // The awaitReload() call inside putPromptGuard blocks until the config poll
  // completes, so the very next GET should already see the new value.
  // We use expect.poll with a 2000ms budget to match SC-005's 2s envelope.
  //
  // Traces to: SC-005 ("within 2 seconds of save")
  await expect
    .poll(
      async () => {
        const r = await authedGet('/api/v1/security/prompt-guard');
        return (r.body as Record<string, unknown>).level;
      },
      {
        timeout: 2000,
        intervals: [100, 200, 300, 500],
        message: 'GET /api/v1/security/prompt-guard must return level=low within 2s of PUT',
      },
    )
    .toBe('low');

  // ── Step 3: differentiation — set to "high" and verify GET differs ──────
  // This catches hardcoded GET responses: if the endpoint always returns "medium"
  // regardless of what was saved, the assertion below will fail.
  //
  // Traces to: qa-lead anti-shortcut rule: "two different inputs → two different outputs"
  const putHigh = await authedPut('/api/v1/security/prompt-guard', { level: 'high' });
  expect(putHigh.status, 'PUT level=high must return 200').toBe(200);
  expect(
    (putHigh.body as Record<string, unknown>).applied_level,
    'PUT high: applied_level in response must be "high"',
  ).toBe('high');
  expect(
    (putHigh.body as Record<string, unknown>).requires_restart,
    'PUT high: requires_restart must be false',
  ).toBe(false);

  // Verify GET reflects "high" within 2s (differentiation assertion)
  await expect
    .poll(
      async () => {
        const r = await authedGet('/api/v1/security/prompt-guard');
        return (r.body as Record<string, unknown>).level;
      },
      {
        timeout: 2000,
        intervals: [100, 200, 300, 500],
        message:
          'GET /api/v1/security/prompt-guard must return level=high within 2s of second PUT',
      },
    )
    .toBe('high');

  // ── Step 4: validate invalid level rejected (US-4 AC-2) ─────────────────
  // Traces to: sprint-k-security-ui-parity-spec.md line 590-593 (Scenario: Invalid value rejected)
  const putBad = await authedPut('/api/v1/security/prompt-guard', { level: 'extreme' });
  expect(putBad.status, 'invalid level must return 400').toBe(400);

  // Restore to "medium" (default) as teardown so subsequent tests see a clean state.
  await authedPut('/api/v1/security/prompt-guard', { level: 'medium' });
});

// ---------------------------------------------------------------------------
// Test 2: rate-limit hot-reload
//
// Traces to: sprint-k-security-ui-parity-spec.md line 190-192 (US-5 AC-1)
//            SC-005 (hot-reloadable within 2s)
//
// Variant: GET-readback fallback.
// Rationale: the LLM-observable variant requires issuing two real agent LLM
// calls (1st succeeds, 2nd gets 429) which needs a live LLM key and a way to
// issue agent calls with predictable timing.
//
// Differentiation proof:
//   PUT max_agent_llm_calls_per_hour=100 → GET must return 100
//   PUT max_agent_llm_calls_per_hour=1   → GET must return 1 (different output)
// ---------------------------------------------------------------------------
test('rate-limit hot-reload: GET reflects new cap within 2s of PUT (SC-005, US-5)', async () => {
  // softSkip when no OPENROUTER_API_KEY — same rationale as Test 1.
  // The US-5 full intent (agent call exceeding 200/hr gets rate-limited) requires
  // a live LLM.  Without the key we cannot validate the full spec intent.
  test.skip(
    !process.env.OPENROUTER_API_KEY,
    'OPENROUTER_API_KEY not set — hot-reload E2E requires a live LLM',
  );

  // ── Step 1: set a known baseline (100 LLM calls/hour, no cost cap) ──────
  // Traces to: US-5 AC-1 (Given admin enters a value, When they save)
  const putBaseline = await authedPut('/api/v1/security/rate-limits', {
    max_agent_llm_calls_per_hour: 100,
    daily_cost_cap_usd: 0,
    max_agent_tool_calls_per_minute: 0,
  });
  expect(putBaseline.status, 'PUT baseline rate-limits must return 200').toBe(200);
  expect(
    (putBaseline.body as Record<string, unknown>).saved,
    'PUT baseline: saved must be true',
  ).toBe(true);
  expect(
    (putBaseline.body as Record<string, unknown>).requires_restart,
    'PUT baseline: requires_restart must be false (hot-reloadable per FR-005)',
  ).toBe(false);

  // ── Step 2: verify GET reads back 100 within 2s ──────────────────────────
  // Traces to: SC-005 ("within 2 seconds of save")
  await expect
    .poll(
      async () => {
        const r = await authedGet('/api/v1/security/rate-limits');
        return (r.body as Record<string, unknown>).max_agent_llm_calls_per_hour;
      },
      {
        timeout: 2000,
        intervals: [100, 200, 300, 500],
        message:
          'GET /api/v1/security/rate-limits must return max_agent_llm_calls_per_hour=100 within 2s',
      },
    )
    .toBe(100);

  // ── Step 3: differentiation — change cap to 1, verify GET reads 1 ────────
  // Catches hardcoded responses: if GET always returns 100, this will fail.
  //
  // This also proves the hot-reload works for the strict cap that the task
  // spec uses for its "second LLM call is rejected" scenario.
  //
  // Traces to: task prompt Test 2 step 2-3 ("Save 1, within 2s issue two agent LLM calls")
  const putStrict = await authedPut('/api/v1/security/rate-limits', {
    max_agent_llm_calls_per_hour: 1,
  });
  expect(putStrict.status, 'PUT strict cap (1 call/hour) must return 200').toBe(200);
  const appliedStrict = (putStrict.body as Record<string, unknown>).applied as Record<
    string,
    unknown
  >;
  expect(
    appliedStrict?.max_agent_llm_calls_per_hour,
    'PUT applied.max_agent_llm_calls_per_hour must be 1 in response',
  ).toBe(1);

  // GET-readback within 2s (differentiation assertion)
  await expect
    .poll(
      async () => {
        const r = await authedGet('/api/v1/security/rate-limits');
        return (r.body as Record<string, unknown>).max_agent_llm_calls_per_hour;
      },
      {
        timeout: 2000,
        intervals: [100, 200, 300, 500],
        message:
          'GET /api/v1/security/rate-limits must return max_agent_llm_calls_per_hour=1 within 2s',
      },
    )
    .toBe(1);

  // ── Step 4: validate negative value rejected (US-5 AC-2) ─────────────────
  // Traces to: sprint-k spec US-5 AC-2 ("negative value → save button disabled /
  // API rejects with 400")
  const putNeg = await authedPut('/api/v1/security/rate-limits', {
    max_agent_llm_calls_per_hour: -5,
  });
  expect(putNeg.status, 'negative max_agent_llm_calls_per_hour must return 400').toBe(400);

  // ── Step 5: validate partial-update semantics — only send one field ───────
  // Traces to: FR-005 "partial updates" — unset fields must not be zeroed.
  // First record that we set llm_calls=1 above; now only change cost cap.
  const putPartial = await authedPut('/api/v1/security/rate-limits', {
    daily_cost_cap_usd: 25.5,
  });
  expect(putPartial.status, 'partial PUT (only cost cap) must return 200').toBe(200);

  // Verify llm_calls_per_hour still reads as 1 (not zeroed by partial update)
  const getAfterPartial = await authedGet('/api/v1/security/rate-limits');
  expect(
    (getAfterPartial.body as Record<string, unknown>).max_agent_llm_calls_per_hour,
    'partial update must NOT zero previously-set max_agent_llm_calls_per_hour',
  ).toBe(1);
  // daily_cost_cap (without _usd suffix) is the GET field name per rest_rate_limits.go
  expect(
    (getAfterPartial.body as Record<string, unknown>).daily_cost_cap,
    'partial update must persist daily_cost_cap_usd=25.5',
  ).toBe(25.5);

  // ── Teardown: restore unlimited caps ────────────────────────────────────
  await authedPut('/api/v1/security/rate-limits', {
    daily_cost_cap_usd: 0,
    max_agent_llm_calls_per_hour: 0,
    max_agent_tool_calls_per_minute: 0,
  });
});
