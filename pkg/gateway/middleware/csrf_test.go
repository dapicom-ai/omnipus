//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package middleware

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nextHandler is a trivial handler that records a success-marker in the
// response so tests can confirm whether the middleware let the request
// through.
var nextHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "next-ran")
})

// buildMW wraps nextHandler with CSRFMiddleware built from the given options.
func buildMW(opts ...Option) http.Handler {
	return CSRFMiddleware(opts...)(nextHandler)
}

func TestCSRF_SafeMethodsPassThrough(t *testing.T) {
	h := buildMW()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/agents", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "safe method %s must bypass CSRF gate", method)
			assert.Equal(t, "next-ran", rec.Body.String())
		})
	}
}

func TestCSRF_MissingCookie(t *testing.T) {
	h := buildMW()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader([]byte(`{}`)))
	// No cookie, no header.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "csrf cookie missing", body["error"])
	assert.NotContains(t, rec.Body.String(), "next-ran", "next handler must not run on rejected CSRF")
}

func TestCSRF_MissingHeader(t *testing.T) {
	h := buildMW()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "abc123"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "csrf header missing", body["error"])
}

func TestCSRF_Mismatch(t *testing.T) {
	var reportedRoute, reportedIP string
	var reportCalls int
	h := buildMW(
		WithReporter(func(r *http.Request, sourceIP, route string) {
			reportCalls++
			reportedIP = sourceIP
			reportedRoute = route
		}),
		WithClientIPFunc(func(r *http.Request) string { return "203.0.113.9" }),
	)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "correct-token"})
	req.Header.Set(CSRFHeaderName, "wrong-token")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "csrf token mismatch", body["error"])
	assert.Equal(t, 1, reportCalls, "reporter must fire exactly once on mismatch")
	assert.Equal(t, "203.0.113.9", reportedIP)
	assert.Equal(t, "/api/v1/config", reportedRoute)
}

func TestCSRF_MatchPassesThrough(t *testing.T) {
	h := buildMW()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "match-me"})
	req.Header.Set(CSRFHeaderName, "match-me")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "next-ran", rec.Body.String())
}

func TestCSRF_DefaultExempt(t *testing.T) {
	// Default exempt list (no options passed) includes the cookie-issuer
	// endpoints (onboarding, login, register-admin) and operational health
	// endpoints (mounted on health-server mux; exempt here for
	// defense-in-depth in case of future remount).
	h := buildMW()
	for _, path := range []string{
		"/api/v1/onboarding/complete",
		"/api/v1/auth/login",
		"/api/v1/auth/register-admin",
		"/health",
		"/ready",
		"/reload",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code, "exempt path %s must bypass CSRF gate", path)
			assert.Equal(t, "next-ran", rec.Body.String())
		})
	}
}

func TestCSRF_WithExemptPath_DropsDefaults(t *testing.T) {
	// When a caller supplies WithExemptPath without WithDefaultExempts, the
	// default set is intentionally dropped. The onboarding endpoint is no
	// longer exempt; it needs cookie+header.
	h := buildMW(WithExemptPath("/custom"))

	// Onboarding is now gated.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code,
		"WithExemptPath without WithDefaultExempts must drop the default set")

	// Custom exempt path passes through.
	req = httptest.NewRequest(http.MethodPost, "/custom", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCSRF_WithExemptPaths_Multiple(t *testing.T) {
	// WithExemptPaths(...) is a bulk variant equivalent to calling
	// WithExemptPath once per arg.
	h := buildMW(WithExemptPaths("/one", "/two", "/three"))

	for _, p := range []string{"/one", "/two", "/three"} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s must be exempt", p)
	}

	// A path not in the custom set is still gated.
	req := httptest.NewRequest(http.MethodPost, "/four", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCSRF_WithDefaultExempts_AndCustom(t *testing.T) {
	// Combining WithDefaultExempts with WithExemptPath yields defaults UNION
	// custom. Both the default /api/v1/auth/login AND the custom /extra are
	// exempt.
	h := buildMW(WithDefaultExempts(), WithExemptPath("/extra"))

	for _, p := range []string{
		"/api/v1/auth/login",
		"/api/v1/onboarding/complete",
		"/extra",
	} {
		req := httptest.NewRequest(http.MethodPost, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s must be exempt", p)
	}
}

func TestCSRF_OptionsDeepCopy_NoPostConstructionMutation(t *testing.T) {
	// Build a slice, hand it to the constructor, then mutate the slice. The
	// middleware's behavior must be unaffected — the constructor must deep-copy
	// the paths into its private map.
	paths := make([]string, 0, 3)
	paths = append(paths, "/a", "/b")
	h := buildMW(WithExemptPaths(paths...))

	// Mutate the caller's slice AFTER construction.
	paths[0] = "/hijacked"
	paths = append(paths, "/c")
	_ = paths

	// /a must still be exempt; /hijacked must still be gated.
	req := httptest.NewRequest(http.MethodPost, "/a", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "original exempt path must survive caller mutation")

	req = httptest.NewRequest(http.MethodPost, "/hijacked", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "post-construction slice mutation must not leak into middleware")
}

func TestCSRF_WithReporter_NilIsSafe(t *testing.T) {
	// Passing a nil reporter option must not crash; the middleware simply
	// rejects mismatches without invoking a callback.
	h := buildMW(WithReporter(nil))
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/abc", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "a"})
	req.Header.Set(CSRFHeaderName, "b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCSRF_NoReporterOnMismatchIsSafe(t *testing.T) {
	// With no WithReporter option at all, a mismatch still returns 403 and
	// doesn't panic.
	h := buildMW()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/abc", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "a"})
	req.Header.Set(CSRFHeaderName, "b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCSRF_WithClientIPFunc_FallbackRemoteAddr(t *testing.T) {
	// When no WithClientIPFunc is supplied, the mismatch reporter gets
	// r.RemoteAddr as the source IP.
	var seenIP string
	h := buildMW(WithReporter(func(r *http.Request, sourceIP, route string) {
		seenIP = sourceIP
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config", nil)
	req.RemoteAddr = "192.0.2.7:51234"
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "x"})
	req.Header.Set(CSRFHeaderName, "y")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "192.0.2.7:51234", seenIP, "fallback must be r.RemoteAddr")
}

func TestCSRFMiddleware_BearerBypass(t *testing.T) {
	h := buildMW()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/doctor", nil)
	req.Header.Set("Authorization", "Bearer omnipus_abcdef0123456789")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"Bearer-auth clients skip the CSRF gate (browsers cannot set Authorization cross-origin)")
	assert.Equal(t, "next-ran", rec.Body.String())
}

func TestCSRFMiddleware_BearerBypass_MissingPrefixStillGated(t *testing.T) {
	h := buildMW()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/doctor", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code,
		"only Bearer bypass; Basic auth must still be gated")
}

func TestCSRFMiddleware_PlainHTTPCookieAccepted(t *testing.T) {
	h := buildMW()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/doctor", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieNameHTTP, Value: "plainvalue"})
	req.Header.Set(CSRFHeaderName, "plainvalue")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"plain-HTTP deployments send csrf (no __Host- prefix); gate accepts it")
}

func TestCSRFMiddleware_HostCookiePreferredOverPlain(t *testing.T) {
	h := buildMW()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/doctor", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "hostvalue"})
	req.AddCookie(&http.Cookie{Name: CSRFCookieNameHTTP, Value: "plainvalue"})
	req.Header.Set(CSRFHeaderName, "hostvalue")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"when both cookies are present, __Host-csrf wins (matches header)")
}

func TestCSRF_NilOptionIgnored(t *testing.T) {
	// Passing a nil Option must be safely ignored, not panic. This lets
	// callers conditionally apply options via a ternary without branching.
	var noOpt Option
	h := buildMW(noOpt)

	// Default behavior still in effect: onboarding is exempt.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestIssueCSRFCookie_TLS_Attributes(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://example/api/v1/auth/login", nil)
	req.TLS = &tls.ConnectionState{}
	require.NoError(t, IssueCSRFCookie(rec, req))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1, "exactly one cookie must be set")
	c := cookies[0]

	assert.Equal(t, CSRFCookieName, c.Name, "TLS request must yield __Host-csrf")
	assert.Equal(t, "/", c.Path, "__Host- requires Path=/")
	assert.Empty(t, c.Domain, "__Host- requires no Domain attribute")
	assert.True(t, c.Secure, "__Host- requires Secure")
	assert.False(t, c.HttpOnly, "SPA must be able to read the cookie via document.cookie")
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite, "must be SameSite=Strict for CSRF protection")
	assert.Len(t, c.Value, 43, "token must be 32 bytes base64-url-encoded without padding")
}

func TestIssueCSRFCookie_PlainHTTP_Downgrade(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example/api/v1/auth/login", nil)
	require.NoError(t, IssueCSRFCookie(rec, req))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	c := cookies[0]

	assert.Equal(t, CSRFCookieNameHTTP, c.Name, "plain-HTTP must yield csrf (no __Host- prefix)")
	assert.False(t, c.Secure, "plain-HTTP cookie must not set Secure (browser would refuse to store)")
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.False(t, c.HttpOnly)
}

func TestIssueCSRFCookie_XForwardedProto_HTTPS(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example/api/v1/auth/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	require.NoError(t, IssueCSRFCookie(rec, req))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, CSRFCookieName, cookies[0].Name, "proxy-terminated TLS must yield __Host-csrf")
	assert.True(t, cookies[0].Secure)
}

func TestIssueCSRFCookie_TokenIsUnique(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://example/api/v1/auth/login", nil)
	req.TLS = &tls.ConnectionState{}
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		rec := httptest.NewRecorder()
		require.NoError(t, IssueCSRFCookie(rec, req))
		c := rec.Result().Cookies()[0]
		assert.False(t, seen[c.Value], "token collision on iteration %d: %q", i, c.Value)
		seen[c.Value] = true
	}
}

func TestIssueCSRFCookie_HeaderIsParseable(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://example/api/v1/auth/login", nil)
	req.TLS = &tls.ConnectionState{}
	require.NoError(t, IssueCSRFCookie(rec, req))
	setCookie := rec.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie)

	assert.True(t, strings.HasPrefix(setCookie, CSRFCookieName+"="),
		"Set-Cookie must start with __Host-csrf=")
	assert.Contains(t, setCookie, "Path=/")
	assert.Contains(t, setCookie, "Secure")
	assert.Contains(t, setCookie, "SameSite=Strict")
	assert.NotContains(t, setCookie, "HttpOnly",
		"HttpOnly must not be set — SPA needs to read the cookie")
	assert.NotContains(t, setCookie, "Domain=",
		"__Host- prefix forbids Domain attribute")
}

func TestCSRF_ErrorBody_JSONEncoded(t *testing.T) {
	// The error body is JSON-encoded via encoding/json (not fmt.Fprintf),
	// so Content-Type is application/json and the payload is a valid JSON
	// object with an "error" field.
	h := buildMW()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "csrf cookie missing", body["error"])
}
