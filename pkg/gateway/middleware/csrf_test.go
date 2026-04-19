//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package middleware

import (
	"bytes"
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

// buildMW wraps nextHandler with default CSRFMiddleware.
func buildMW(cfg Config) http.Handler {
	return CSRFMiddleware(cfg)(nextHandler)
}

func TestCSRF_SafeMethodsPassThrough(t *testing.T) {
	h := buildMW(Config{})
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
	h := buildMW(Config{})
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
	h := buildMW(Config{})
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
	h := buildMW(Config{
		Reporter: func(r *http.Request, sourceIP, route string) {
			reportCalls++
			reportedIP = sourceIP
			reportedRoute = route
		},
		ClientIP: func(r *http.Request) string { return "203.0.113.9" },
	})
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
	h := buildMW(Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "match-me"})
	req.Header.Set(CSRFHeaderName, "match-me")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "next-ran", rec.Body.String())
}

func TestCSRF_DefaultExempt(t *testing.T) {
	// Default exempt list includes onboarding-complete (for the bootstrap
	// case described in the package doc) and the operational health
	// endpoints (which are not browser-driven).
	h := buildMW(Config{})
	for _, path := range []string{
		"/api/v1/onboarding/complete",
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

func TestCSRF_CustomExemptReplacesDefault(t *testing.T) {
	// When a caller supplies a non-nil ExemptPaths, it REPLACES the default.
	// So the onboarding endpoint is no longer exempt; it needs cookie+header.
	h := buildMW(Config{ExemptPaths: map[string]struct{}{"/custom": {}}})

	// Onboarding is gated now.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "custom exempt list must replace default; onboarding now gated")

	// Custom exempt path passes through.
	req = httptest.NewRequest(http.MethodPost, "/custom", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCSRF_NoReporterOnMismatchIsSafe(t *testing.T) {
	// Reporter is optional. A nil Reporter must not crash the middleware
	// on a mismatch.
	h := buildMW(Config{Reporter: nil})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/abc", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "a"})
	req.Header.Set(CSRFHeaderName, "b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestIssueCSRFCookie_Attributes(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, IssueCSRFCookie(rec))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1, "exactly one cookie must be set")
	c := cookies[0]

	assert.Equal(t, CSRFCookieName, c.Name, "cookie must be __Host-csrf")
	assert.Equal(t, "/", c.Path, "__Host- requires Path=/")
	assert.Empty(t, c.Domain, "__Host- requires no Domain attribute")
	assert.True(t, c.Secure, "__Host- requires Secure")
	assert.False(t, c.HttpOnly, "SPA must be able to read the cookie via document.cookie")
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite, "must be SameSite=Strict for CSRF protection")
	// 32 random bytes base64-url-encoded = 43 chars (no padding).
	assert.Len(t, c.Value, 43, "token must be 32 bytes base64-url-encoded without padding")
}

func TestIssueCSRFCookie_TokenIsUnique(t *testing.T) {
	// Sanity check: two successive calls produce distinct tokens. Not a
	// real entropy test (that belongs in a fuzz run), but it catches the
	// common bug of accidentally returning a constant.
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		rec := httptest.NewRecorder()
		require.NoError(t, IssueCSRFCookie(rec))
		c := rec.Result().Cookies()[0]
		assert.False(t, seen[c.Value], "token collision on iteration %d: %q", i, c.Value)
		seen[c.Value] = true
	}
}

func TestIssueCSRFCookie_HeaderIsParseable(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, IssueCSRFCookie(rec))
	setCookie := rec.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie)

	// Must include all required attributes literally.
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
