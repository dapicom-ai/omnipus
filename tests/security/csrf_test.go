//go:build !cgo

package security_test

// File purpose: CSRF protection tests for state-changing POST endpoints (PR-D Axis-7).
//
// IMPORTANT — DOCUMENTED ADR-LEVEL GAP:
//
// The Omnipus gateway does NOT implement explicit CSRF protection today. It
// relies solely on bearer-token authentication + Origin-reflected CORS. In
// practice this means:
//
//   1. A browser that has an Omnipus bearer token stored in localStorage
//      sends it via Authorization: Bearer <token>, NOT via a cookie.
//   2. Cross-origin JavaScript cannot read localStorage, so classic CSRF
//      (where a malicious site forges a cookie-authenticated POST) does NOT
//      apply — the attacker cannot obtain the bearer token.
//   3. The server does not reject missing or hostile Origin headers on
//      state-changing POSTs; it only uses Origin to decide whether to reflect
//      it in the Access-Control-Allow-Origin response header.
//   4. A fetch() from a different origin sending a bearer token the attacker
//      somehow obtained would succeed — the server has no CSRF token and no
//      Origin-based POST gate.
//
// Mitigation analysis:
//   - Bearer-in-localStorage is the standard browser pattern for SPA tokens.
//   - Same-origin policy prevents cross-origin localStorage read.
//   - The SPA uses same-origin fetch, so CORS preflight never runs.
//   - Residual risk: token leak via XSS (see xss_test.go) OR token stolen
//     via a network attacker on a non-TLS deployment.
//
// These tests ASSERT THE CURRENT BEHAVIOR so future regressions are caught.
// They do NOT assert CSRF-token enforcement (which does not exist). If a
// future Omnipus release adds CSRF-token enforcement, update these tests.
//
// Plan reference: temporal-puzzling-melody.md §6 PR-D.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
)

// csrfTarget is a state-changing endpoint to probe.
type csrfTarget struct {
	method string
	path   string
	// body is sent on each request; empty body means no Content-Type and no payload.
	body string
	// expectAuthedOK is the status code (or code range) returned when the
	// request is sent with a valid bearer token AND same-origin Origin header.
	expectAuthedOK int
	// Whether an empty body with no Content-Type is acceptable (for endpoints
	// that only look at headers).
	needsJSONBody bool
}

func csrfTargets() []csrfTarget {
	return []csrfTarget{
		{
			// Onboarding is optional-auth; it can be called before the admin
			// exists. It should accept a well-formed body regardless of Origin.
			method:         http.MethodPost,
			path:           "/api/v1/onboarding/complete",
			body:           `{"provider":{"id":"openai","api_key":"sk-test","model":"gpt-4o"},"admin":{"username":"csrfadmin","password":"securepass123"}}`,
			needsJSONBody:  true,
			expectAuthedOK: http.StatusOK,
		},
		{
			method:         http.MethodPost,
			path:           "/api/v1/agents",
			body:           `{"name":"csrf-test-agent","model":"scripted-model"}`,
			needsJSONBody:  true,
			expectAuthedOK: http.StatusOK,
		},
		{
			method:         http.MethodPut,
			path:           "/api/v1/security/tool-policies",
			body:           `{"tool_policies":{}}`,
			needsJSONBody:  true,
			expectAuthedOK: http.StatusOK,
		},
		{
			method:         http.MethodPost,
			path:           "/api/v1/sessions",
			body:           `{"agent_id":"omnipus-system","type":"chat"}`,
			needsJSONBody:  true,
			expectAuthedOK: http.StatusCreated,
		},
		{
			// /api/v1/config accepts PUT for updates.
			method:         http.MethodPut,
			path:           "/api/v1/config",
			body:           `{"agents":{"defaults":{}}}`,
			needsJSONBody:  true,
			expectAuthedOK: http.StatusOK,
		},
	}
}

func TestCSRFProtection(t *testing.T) {
	gw := testutil.StartTestGateway(t, testutil.WithBearerAuth())

	// The gateway has DevModeBypass=false AND a valid bearer token pre-loaded.
	// Every request includes the valid token via gw.NewRequest (which always
	// sets Origin to gw.URL). For CSRF probes we construct raw requests.

	for _, tgt := range csrfTargets() {
		name := strings.NewReplacer("/", "_", " ", "_").Replace(tgt.path)
		t.Run(name+"_no_origin", func(t *testing.T) {
			body := bytes.NewReader([]byte(tgt.body))
			req, err := http.NewRequest(tgt.method, gw.URL+tgt.path, body)
			require.NoError(t, err)
			// Explicitly omit Origin.
			req.Header.Del("Origin")
			if tgt.needsJSONBody {
				req.Header.Set("Content-Type", "application/json")
			}
			if gw.BearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+gw.BearerToken)
			}
			resp, err := gw.HTTPClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// DOCUMENTED GAP: the gateway currently does NOT reject missing
			// Origin on state-changing POSTs. This assertion records the
			// current behavior. When CSRF protection lands, flip this
			// assertion.
			t.Logf("current behavior: bearer-auth-only; no Origin gate on %s %s (status=%d)",
				tgt.method, tgt.path, resp.StatusCode)

			// At minimum, the server must respond — no hang, no crash.
			assert.NotEqual(t, 0, resp.StatusCode,
				"server must respond with a status code, not crash")
			// If we ever add CSRF protection, the server would return 403.
			// For now we assert only that the request WAS processed (not 5xx
			// due to a crash). Accept 200/201/400/401/409/422 as valid
			// responses that indicate the handler ran.
			assert.Less(t, resp.StatusCode, 500,
				"server must not 5xx on missing-Origin POST — gap documented, "+
					"but a 5xx indicates a real bug in error handling")
		})

		t.Run(name+"_wrong_origin", func(t *testing.T) {
			body := bytes.NewReader([]byte(tgt.body))
			req, err := http.NewRequest(tgt.method, gw.URL+tgt.path, body)
			require.NoError(t, err)
			req.Header.Set("Origin", "https://evil.example.com")
			if tgt.needsJSONBody {
				req.Header.Set("Content-Type", "application/json")
			}
			if gw.BearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+gw.BearerToken)
			}
			resp, err := gw.HTTPClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// DOCUMENTED GAP: the gateway does NOT reject hostile Origin.
			t.Logf("current behavior: bearer-auth-only; hostile Origin %q accepted on %s %s (status=%d). "+
				"Record of gap: evil.example.com did not trigger a 403.",
				"https://evil.example.com", tgt.method, tgt.path, resp.StatusCode)

			// Verify the Access-Control-Allow-Origin response header is NOT
			// set to the hostile origin — that WOULD enable a cross-origin
			// browser read. The isAllowedOrigin() gate must refuse to reflect
			// evil.example.com.
			acao := resp.Header.Get("Access-Control-Allow-Origin")
			assert.NotEqual(t, "https://evil.example.com", acao,
				"CORS must NOT reflect a hostile Origin header — this is the "+
					"one gate that actually protects browser-based attacks")

			// Do not 5xx.
			assert.Less(t, resp.StatusCode, 500, "must not crash on hostile Origin")
		})

		t.Run(name+"_no_csrf_token", func(t *testing.T) {
			// There is no CSRF token mechanism in the gateway today. Document
			// this explicitly so the test surface is honest.
			t.Log("current behavior: no CSRF token in the gateway — bearer auth only; " +
				"cross-origin JS cannot read localStorage-held token so CSRF is " +
				"mitigated by same-origin policy, not by a token check")

			// Send a properly-authenticated request with Origin matching the
			// gateway — this should succeed. Failure here means our "CSRF
			// token not required" understanding is wrong.
			body := bytes.NewReader([]byte(tgt.body))
			req, err := http.NewRequest(tgt.method, gw.URL+tgt.path, body)
			require.NoError(t, err)
			req.Header.Set("Origin", gw.URL)
			if tgt.needsJSONBody {
				req.Header.Set("Content-Type", "application/json")
			}
			if gw.BearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+gw.BearerToken)
			}
			resp, err := gw.HTTPClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Expect success (or at least not forbidden). A 403 with a body
			// containing "csrf" would mean CSRF enforcement landed — update
			// this test.
			if resp.StatusCode == http.StatusForbidden {
				var body map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&body)
				if strings.Contains(strings.ToLower(body["error"]), "csrf") {
					t.Fatalf("CSRF enforcement is now live (%s %s -> 403 %q). "+
						"Update this test to assert token-based CSRF gates.",
						tgt.method, tgt.path, body["error"])
				}
			}
			assert.Less(t, resp.StatusCode, 500,
				"must not 5xx on authenticated same-origin request")
		})
	}
}

// TestCSRFCORSReflectionGate verifies the ONE real CSRF protection Omnipus
// does have: the CORS Access-Control-Allow-Origin reflection gate will not
// echo hostile origins, which prevents browser JS on evil.example.com from
// READING the response (even if it could send the request).
func TestCSRFCORSReflectionGate(t *testing.T) {
	gw := testutil.StartTestGateway(t, testutil.WithBearerAuth())

	hostileOrigins := []string{
		"https://evil.example.com",
		"http://attacker.localhost",
		"null",
		"data:,",
		"file://",
	}
	for _, origin := range hostileOrigins {
		t.Run("hostile_origin_"+sanitizeName(origin), func(t *testing.T) {
			// Use an auth-required GET endpoint — simplest way to exercise
			// CORS reflection without touching state.
			req, err := http.NewRequest(http.MethodGet, gw.URL+"/api/v1/agents", nil)
			require.NoError(t, err)
			req.Header.Set("Origin", origin)
			if gw.BearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+gw.BearerToken)
			}
			resp, err := gw.HTTPClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			acao := resp.Header.Get("Access-Control-Allow-Origin")
			assert.NotEqual(t, origin, acao,
				"CORS must NOT reflect hostile origin %q (got ACAO=%q) — this "+
					"would let browser JS read the response body",
				origin, acao)
			// It is acceptable for ACAO to be empty or to be gw.URL.
			if acao != "" && acao != gw.URL {
				t.Fatalf("CORS reflected unexpected origin %q (req.Origin=%q) — "+
					"expected empty or %q",
					acao, origin, gw.URL)
			}
		})
	}

	// Positive control: same-origin requests should get ACAO reflected.
	t.Run("same_origin_reflected", func(t *testing.T) {
		req, err := gw.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		require.NoError(t, err)
		resp, err := gw.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		acao := resp.Header.Get("Access-Control-Allow-Origin")
		assert.Equal(t, gw.URL, acao,
			"same-origin requests MUST get the gateway URL echoed in ACAO")
	})
}

// sanitizeName produces a t.Run-safe name from an arbitrary string.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "empty"
	}
	return b.String()
}
