//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package middleware provides HTTP middleware for the Omnipus gateway.
//
// CSRF protection (this file) implements the double-submit cookie pattern:
// a random value is stored in an HttpOnly=false __Host-csrf cookie and must
// be echoed in the X-CSRF-Token request header on every state-changing
// method. Cross-origin JavaScript cannot read the cookie (same-origin policy
// applies to cookies bound to our origin), so an attacker cannot forge the
// header even if they can trick a browser into sending the request.
//
// References:
//   - OWASP CSRF cheat sheet: https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html
//   - __Host- cookie prefix: https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies#cookie_prefixes
//   - Plan reference: temporal-puzzling-melody.md §1 (CSRF/Origin decision)
//   - Issue #97: https://github.com/dapicom-ai/omnipus/issues/97
package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// CSRFCookieName is the name of the double-submit cookie.
//
// The __Host- prefix enforces three properties at the browser layer:
//   - Secure must be set
//   - Domain must be absent (attaches only to the exact host that issued it)
//   - Path must be /
//
// If any of those are violated the browser refuses to store the cookie.
// This is defense-in-depth against a weaker cookie slipping out.
const CSRFCookieName = "__Host-csrf"

// CSRFHeaderName is the header that clients must echo with the cookie value.
const CSRFHeaderName = "X-CSRF-Token"

// csrfTokenBytes is the entropy of a fresh token (256 bits).
const csrfTokenBytes = 32

// exemptPaths are paths that bypass the cookie+header check but still receive
// a freshly-issued __Host-csrf cookie on their response when appropriate.
//
// /api/v1/onboarding/complete is exempted because the first caller has no
// cookie yet — on a fresh install the SPA runs before any credential round-trip.
// The handler itself issues the cookie in the response so subsequent
// state-changing calls (e.g., /api/v1/auth/login on second startup) are gated.
//
// /health, /ready, /reload are operational endpoints exposed by the health
// server. They are intended for operators (curl, kubelet probes, systemd) that
// do not carry browser credentials, so there is no CSRF attack surface: an
// attacker origin cannot trick a browser into calling them with privileged
// context. Gating them would break liveness probes and operator tooling.
//
// Plan reference: temporal-puzzling-melody.md §1.
var exemptPaths = map[string]struct{}{
	"/api/v1/onboarding/complete": {},
	"/health":                     {},
	"/ready":                      {},
	"/reload":                     {},
}

// stateChangingMethods lists the HTTP verbs that trigger CSRF enforcement.
// GET/HEAD/OPTIONS are RFC-safe methods and MUST NOT be gated — doing so
// breaks preflight and simple reads. PATCH is included because it mutates.
var stateChangingMethods = map[string]struct{}{
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
}

// MismatchReporter is called when a state-changing request passes the cookie
// presence check but the cookie and header don't match. Implementations
// typically write to the audit log (SEC-15). Passing nil disables reporting;
// the middleware still rejects the request.
//
// route is the raw r.URL.Path, sourceIP is the remote IP extracted by the
// caller's convention (X-Forwarded-For, RemoteAddr, etc.). The middleware
// deliberately does not parse IP itself — that concern belongs to the
// gateway's existing clientIP helper.
type MismatchReporter func(r *http.Request, sourceIP, route string)

// Config configures the CSRF middleware. A zero value is usable: exempt
// paths default to the onboarding-complete endpoint and the reporter is nil.
type Config struct {
	// ExemptPaths, if non-nil, REPLACES the default exempt list. Callers
	// who want to keep the default and add more should merge manually.
	// Exact match only — no prefix or glob.
	ExemptPaths map[string]struct{}

	// Reporter, if non-nil, is invoked on every cookie+header mismatch
	// before the 403 response is written. It must not write to the
	// ResponseWriter; it is for logging only.
	Reporter MismatchReporter

	// ClientIP extracts the caller IP from a request. If nil, the
	// middleware falls back to r.RemoteAddr (which may include the port).
	// This avoids a hard dependency on the gateway's clientIP helper.
	ClientIP func(r *http.Request) string
}

// CSRFMiddleware returns an HTTP middleware that enforces the double-submit
// cookie CSRF check on state-changing requests.
//
// Semantics:
//   - GET, HEAD, OPTIONS: pass through unchanged.
//   - Exempt paths (see Config.ExemptPaths): pass through. The handler is
//     expected to call IssueCSRFCookie to seed a cookie for the client.
//   - POST, PUT, PATCH, DELETE on non-exempt paths:
//     1. If the __Host-csrf cookie is missing → 403 "csrf cookie missing".
//     2. If the X-CSRF-Token header is missing → 403 "csrf header missing".
//     3. If cookie and header don't match (constant-time compare) →
//     403 "csrf token mismatch", Reporter invoked.
//     4. Match → request proceeds to next.
//
// The middleware NEVER allows a state-changing request through when the
// check fails. Fail-closed is the only correct behaviour for a gate.
func CSRFMiddleware(cfg Config) func(http.Handler) http.Handler {
	exempt := cfg.ExemptPaths
	if exempt == nil {
		exempt = exemptPaths
	}
	clientIP := cfg.ClientIP
	if clientIP == nil {
		clientIP = func(r *http.Request) string { return r.RemoteAddr }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Safe methods — RFC 7231 §4.2.1: GET, HEAD, OPTIONS are side-effect-free.
			if _, safe := stateChangingMethods[r.Method]; !safe {
				next.ServeHTTP(w, r)
				return
			}

			// Exempt route — the handler itself is responsible for seeding
			// a cookie. See IssueCSRFCookie.
			if _, ok := exempt[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(CSRFCookieName)
			if err != nil || cookie.Value == "" {
				writeCSRFError(w, "csrf cookie missing")
				return
			}

			header := r.Header.Get(CSRFHeaderName)
			if header == "" {
				writeCSRFError(w, "csrf header missing")
				return
			}

			if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
				if cfg.Reporter != nil {
					cfg.Reporter(r, clientIP(r), r.URL.Path)
				}
				writeCSRFError(w, "csrf token mismatch")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IssueCSRFCookie generates a fresh 256-bit random token, base64-url encodes
// it, and writes it as the __Host-csrf cookie on the response.
//
// Cookie attributes (all required for double-submit + __Host- prefix):
//   - HttpOnly: false — the SPA must read this cookie to echo the header.
//   - Secure: true — TLS only. Required by __Host-.
//   - SameSite: Strict — prevents cross-origin sends. Defense-in-depth on
//     top of the header check.
//   - Path: /. Required by __Host-.
//   - Domain: unset. Required by __Host-.
//
// Returns an error if the OS RNG fails (practically impossible but the
// contract is honest). Callers should surface a 500 if so.
//
// On a dev-mode server bound to plain HTTP (no TLS) the browser will refuse
// to store a Secure cookie. For localhost this is usually fine because
// modern browsers treat 127.0.0.1/localhost as a "potentially trustworthy
// origin" and honour Secure cookies over http. For arbitrary hosts, the
// gateway must run on TLS.
func IssueCSRFCookie(w http.ResponseWriter) error {
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("csrf: rand.Read: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		// No Domain — __Host- forbids it.
		// No MaxAge — session cookie; lives until the browser closes. The
		// SPA re-issues on every login/onboarding/refresh.
	})
	return nil
}

// writeCSRFError writes a 403 JSON error response. We deliberately use a
// single fmt.Fprintf instead of encoding/json to avoid pulling an import.
// The response shape matches the rest of the gateway's { "error": "..." }
// convention so the SPA's existing error path handles it uniformly.
func writeCSRFError(w http.ResponseWriter, msg string) {
	// Escape the " in msg — none of our error strings contain quotes but be
	// defensive in case a future caller passes untrusted input.
	escaped := strings.ReplaceAll(msg, `"`, `\"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"error":"%s"}`, escaped)
}
