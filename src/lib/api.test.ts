// Unit tests for readCSRFCookie (F31 — defensive decodeURIComponent).
// readCSRFCookie is not exported, so we exercise it indirectly via the
// module-level behaviour: the function is called by buildHeaders() which is
// called by request(). However, the simplest and most direct approach is to
// test the exported surface that reads the cookie — buildHeaders is also
// private. We therefore test through the observable side-effect:
// readCSRFCookie is called by request() and its return value ends up in the
// X-CSRF-Token header.  But to keep the tests focused and avoid needing a
// real fetch, we re-implement readCSRFCookie inline in the test file and
// verify the same logic. The real function is also exercised via the
// integration path in the "request header" group below.
//
// Strategy:
//   Group 1 — pure unit tests of the cookie-parsing + decode logic (no fetch
//              mock needed, just document.cookie manipulation).
//   Group 2 — integration: stub fetch and verify the X-CSRF-Token header that
//              request() assembles from the cookie.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// ── Helpers ────────────────────────────────────────────────────────────────────

// setCookie replaces document.cookie with a single "a=b; c=d" string.
// jsdom exposes document.cookie as an unconfigurable getter/setter that
// simulates a real cookie jar.  We use Object.defineProperty to override it
// with a plain value for each test.
function stubCookie(value: string) {
  Object.defineProperty(document, 'cookie', {
    configurable: true,
    get: () => value,
  })
}

function restoreCookie() {
  // Remove our override so subsequent tests start clean.
  // jsdom reinstates its own descriptor when we delete the override.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (document as any).cookie
}

// Inline reimplementation of readCSRFCookie so we can test the logic directly
// without exporting the private function from api.ts.  The logic must stay
// byte-for-byte identical to the production implementation.
function readCSRFCookie(): string | null {
  if (typeof document === 'undefined') return null
  const prefix = '__Host-csrf='
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim()
    if (trimmed.startsWith(prefix)) {
      const raw = trimmed.slice(prefix.length)
      try {
        return decodeURIComponent(raw)
      } catch {
        return raw
      }
    }
  }
  return null
}

// ── Group 1: pure cookie-parsing unit tests ────────────────────────────────────

describe('readCSRFCookie', () => {
  afterEach(() => {
    restoreCookie()
  })

  it('returns null when __Host-csrf cookie is absent', () => {
    stubCookie('other=value; another=thing')
    expect(readCSRFCookie()).toBeNull()
  })

  it('returns null when document.cookie is empty', () => {
    stubCookie('')
    expect(readCSRFCookie()).toBeNull()
  })

  it('returns raw value for URL-safe base64 (no encoding needed)', () => {
    // RawURLEncoding chars only — no percent-encoding occurs.
    stubCookie('session=abc; __Host-csrf=abc123_-XYZ; path=/')
    expect(readCSRFCookie()).toBe('abc123_-XYZ')
  })

  it('decodes a percent-encoded value (e.g. standard base64 padding)', () => {
    // __Host-csrf=abc%3D%3D → decodes to abc==
    stubCookie('__Host-csrf=abc%3D%3D')
    expect(readCSRFCookie()).toBe('abc==')
  })

  it('decodes a value with plus sign encoding', () => {
    // %2B decodes to +
    stubCookie('__Host-csrf=tok%2Bvalue')
    expect(readCSRFCookie()).toBe('tok+value')
  })

  it('falls back to raw string on malformed percent-encoding', () => {
    // %ZZ is not a valid percent-encoded sequence — decodeURIComponent throws.
    stubCookie('__Host-csrf=abc%ZZ')
    expect(readCSRFCookie()).toBe('abc%ZZ')
  })

  it('handles lone percent sign at end without throwing', () => {
    stubCookie('__Host-csrf=tok%')
    expect(readCSRFCookie()).toBe('tok%')
  })

  it('picks the correct cookie when multiple are present', () => {
    stubCookie('a=1; __Host-csrf=correct_token; b=2')
    expect(readCSRFCookie()).toBe('correct_token')
  })

  it('handles leading whitespace around cookie pairs', () => {
    stubCookie('  __Host-csrf=spaced_token  ')
    // trim() is applied to each part, so leading/trailing spaces around the
    // pair are stripped before the prefix match.
    expect(readCSRFCookie()).toBe('spaced_token')
  })
})

// ── Group 2: integration — X-CSRF-Token header is set from decoded cookie ──────
//
// We import the api module so the real readCSRFCookie runs, stub fetch, and
// assert that the header value matches the decoded cookie, not the raw one.

describe('api request: X-CSRF-Token header uses decoded cookie value', () => {
  let fetchSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    fetchSpy = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    vi.stubGlobal('fetch', fetchSpy)
    // Provide a valid auth token so getAuthHeaders() doesn't skip the header.
    sessionStorage.setItem('omnipus_auth_token', 'test-bearer')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    sessionStorage.clear()
    restoreCookie()
  })

  it('sends decoded CSRF value in X-CSRF-Token when cookie is percent-encoded', async () => {
    // Set a percent-encoded cookie value.
    stubCookie('__Host-csrf=abc%3D%3D')

    // Import dynamically so the module uses our stubbed document.cookie.
    const { fetchAgents } = await import('./api')
    await fetchAgents()

    expect(fetchSpy).toHaveBeenCalledOnce()
    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit]
    const headers = new Headers(init.headers as HeadersInit)
    expect(headers.get('X-CSRF-Token')).toBe('abc==')
  })

  it('sends raw CSRF value in X-CSRF-Token when cookie is not encoded', async () => {
    stubCookie('__Host-csrf=rawtoken_123')

    const { fetchAgents } = await import('./api')
    await fetchAgents()

    const [, init] = fetchSpy.mock.calls[0] as [string, RequestInit]
    const headers = new Headers(init.headers as HeadersInit)
    expect(headers.get('X-CSRF-Token')).toBe('rawtoken_123')
  })
})
