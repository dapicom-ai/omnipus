// Unit tests for ApiError + isApiError discriminator.
//
// Coverage groups:
//   1. Constructor + default messages
//   2. Status-class predicates (isAuthError / isNotFound / isRateLimited /
//      isServerError / isNetworkError)
//   3. isApiError discriminator
//   4. fromResponse — JSON body parse path
//   5. fromResponse — text body fallback
//   6. Network-error case (status 0)

import { describe, it, expect } from 'vitest'
import { ApiError, isApiError } from './api-error'

describe('ApiError constructor + defaults', () => {
  it('uses provided userMessage when non-empty', () => {
    const err = new ApiError(404, 'Agent not found')
    expect(err.userMessage).toBe('Agent not found')
    // Legacy compat: Error.message is "${status}: ${userMessage}" so existing
    // string-matchers (err.message.startsWith('401') etc) still work.
    expect(err.message).toBe('404: Agent not found')
    expect(err.status).toBe(404)
    expect(err.name).toBe('ApiError')
  })

  it('falls back to status-class default when userMessage is empty', () => {
    const err = new ApiError(429, '')
    expect(err.userMessage).toMatch(/Too many requests/i)
    expect(err.message).toMatch(/^429:/)
  })

  it('falls back to status-class default when userMessage is whitespace', () => {
    const err = new ApiError(401, '   ')
    expect(err.userMessage).toMatch(/session has expired/i)
  })

  it('captures optional code, body, and cause', () => {
    const cause = new Error('original')
    const err = new ApiError(409, 'last admin', {
      code: 'last_admin',
      body: '{"error":"last admin","code":"last_admin"}',
      cause,
    })
    expect(err.code).toBe('last_admin')
    expect(err.body).toBe('{"error":"last admin","code":"last_admin"}')
    expect(err.cause).toBe(cause)
  })

  it('formats network errors (status 0) without a status prefix', () => {
    // Transport failures don't have a real HTTP status; the legacy Error.message
    // shape doesn't apply, so the message is the bare userMessage.
    const err = new ApiError(0, 'Network unavailable. Check your connection.')
    expect(err.message).toBe('Network unavailable. Check your connection.')
    expect(err.status).toBe(0)
  })
})

describe('ApiError predicates', () => {
  it('isAuthError() is true for 401 and 403', () => {
    expect(new ApiError(401, 'x').isAuthError()).toBe(true)
    expect(new ApiError(403, 'x').isAuthError()).toBe(true)
    expect(new ApiError(404, 'x').isAuthError()).toBe(false)
    expect(new ApiError(500, 'x').isAuthError()).toBe(false)
  })

  it('isNotFound() is true only for 404', () => {
    expect(new ApiError(404, 'x').isNotFound()).toBe(true)
    expect(new ApiError(403, 'x').isNotFound()).toBe(false)
    expect(new ApiError(410, 'x').isNotFound()).toBe(false)
  })

  it('isRateLimited() is true only for 429', () => {
    expect(new ApiError(429, 'x').isRateLimited()).toBe(true)
    expect(new ApiError(503, 'x').isRateLimited()).toBe(false)
  })

  it('isServerError() is true for any 5xx', () => {
    expect(new ApiError(500, 'x').isServerError()).toBe(true)
    expect(new ApiError(502, 'x').isServerError()).toBe(true)
    expect(new ApiError(503, 'x').isServerError()).toBe(true)
    expect(new ApiError(599, 'x').isServerError()).toBe(true)
    expect(new ApiError(499, 'x').isServerError()).toBe(false)
    expect(new ApiError(600, 'x').isServerError()).toBe(false)
  })

  it('isNetworkError() is true only for status 0', () => {
    expect(new ApiError(0, 'x').isNetworkError()).toBe(true)
    expect(new ApiError(404, 'x').isNetworkError()).toBe(false)
  })
})

describe('isApiError discriminator', () => {
  it('returns true for ApiError instances', () => {
    expect(isApiError(new ApiError(500, 'boom'))).toBe(true)
  })

  it('returns false for plain Error', () => {
    expect(isApiError(new Error('not an api error'))).toBe(false)
  })

  it('returns false for non-Error values', () => {
    expect(isApiError(null)).toBe(false)
    expect(isApiError(undefined)).toBe(false)
    expect(isApiError('string error')).toBe(false)
    expect(isApiError({ status: 401, message: 'fake' })).toBe(false)
  })
})

describe('ApiError.fromResponse — JSON body', () => {
  it('parses {error: "..."} into userMessage', async () => {
    const res = new Response('{"error":"invalid credentials"}', {
      status: 401,
      headers: { 'Content-Type': 'application/json' },
    })
    const err = await ApiError.fromResponse(res)
    expect(err.status).toBe(401)
    expect(err.userMessage).toBe('invalid credentials')
    expect(err.body).toBe('{"error":"invalid credentials"}')
  })

  it('parses {code, message} into both fields', async () => {
    const res = new Response('{"code":"RATE_LIMITED","message":"slow down"}', {
      status: 429,
    })
    const err = await ApiError.fromResponse(res)
    expect(err.code).toBe('RATE_LIMITED')
    expect(err.userMessage).toBe('slow down')
  })

  it('prefers JSON.error over JSON.message when both present', async () => {
    const res = new Response('{"error":"first","message":"second"}', { status: 400 })
    const err = await ApiError.fromResponse(res)
    expect(err.userMessage).toBe('first')
  })
})

describe('ApiError.fromResponse — text fallback', () => {
  it('falls back to plain text when body is not JSON', async () => {
    const res = new Response('something broke', { status: 500 })
    const err = await ApiError.fromResponse(res)
    expect(err.status).toBe(500)
    expect(err.userMessage).toBe('something broke')
    expect(err.body).toBe('something broke')
  })

  it('falls back to default message when body is empty', async () => {
    const res = new Response('', { status: 503 })
    const err = await ApiError.fromResponse(res)
    expect(err.status).toBe(503)
    // Status-class default for any 5xx.
    expect(err.userMessage).toMatch(/server is unavailable/i)
  })

  it('handles 404 with empty body using its own default', async () => {
    const res = new Response('', { status: 404 })
    const err = await ApiError.fromResponse(res)
    expect(err.userMessage).toMatch(/not found/i)
  })
})

describe('ApiError extends Error properly', () => {
  it('is catchable as Error', () => {
    let caught: unknown
    try {
      throw new ApiError(500, 'oops')
    } catch (e) {
      caught = e
    }
    expect(caught instanceof Error).toBe(true)
    expect(caught instanceof ApiError).toBe(true)
  })

  it('preserves a cause through Error.cause', () => {
    const cause = new TypeError('fetch failed')
    const err = new ApiError(0, 'network down', { cause })
    expect(err.cause).toBe(cause)
  })
})
