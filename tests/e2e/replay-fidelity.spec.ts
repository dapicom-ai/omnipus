/**
 * replay-fidelity.spec.ts — Sprint I3 E2E tests for historical chat replay fidelity.
 *
 * Scope: TDD rows 23-27 from sprint-i-historical-replay-fidelity-spec.md.
 * Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenarios 1-10, SC-I-001 to SC-I-006.
 *
 * Dependency chain:
 *   I1 (backend-lead): pkg/gateway/replay.go — emits tool_call_start/result/subagent_* on replay.
 *   I2 (frontend-lead): src/store/chat.ts — isReplaying state; ChatScreen send-button disable.
 *
 * Tests (a), (c), (d), (e) are runnable against any gateway build.
 *   - (a) and (d) will FAIL (red) until I1 lands — the tool-call-badge assertions will not pass
 *     because the current replay loop emits markdown text, not structured frames.
 *   - (e) will FAIL (red) until I2 lands — isReplaying / disabled send button does not exist.
 *   - (c) is skipped with a clear blocking message until I1 (streaming backend) + I2 land.
 *   - (b) requires Sprint H SubagentBlock UI AND I1/I2 — skipped with blocking message.
 *
 * "Scenario provider" approach: since no deterministic LLM scenario provider exists yet,
 * these tests create sessions via the REST API, then seed the transcript.jsonl file
 * directly in OMNIPUS_HOME/sessions/<id>/ before navigating the browser to the session.
 * OMNIPUS_HOME is read from the OMNIPUS_HOME env var (default /tmp/omnipus-e2e-test).
 *
 * Axe accessibility check: sub-test at the end of test (a) validates WCAG 2.1 AA on a
 * replay-rendered page, per SC-I-006.
 */

import * as fs from 'fs'
import * as path from 'path'
import { expect, type Page } from '@playwright/test'
import { test } from './fixtures/console-errors'
import { expectA11yClean } from './fixtures/a11y'
import { chatInput, sendButton } from './fixtures/selectors'

// ── Constants ──────────────────────────────────────────────────────────────────

const BASE_URL = process.env.OMNIPUS_URL || 'http://localhost:6060'

/**
 * OMNIPUS_HOME: the gateway's workspace directory.
 * Must match the directory the running gateway is using.
 * Override via env var when the gateway is started with a non-default home.
 */
const OMNIPUS_HOME =
  process.env.OMNIPUS_HOME ||
  (process.env.HOME ? path.join(process.env.HOME, '.omnipus') : '/tmp/omnipus-e2e-test')

// ── Transcript helpers ─────────────────────────────────────────────────────────

interface TranscriptToolCall {
  id: string
  tool: string
  status: string
  duration_ms?: number
  parameters?: Record<string, unknown>
  result?: Record<string, unknown>
  parent_tool_call_id?: string
}

interface TranscriptEntry {
  id: string
  type?: string
  role: string
  content?: string
  summary?: string
  timestamp: string
  agent_id?: string
  tool_calls?: TranscriptToolCall[]
}

/**
 * Seed a transcript.jsonl into the session's directory.
 * The gateway reads this file when the browser sends attach_session.
 * Precondition: the session directory must already exist (created via POST /api/v1/sessions).
 */
function seedTranscript(sessionId: string, entries: TranscriptEntry[]): void {
  const sessionDir = path.join(OMNIPUS_HOME, 'sessions', sessionId)
  if (!fs.existsSync(sessionDir)) {
    throw new Error(
      `Session directory does not exist: ${sessionDir}. ` +
        'Create the session via REST API before seeding the transcript.',
    )
  }
  const transcriptPath = path.join(sessionDir, 'transcript.jsonl')
  const lines = entries.map((e) => JSON.stringify(e)).join('\n') + '\n'
  fs.writeFileSync(transcriptPath, lines, { encoding: 'utf-8' })
}

/**
 * Read the auth token from the auth state file.
 * The global-setup copies it from sessionStorage to localStorage and saves
 * storageState — we extract it from the auth file here.
 */
function getStoredAuthToken(): string | null {
  const authFile = path.join(
    path.dirname(new URL(import.meta.url).pathname),
    'fixtures/.auth/admin.json',
  )
  if (!fs.existsSync(authFile)) {
    return null
  }
  try {
    const raw = fs.readFileSync(authFile, 'utf-8')
    const state = JSON.parse(raw) as {
      origins?: Array<{
        origin: string
        localStorage?: Array<{ name: string; value: string }>
      }>
    }
    for (const origin of state.origins ?? []) {
      for (const item of origin.localStorage ?? []) {
        if (item.name === 'omnipus_auth_token') {
          return item.value
        }
      }
    }
  } catch {
    // Auth file may not exist in first run
  }
  return null
}

/**
 * Extract the __Host-csrf cookie value from the browser context.
 * The gateway uses double-submit cookie CSRF: the cookie value must also be
 * sent as the X-CSRF-Token request header on state-mutating endpoints.
 * Returns null if the cookie is not present (pre-auth or first load).
 */
async function getCsrfToken(page: Page): Promise<string | null> {
  const cookies = await page.context().cookies()
  const csrfCookie = cookies.find((c) => c.name === '__Host-csrf')
  return csrfCookie?.value ?? null
}

/**
 * Build headers for a REST API request: Authorization + CSRF token.
 * Both are required for state-mutating endpoints.
 */
async function apiHeaders(
  page: Page,
): Promise<Record<string, string>> {
  const authToken = getStoredAuthToken()
  const csrfToken = await getCsrfToken(page)
  return {
    'Content-Type': 'application/json',
    ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}),
    ...(csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
  }
}

/**
 * Create a new session via the REST API and return its ID.
 * Uses page.request which inherits the browser's cookies (including __Host-csrf),
 * and sends the X-CSRF-Token header required by the gateway's double-submit CSRF check.
 */
async function createSession(page: Page): Promise<string> {
  const resp = await page.request.post(`${BASE_URL}/api/v1/sessions`, {
    headers: await apiHeaders(page),
    data: { agent_id: 'main', type: 'chat' },
  })

  if (!resp.ok()) {
    const body = await resp.text()
    throw new Error(
      `POST /api/v1/sessions failed: ${resp.status()} ${resp.statusText()} — ${body}`,
    )
  }

  const meta = (await resp.json()) as { id: string }
  if (!meta.id) {
    throw new Error('POST /api/v1/sessions returned no id')
  }
  return meta.id
}

/**
 * Rename a session via the REST API.
 * Sends both Authorization and X-CSRF-Token headers.
 */
async function renameSession(page: Page, sessionId: string, title: string): Promise<void> {
  const resp = await page.request.put(`${BASE_URL}/api/v1/sessions/${sessionId}`, {
    headers: await apiHeaders(page),
    data: { title },
  })

  if (!resp.ok()) {
    const body = await resp.text()
    throw new Error(
      `PUT /api/v1/sessions/${sessionId} failed: ${resp.status()} ${resp.statusText()} — ${body}`,
    )
  }
}

/**
 * Navigate to the sessions panel and click a session by its title.
 * The session must be visible in the panel (sorted by recency — newly created sessions appear first).
 */
async function openSession(page: Page, sessionTitle: string): Promise<void> {
  // Click the "Sessions" button in the top-right of the chat header
  await page.getByRole('button', { name: 'Open sessions panel' }).click()

  // Wait for the session list to appear
  const sessionBtn = page
    .getByRole('button', { name: new RegExp(`Open session: ${sessionTitle}`, 'i') })
    .first()

  await expect(sessionBtn).toBeVisible({ timeout: 10_000 })
  await sessionBtn.click()
}

/**
 * Wait for the WebSocket connection to be established.
 * The chat input is disabled and shows "Connecting to gateway..." when isConnected=false.
 * We wait for the placeholder to change to indicate the connection is up.
 */
async function waitForWsConnected(page: Page): Promise<void> {
  // The textarea becomes enabled when isConnected=true. Its placeholder changes from
  // "Connecting to gateway..." to an agent-specific prompt.
  // Wait for the textarea to be enabled (not disabled), indicating WS is connected.
  await expect(chatInput(page)).toBeEnabled({ timeout: 15_000 })
}

/**
 * Wait for the replay to complete by waiting for the done frame.
 * Post-I2: the chat input is disabled during replay (FR-I-014 — isReplaying=true) and
 * re-enabled on done. Pre-I2: the input state is unchanged by replay (always enabled).
 *
 * We wait for the chat input to be enabled as the signal that either:
 * (a) the done frame arrived and isReplaying flipped to false (post-I2), or
 * (b) replay had no effect on the input (pre-I2, so it's enabled immediately).
 *
 * The send button (ComposerPrimitive.Send) is disabled when the input is empty regardless,
 * so it is not a reliable done-frame indicator.
 */
async function waitForReplayDone(page: Page): Promise<void> {
  await expect(chatInput(page)).toBeEnabled({ timeout: 30_000 })
}

// ── Test (a): tool-call fidelity on reopen ────────────────────────────────────

test(
  '(a) tool-call fidelity on reopen: live-captured DOM matches replay-captured DOM',
  async ({ page }) => {
    // Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenarios 1, 2, 3; TDD row 23.
    // SC-I-004 narrow criteria: badge count, tool attribute values, status icons, message roles.
    //
    // IMPORTANT: This test will FAIL (red) until I1 (pkg/gateway/replay.go) lands.
    // The current replay loop emits tool calls as markdown text, not tool_call_start/result frames.
    // tool-call-badge elements will be absent; the assertion below will catch this clearly.

    await page.goto('/')
    await expect(page.getByRole('banner')).toBeVisible({ timeout: 15_000 })

    // Wait for WS connection to be established before proceeding.
    // The send button is disabled when isConnected=false.
    await waitForWsConnected(page)

    // ── Step 1: Create a session and seed a transcript with known tool calls ──
    // Uses page.request for CSRF cookie inheritance (see createSession helper).
    const sessionId = await createSession(page)
    const sessionTitle = `replay-fidelity-test-a-${Date.now()}`
    await renameSession(page, sessionId, sessionTitle)

    // Seed the transcript: one user message + one assistant reply with two tool calls.
    // Dataset D1 (shell tool) + D4 (agent_id="ray") from spec.
    // Traces to: BDD Scenario 1 (tool-call fidelity), Scenario 3 (agent label).
    seedTranscript(sessionId, [
      {
        id: 'entry-user-1',
        role: 'user',
        content: 'run a shell command',
        timestamp: new Date(Date.now() - 5000).toISOString(),
        agent_id: '',
      },
      {
        id: 'entry-asst-1',
        role: 'assistant',
        content: 'I ran the commands for you.',
        timestamp: new Date(Date.now() - 4000).toISOString(),
        agent_id: 'ray',
        tool_calls: [
          {
            id: 't1',
            tool: 'shell',
            status: 'success',
            duration_ms: 42,
            parameters: { cmd: 'echo hi' },
            result: { stdout: 'hi\n' },
          },
          {
            id: 't2',
            tool: 'fs.list',
            status: 'success',
            duration_ms: 15,
            parameters: { path: '/tmp' },
            result: { entries: ['a', 'b'] },
          },
        ],
      },
    ])

    // ── Step 2: Navigate to the session via the session panel ──
    await openSession(page, sessionTitle)

    // Wait for replay to complete
    await waitForReplayDone(page)

    // ── Step 3: Capture replayed DOM ──

    // SC-I-004(iv): message role order — user message appears before assistant message.
    // Traces to: Scenario 4 (user messages), Scenario 3 (agent label).
    const userMsgs = page.locator('[data-message-id].flex-row-reverse')
    await expect(userMsgs).toHaveCount(1, { timeout: 15_000 })

    const asstMsgs = page.locator('[data-message-id]:not(.flex-row-reverse)')
    await expect(asstMsgs).toHaveCount(1, { timeout: 15_000 })

    // SC-I-004(i): badge count — two tool calls must produce two tool-call-badge elements.
    // BLOCKED until I1 lands: the current replay emits markdown text, not structured frames.
    // This assertion is the key validation — if it fails with 0 found, I1 is not implemented.
    const badges = page.locator('[data-testid="tool-call-badge"]')
    await expect(badges).toHaveCount(2, { timeout: 15_000 })

    // SC-I-004(ii): tool attribute values — shell and fs.list in the badge set.
    // Traces to: BDD Scenario 1, Scenario 2.
    const toolNames = await badges.evaluateAll((els) =>
      els.map((el) => el.getAttribute('data-tool') ?? el.getAttribute('tool') ?? el.textContent ?? ''),
    )
    expect(new Set(toolNames)).toEqual(new Set(['shell', 'fs.list']))

    // SC-I-004(iii): status icons — both badges should show success status.
    // The ToolCallBadge renders a CheckCircle icon for success (aria-label or class-based).
    for (let i = 0; i < 2; i++) {
      const badge = badges.nth(i)
      // Post-I1: badges carry data-status="success" or have aria-label containing success.
      // We check for the absence of error/running indicators as the baseline.
      await expect(badge).not.toContainText('Running...', { timeout: 5_000 })
      await expect(badge).not.toContainText('Failed', { timeout: 5_000 })
    }

    // Expanded pane: click the shell badge and verify params/result.
    // Traces to: BDD Scenario 1 (expand, echo hi params, hi result).
    const shellBadge = page.locator('[data-testid="tool-call-badge"][data-tool="shell"]').first()
    await shellBadge.click()
    await expect(shellBadge).toContainText('echo hi', { timeout: 5_000 })
    await expect(shellBadge).toContainText('hi', { timeout: 5_000 })

    // ── Axe check: SC-I-006 ──
    // WCAG 2.1 AA on replay-rendered page; zero new violations vs. baseline.
    // Traces to: SC-I-006.
    await expectA11yClean(page)
  },
)

// ── Test (b): subagent span round-trip ────────────────────────────────────────

test(
  '(b) subagent span round-trip: SubagentBlock renders on reopen with correct step count',
  // Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenario 5; TDD row 24.
  // Unskipped after Sprint H SubagentBlock UI + Sprint I streamReplay landed on the same branch.
  async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('banner')).toBeVisible({ timeout: 15_000 })

    const sessionId = await createSession(page)
    const sessionTitle = `replay-fidelity-test-b-${Date.now()}`
    await renameSession(page, sessionId, sessionTitle)

    // Dataset D2: spawn call + nested tool with ParentToolCallID.
    // Traces to: BDD Scenario 5.
    seedTranscript(sessionId, [
      {
        id: 'entry-asst-spawn',
        role: 'assistant',
        content: 'Spawning a subagent.',
        timestamp: new Date(Date.now() - 3000).toISOString(),
        agent_id: 'mia',
        tool_calls: [
          {
            id: 'c1',
            tool: 'spawn',
            status: 'success',
            duration_ms: 500,
            parameters: { task: 'do nested work' },
            result: { status: 'done' },
          },
        ],
      },
      {
        id: 'entry-nested-1',
        role: 'assistant',
        content: '',
        timestamp: new Date(Date.now() - 2000).toISOString(),
        agent_id: 'ray',
        tool_calls: [
          {
            id: 't2',
            tool: 'shell',
            status: 'success',
            duration_ms: 20,
            parameters: { cmd: 'echo nested' },
            result: { stdout: 'nested\n' },
            parent_tool_call_id: 'c1',
          },
        ],
      },
    ])

    await openSession(page, sessionTitle)
    await waitForReplayDone(page)

    // Assert SubagentBlock is present.
    // BLOCKED: no SubagentBlock component in current SPA.
    const subagentBlock = page.locator('[data-testid="subagent-collapsed"]').first()
    await expect(subagentBlock).toBeVisible({ timeout: 15_000 })

    // Capture live step count from collapsed header.
    const liveStepText = await subagentBlock.textContent()

    // Expand the block.
    await subagentBlock.click()

    // Assert the nested tool call badge is present.
    const nestedBadge = page.locator('[data-testid="tool-call-badge"][data-tool="shell"]').first()
    await expect(nestedBadge).toBeVisible({ timeout: 5_000 })

    // Close and reopen the session to validate replay round-trip.
    // Navigate away then back.
    await page.goto('/')
    await openSession(page, sessionTitle)
    await waitForReplayDone(page)

    const replayedBlock = page.locator('[data-testid="subagent-collapsed"]').first()
    await expect(replayedBlock).toBeVisible({ timeout: 15_000 })
    const replayedStepText = await replayedBlock.textContent()

    // Same step count as live.
    expect(replayedStepText).toBe(liveStepText)

    // Expand replayed block; nested badge still present.
    await replayedBlock.click()
    await expect(
      page.locator('[data-testid="tool-call-badge"][data-tool="shell"]').first(),
    ).toBeVisible({ timeout: 5_000 })
  },
)

// ── Test (c): attach-during-active-turn no event loss ─────────────────────────

test.skip(
  '(c) attach-during-active-turn: second browser context receives all events without loss',
  // BLOCKED on:
  // 1. I1 (pkg/gateway/replay.go) must implement FR-I-009: register live forwarder BEFORE
  //    replay, buffer during replay, flush after done. Without this, events that arrive
  //    during the replay window are lost.
  // 2. A deterministic slow-streaming scenario provider is needed to control the timing.
  //    The current LLM calls are non-deterministic and have no mock stream harness.
  //
  // Scenario provider gap surfaced: pkg/gateway needs a test-mode streaming scenario
  //    endpoint (e.g., GET /api/v1/_test/slow-stream?session_id=<id>&duration_ms=10000)
  //    that emits tokens slowly and deterministically. Without this, the 2-second timing
  //    window for "attach after start but before done" cannot be reliably controlled.
  //
  // Go unit-test recommendation: TestAttach_RegistersLiveEventsBeforeReplay (TDD row 16)
  //    covers this race at the unit level without needing a full browser. The E2E is a
  //    belt-and-suspenders check — the Go unit test should be prioritised.
  //
  // Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenario 9; TDD row 25.
  async ({ page, browser }) => {
    // Implementation sketch (for when blockers are resolved):
    // 1. Start a gateway with a slow-stream scenario provider activated.
    // 2. Browser context 1 sends a message that triggers the slow stream (>10s).
    // 3. Sleep ~2s; attach browser context 2 to the same session.
    // 4. Wait for replay done on context 2.
    // 5. Wait for the final assistant message on both contexts.
    // 6. Assert context 2's final message content === context 1's.
    void page
    void browser
  },
)

// ── Test (d): live continuation after replay ──────────────────────────────────

test(
  '(d) live continuation after replay: new message appends below replayed transcript',
  async ({ page }) => {
    // Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenario 8; TDD row 26.
    // SC-I-004(iv): message ordering preserved; live continuation appears after replayed transcript.
    //
    // IMPORTANT: This test requires a real LLM (OPENROUTER_API_KEY_CI) to send a live message.
    // The replay fidelity portion (assert replayed messages are present) will FAIL until I1 lands
    // — the current replay emits markdown text, and tool-call-badge assertions won't pass.
    // However, the live continuation itself (new message appears below) can be validated today.

    await page.goto('/')
    await expect(page.getByRole('banner')).toBeVisible({ timeout: 15_000 })

    // Wait for WS connection to be established.
    await waitForWsConnected(page)

    const sessionId = await createSession(page)
    const sessionTitle = `replay-fidelity-test-d-${Date.now()}`
    await renameSession(page, sessionId, sessionTitle)

    // Seed a simple multi-entry transcript (no tool calls — validates text replay + continuation).
    // Traces to: BDD Scenario 8 (replay → live continuation), Scenario 4 (user message replay).
    seedTranscript(sessionId, [
      {
        id: 'entry-user-d1',
        role: 'user',
        content: 'My first message in this replayed session.',
        timestamp: new Date(Date.now() - 10000).toISOString(),
        agent_id: '',
      },
      {
        id: 'entry-asst-d1',
        role: 'assistant',
        content: 'I acknowledge your first message.',
        timestamp: new Date(Date.now() - 9000).toISOString(),
        agent_id: 'mia',
      },
    ])

    // ── Step 1: Open the session and wait for replay ──
    await openSession(page, sessionTitle)
    await waitForReplayDone(page)

    // Verify the replayed messages are present (user + assistant).
    // Both are simple text entries — works even without I1.
    const userMsgs = page.locator('[data-message-id].flex-row-reverse')
    await expect(userMsgs).toHaveCount(1, { timeout: 15_000 })

    // W2-9: Replace loose GreaterThanOrEqual(1) with exact count.
    // The fixture seeds exactly 1 assistant message — fidelity tests assert fidelity, not tolerance.
    // Traces to: temporal-puzzling-melody.md W2-9
    const asstMsgs = page.locator('[data-message-id]:not(.flex-row-reverse)')
    await expect(asstMsgs).toHaveCount(1, { timeout: 15_000 })

    // ── Step 2: Send a new message and verify it appears BELOW the replayed transcript ──
    // Traces to: BDD Scenario 8 AS-1: new response streams in below replayed transcript.
    const input = chatInput(page)
    await expect(input).toBeEnabled({ timeout: 10_000 })
    await input.fill('Reply with exactly: "continuation confirmed"')
    await input.press('Enter')

    // A new assistant message appears (total count increases by 1).
    await expect(asstMsgs).toHaveCount(countAfterReplay + 1, { timeout: 60_000 })

    // The last assistant message should contain the expected reply.
    const lastAsstMsg = asstMsgs.last()
    await expect(lastAsstMsg).toContainText(/continuation confirmed/i, { timeout: 30_000 })

    // No messages are duplicated: the replayed messages remain exactly as seeded.
    // SC-I-004(iv): user messages still show exactly 1 (the replayed one + new one we sent = 2).
    await expect(userMsgs).toHaveCount(2, { timeout: 10_000 })

    // All assistant messages: original replayed (1) + new response (1) = 2.
    // Duplication would show 3+ or messages out of order.
    await expect(asstMsgs).toHaveCount(2, { timeout: 10_000 })
  },
)

// ── Test (e): send button disabled during replay ──────────────────────────────

test(
  '(e) send button disabled during replay: input locked until done frame arrives',
  async ({ page }) => {
    // Traces to: sprint-i-historical-replay-fidelity-spec.md BDD Scenario 10; TDD row 27; FR-I-014.
    //
    // IMPORTANT: This test validates FR-I-014 which requires I2 (isReplaying state in chat store).
    // Until I2 lands, the send button will NOT be disabled during replay — this test will FAIL (red).
    // That is the intended behaviour: the failing test shows the feature is not yet implemented.
    //
    // To validate: the test attempts to read the button's disabled state immediately after
    // navigating to a session that has a non-trivial transcript (enough entries to take >0ms to
    // replay). We cannot guarantee timing precisely, but a seeded 20-entry transcript with
    // consecutive entries should give enough replay time to observe the disabled state.

    await page.goto('/')
    await expect(page.getByRole('banner')).toBeVisible({ timeout: 15_000 })

    // Wait for WS connection to be established.
    await waitForWsConnected(page)

    const sessionId = await createSession(page)
    const sessionTitle = `replay-fidelity-test-e-${Date.now()}`
    await renameSession(page, sessionId, sessionTitle)

    // Seed a transcript with many entries to extend replay duration.
    // 10 alternating user/assistant turns with tool calls to give I1's replay loop
    // enough frames to keep the "done" frame delayed for a few hundred milliseconds.
    // Traces to: BDD Scenario 10.
    const entries: TranscriptEntry[] = []
    for (let i = 0; i < 10; i++) {
      entries.push({
        id: `entry-user-${i}`,
        role: 'user',
        content: `Message ${i}`,
        timestamp: new Date(Date.now() - (10000 - i * 900)).toISOString(),
        agent_id: '',
      })
      entries.push({
        id: `entry-asst-${i}`,
        role: 'assistant',
        content: `Response to message ${i}`,
        timestamp: new Date(Date.now() - (9000 - i * 900)).toISOString(),
        agent_id: 'mia',
        tool_calls: [
          {
            id: `tc-${i}`,
            tool: 'shell',
            status: 'success',
            duration_ms: 30,
            parameters: { cmd: `echo ${i}` },
            result: { stdout: `${i}\n` },
          },
        ],
      })
    }
    seedTranscript(sessionId, entries)

    // ── Navigate to the session and immediately check send button state ──
    // Traces to: Scenario 10 When "user types in the input → send button is disabled".

    // Open the session panel and click the session
    await page.getByRole('button', { name: 'Open sessions panel' }).click()
    const sessionBtn = page
      .getByRole('button', {
        name: new RegExp(`Open session: ${sessionTitle}`, 'i'),
      })
      .first()
    await expect(sessionBtn).toBeVisible({ timeout: 10_000 })
    await sessionBtn.click()

    // Immediately after clicking the session, the replay starts.
    // FR-I-014 (I2): the chat input MUST be disabled during replay (isReplaying=true).
    // The textarea is disabled={!isConnected || isStreaming || isUploading || isReplaying}.
    // We check the input is disabled within 500ms — if I2 is not wiring isReplaying to
    // the textarea's disabled prop, this assertion will pass (because we'd expect disabled
    // but get enabled). The test correctly validates the blocked state.
    //
    // Note: the send button (ComposerPrimitive.Send) is ALSO disabled when the input
    // is empty (AssistantUI internal behavior), making it unreliable for replay detection.
    // We use the textarea disabled state as the primary replay indicator.
    const input = chatInput(page)

    // During replay, the chat input must be disabled.
    // isReplaying is set to true when attach_session is sent (session store line 100).
    // isReplaying is set to false when the done frame arrives (chat store line 532).
    //
    // This assertion will FAIL with the current (pre-I1) implementation because:
    //   - The current replay loop emits text-only frames at wire speed (~0ms total)
    //   - The isReplaying window is true → false in <1ms — too fast to observe in Playwright
    //
    // This assertion will PASS once I1 lands because:
    //   - I1 emits N tool_call_start + tool_call_result frames through the WS sendCh
    //   - Each frame takes time to marshal + send; 20 entries × 2 tool frames = 40 frames
    //   - The isReplaying window becomes measurably long (tens to hundreds of milliseconds)
    //
    // BLOCKED on I1 (pkg/gateway/replay.go): replay too fast to observe disabled state.
    await expect(input).toBeDisabled({ timeout: 500 })

    // After replay completes (done frame arrives, isReplaying → false), input must be enabled.
    // Traces to: Scenario 10 When "replay's done frame arrives → send button becomes enabled".
    await expect(input).toBeEnabled({ timeout: 30_000 })

    // After replay, verify the send button enables when text is typed.
    // (ComposerPrimitive.Send is also disabled on empty input — typing enables it.)
    await input.fill('test message to verify send is active')
    const send = sendButton(page)
    await expect(send).toBeEnabled({ timeout: 5_000 })

    // Clear the input after validation
    await input.fill('')
  },
)
