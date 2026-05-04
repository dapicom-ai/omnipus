/**
 * web-serve-canonical.spec.ts — T1.2 + T1.3
 *
 * T1.2: legacy_serve_workspace_replay_renders_in_new_iframe
 *   Seeds a legacy serve_workspace transcript, opens the session, asserts:
 *   - An iframe is mounted (via the unified IframePreview + WebServeBlock path)
 *   - The iframe src contains the /preview/ (rewritten from /serve/) origin-relative path.
 *     The real gateway rewrites legacy /serve/ paths to /preview/ on the preview
 *     listener; here we verify the SPA built the URL from the result.path/url field
 *     which after the web_serve unification carries the /preview/ form.
 *   Note: The legacy serve_workspace result still uses the /serve/ path prefix in the
 *   transcript data, but the SPA wires it through IframePreview and buildIframeURL —
 *   so the test asserts the iframe is rendered at all (not blank/crashed UI), not
 *   the specific path form.
 *
 * T1.3: legacy_run_in_workspace_warmup_replay
 *   Seeds a legacy run_in_workspace transcript (dev-mode, kind inferred from
 *   command + port fields). Asserts:
 *   - A warmup placeholder (or probe iframe or ready state) is rendered — the
 *     warmup state machine started.
 *   - No JS crash / blank UI.
 *
 * Both tests drive against the real embedded SPA (Go binary + Playwright).
 * They intercept /api/v1/about to inject a known preview_port so the SPA
 * builds a consistent iframe URL regardless of the gateway's runtime config.
 *
 * Note: The preview listener is not required to serve the content; we are
 * testing SPA rendering behavior, not end-to-end preview content delivery.
 */

import { expect } from '@playwright/test'
import { test } from './fixtures/console-errors'
import { seedAndOpenSession } from './fixtures/session-setup'

const BASE_URL = process.env.OMNIPUS_URL || 'http://localhost:6060'
const PREVIEW_PORT = parseInt(process.env.OMNIPUS_PREVIEW_PORT || '6061', 10)
const SYNTHETIC_AGENT_ID = 'main'

// ── T1.2: legacy serve_workspace replay ───────────────────────────────────────

test(
  'legacy_serve_workspace_replay_renders_in_new_iframe',
  async ({ page }) => {
    // Intercept /api/v1/about to inject a known preview_port so buildIframeURL
    // produces a deterministic src. Without this, the test depends on the running
    // gateway's preview_listener_enabled state.
    await page.route(`${BASE_URL}/api/v1/about`, async (route) => {
      let base: Record<string, unknown> = {
        version: 'test',
        go_version: 'go1.21',
        os: 'linux',
        arch: 'amd64',
        uptime_seconds: 1,
      }
      try {
        const real = await route.fetch()
        if (real.ok()) base = (await real.json()) as Record<string, unknown>
      } catch {
        // Gateway not reachable — stub is sufficient for SPA render path.
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...base,
          preview_listener_enabled: true,
          preview_port: PREVIEW_PORT,
        }),
      })
    })

    const fakeToken = 'serve-workspace-legacy-token-t12'
    const fakePath = `/serve/${SYNTHETIC_AGENT_ID}/${fakeToken}/`
    const fakeUrl = `http://localhost:${PREVIEW_PORT}${fakePath}`
    const expires = new Date(Date.now() + 3600 * 1000).toISOString()

    // Seed a legacy serve_workspace transcript (no `kind` field, no `command`/`port`).
    // The SPA's inferKind() will classify this as 'static' based on the absence of
    // command+port, routing to ServeWorkspaceUI → IframePreview(kind='serve_workspace').
    await seedAndOpenSession(page, 'web-serve-canonical-t12', [
      {
        id: 'user-t12-1',
        role: 'user',
        content: 'serve my workspace',
        timestamp: new Date(Date.now() - 5000).toISOString(),
        agent_id: '',
      },
      {
        id: 'asst-t12-1',
        role: 'assistant',
        content: 'I have served your workspace.',
        timestamp: new Date(Date.now() - 4000).toISOString(),
        agent_id: SYNTHETIC_AGENT_ID,
        tool_calls: [
          {
            id: 'tc-t12-serve',
            // Legacy tool name — the SPA registers ServeWorkspaceUI under serve_workspace.
            tool: 'serve_workspace',
            status: 'success',
            duration_ms: 80,
            parameters: { path: '.', duration_seconds: 3600 },
            result: {
              path: fakePath,
              url: fakeUrl,
              expires_at: expires,
            },
          },
        ],
      },
    ])

    // Assert: the iframe is rendered — the SPA must not crash or render a blank UI.
    // title="serve_workspace preview" is set by IframePreview.tsx line 850.
    await expect(
      page.locator('iframe[title="serve_workspace preview"]'),
      'IframePreview must mount a visible iframe for the legacy serve_workspace transcript',
    ).toBeVisible({ timeout: 15_000 })

    // Assert: the iframe src is the expected preview URL (not a blank or error src).
    const src = await page
      .locator('iframe[title="serve_workspace preview"]')
      .first()
      .getAttribute('src')

    expect(
      src,
      `iframe src must be non-null and contain the preview path. Got: ${src}`,
    ).not.toBeNull()
    expect(
      src,
      `iframe src must contain the expected preview token in the URL path`,
    ).toContain(fakeToken)

    // Assert: no malformed-result error block rendered (would appear if isWebServeResult rejected).
    // The MalformedResultBlock renders: "web_serve tool returned a malformed result"
    await expect(
      page.locator('text=tool returned a malformed result'),
    ).not.toBeVisible()
  },
)

// ── T1.3: legacy run_in_workspace warmup replay ───────────────────────────────

test(
  'legacy_run_in_workspace_warmup_replay',
  async ({ page }) => {
    // Intercept /api/v1/about as above.
    await page.route(`${BASE_URL}/api/v1/about`, async (route) => {
      let base: Record<string, unknown> = {
        version: 'test',
        go_version: 'go1.21',
        os: 'linux',
        arch: 'amd64',
        uptime_seconds: 1,
      }
      try {
        const real = await route.fetch()
        if (real.ok()) base = (await real.json()) as Record<string, unknown>
      } catch {
        // Stub only.
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ...base,
          preview_listener_enabled: true,
          preview_port: PREVIEW_PORT,
          // Return a SHORT warmup timeout so the test doesn't wait 60 s for the
          // warmup to complete (the fake token will never respond successfully).
          warmup_timeout_seconds: 6,
        }),
      })
    })

    const devToken = 'run-in-workspace-legacy-token-t13'
    const devPath = `/serve/${SYNTHETIC_AGENT_ID}/${devToken}/`
    const devUrl = `http://localhost:${PREVIEW_PORT}${devPath}`
    const expires = new Date(Date.now() + 3600 * 1000).toISOString()

    // Seed a legacy run_in_workspace result — has command + port fields.
    // inferKind() classifies this as 'dev', routing to IframePreview(kind='run_in_workspace').
    // The warmup state machine will start, probing the fake URL (which will fail
    // since no real dev server is there), but the warmup placeholder must render.
    await seedAndOpenSession(page, 'web-serve-canonical-t13', [
      {
        id: 'user-t13-1',
        role: 'user',
        content: 'start the dev server',
        timestamp: new Date(Date.now() - 5000).toISOString(),
        agent_id: '',
      },
      {
        id: 'asst-t13-1',
        role: 'assistant',
        content: 'Dev server started.',
        timestamp: new Date(Date.now() - 4000).toISOString(),
        agent_id: SYNTHETIC_AGENT_ID,
        tool_calls: [
          {
            id: 'tc-t13-run',
            // Legacy tool name
            tool: 'run_in_workspace',
            status: 'success',
            duration_ms: 200,
            parameters: { command: 'vite dev', port: 5173 },
            result: {
              // No `kind` field — legacy
              path: devPath,
              url: devUrl,
              expires_at: expires,
              command: 'vite dev',
              port: 5173,
            },
          },
        ],
      },
    ])

    // Assert: the warmup state machine drives the placeholder — either:
    //   (a) "Starting dev server…" placeholder is visible, OR
    //   (b) Probing phase rendered (probe iframe hidden, placeholder still visible), OR
    //   (c) Warmup timeout error block (if 3 consecutive probes failed fast-fail).
    // Any of these is acceptable — the key regression is a blank/crashed UI.
    //
    // We give the component 15 s to render any of these states.
    const hasWarmupPlaceholder = page.locator('text=Starting dev server')
    const hasWarmupError = page.locator('text=Dev server did not respond in time')
    const hasRetryButton = page.getByRole('button', { name: 'Retry' })

    // At least one of these must appear within the timeout
    await expect(
      page.locator('text=Starting dev server, text=Dev server did not respond in time').first(),
    ).toBeVisible({ timeout: 15_000 }).catch(() =>
      // If neither matched the combined locator, try them individually
      hasWarmupPlaceholder.or(hasWarmupError).first().waitFor({ state: 'visible', timeout: 15_000 })
    )

    // Assert: no malformed-result error block
    await expect(
      page.locator('text=tool returned a malformed result'),
    ).not.toBeVisible()
  },
)
