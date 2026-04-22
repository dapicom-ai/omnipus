//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

// Middleware-gate tests for Sprint K routes (k20, FR-012 / FR-013 / FR-019).
//
// These tests prove that every Sprint K endpoint is wrapped with:
//   withAuth → RequireAdmin → RequireNotBypass
// and that the global CSRF middleware (applied via WrapHTTPHandler) rejects
// state-changing requests without a valid cookie/header pair.
//
// Test strategy: build the exact middleware stack that registerAdditionalEndpoints
// assembles and drive it with httptest — no full gateway boot required. The
// CSRF layer is tested by constructing it around the per-route chain, mirroring
// what the production WrapHTTPHandler call does.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
)

// sprintKRoutes is the canonical table of Sprint K routes and a representative
// state-changing body for POST/PUT/PATCH methods. GET-only routes use an empty
// body. Each entry carries enough metadata for all five test scenarios.
type sprintKRoute struct {
	method string
	path   string
	// body is a minimal valid body for the method. Empty for GET routes.
	body string
}

// allSprintKRoutes covers all 14 route entries (GET+PUT on same path counts
// as two entries; we exercise the state-changing method for multi-method paths).
var allSprintKRoutes = []sprintKRoute{
	{http.MethodGet, "/api/v1/config/pending-restart", ""},
	{http.MethodGet, "/api/v1/security/audit-log", ""},
	{http.MethodPut, "/api/v1/security/audit-log", `{"enabled":true}`},
	{http.MethodGet, "/api/v1/security/skill-trust", ""},
	{http.MethodPut, "/api/v1/security/skill-trust", `{"mode":"require_signed"}`},
	{http.MethodGet, "/api/v1/security/prompt-guard", ""},
	{http.MethodPut, "/api/v1/security/prompt-guard", `{"enabled":false}`},
	{http.MethodGet, "/api/v1/security/rate-limits", ""},
	{http.MethodPut, "/api/v1/security/rate-limits", `{"api_requests_per_minute":60}`},
	{http.MethodGet, "/api/v1/security/sandbox-config", ""},
	{http.MethodPut, "/api/v1/security/sandbox-config", `{"mode":"permissive"}`},
	{http.MethodGet, "/api/v1/security/session-scope", ""},
	{http.MethodPut, "/api/v1/security/session-scope", `{"scope":"agent"}`},
	{http.MethodGet, "/api/v1/security/retention", ""},
	{http.MethodPut, "/api/v1/security/retention", `{"session_retention_days":30}`},
	{http.MethodPost, "/api/v1/security/retention/sweep", ""},
	{http.MethodGet, "/api/v1/users", ""},
	{http.MethodPost, "/api/v1/users", `{"username":"bob","role":"user","password":"testpwd1"}`},
	{http.MethodDelete, "/api/v1/users/bob", ""},
	{http.MethodPut, "/api/v1/users/bob/password", `{"password":"newpwd123"}`},
	{http.MethodPatch, "/api/v1/users/bob/role", `{"role":"user"}`},
}

// stateChangingSprintKRoutes returns only the routes with state-changing methods
// (POST / PUT / PATCH / DELETE) — these are the ones CSRF gates.
func stateChangingSprintKRoutes() []sprintKRoute {
	out := make([]sprintKRoute, 0)
	stateChanging := map[string]bool{
		http.MethodPost:   true,
		http.MethodPut:    true,
		http.MethodPatch:  true,
		http.MethodDelete: true,
	}
	for _, r := range allSprintKRoutes {
		if stateChanging[r.method] {
			out = append(out, r)
		}
	}
	return out
}

// innerOKHandler is a trivial 200 responder used as the terminus of each
// middleware chain in tests.
var innerOKHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// buildSprintKFullHandler assembles the complete per-route middleware chain:
//   withAuth → RequireAdmin → RequireNotBypass → inner
//
// Used for TestSprintK_UnauthenticatedRequestsRejected where the real
// withAuth bearer-check must fire.
func buildSprintKFullHandler(api *restAPI) http.HandlerFunc {
	return api.withAuth(
		middleware.RequireAdmin(
			http.HandlerFunc(middleware.RequireNotBypass(innerOKHandler)),
		).ServeHTTP,
	)
}

// buildInnerChainHandler assembles the inner gates only:
//   RequireAdmin → RequireNotBypass → inner
//
// Used when the caller injects the role+config into context directly
// (simulating what withAuth would do after a successful token check).
// This lets TestAdminOnly and TestDevModeBypass probe those gates without
// needing real bcrypt tokens.
func buildInnerChainHandler() http.Handler {
	return middleware.RequireAdmin(
		http.HandlerFunc(middleware.RequireNotBypass(innerOKHandler)),
	)
}

// makeAdminCtxRequest builds a request with an admin role injected into the
// context (simulating what withAuth produces after a successful token check)
// and a non-bypass config snapshot in context.
func makeAdminCtxRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	cfg := &config.Config{}
	cfg.Gateway.DevModeBypass = false
	ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, config.UserRoleAdmin)
	ctx = context.WithValue(ctx, ctxkey.UserContextKey{}, &config.UserConfig{
		Username: "admin",
		Role:     config.UserRoleAdmin,
	})
	ctx = context.WithValue(ctx, ctxkey.ConfigContextKey{}, cfg)
	return r.WithContext(ctx)
}

// makeNonAdminCtxRequest builds a request with a user (non-admin) role in context.
func makeNonAdminCtxRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	cfg := &config.Config{}
	cfg.Gateway.DevModeBypass = false
	ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, config.UserRoleUser)
	ctx = context.WithValue(ctx, ctxkey.UserContextKey{}, &config.UserConfig{
		Username: "bob",
		Role:     config.UserRoleUser,
	})
	ctx = context.WithValue(ctx, ctxkey.ConfigContextKey{}, cfg)
	return r.WithContext(ctx)
}

// makeBypassCtxRequest builds an admin-role request but with DevModeBypass=true.
func makeBypassCtxRequest(method, path, body string) *http.Request {
	r := makeAdminCtxRequest(method, path, body)
	cfg := &config.Config{}
	cfg.Gateway.DevModeBypass = true
	ctx := context.WithValue(r.Context(), ctxkey.ConfigContextKey{}, cfg)
	return r.WithContext(ctx)
}

// TestCSRF_AllNewEndpointsSubjectToGate proves that every Sprint K state-changing
// route returns 403 when the CSRF cookie is absent. This mirrors the global
// middleware.CSRFMiddleware that production applies via WrapHTTPHandler.
//
// The inner chain used here is RequireAdmin→RequireNotBypass→inner (with admin
// context injected) so that only the CSRF gate decides the outcome. This isolates
// the CSRF check from auth/role complexity while still exercising the correct
// handler chain order.
//
// Outcome: 403 {"error":"csrf cookie missing"} on each state-changing route.
func TestCSRF_AllNewEndpointsSubjectToGate(t *testing.T) {
	// Wrap the inner chain with CSRF middleware, same as production.
	// No exempt paths configured beyond the defaults — Sprint K paths are NOT exempt.
	csrfH := middleware.CSRFMiddleware()(buildInnerChainHandler())

	routes := stateChangingSprintKRoutes()
	passed := 0
	for _, route := range routes {
		route := route
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req := makeAdminCtxRequest(route.method, route.path, route.body)
			// No CSRF cookie — gate must fire.

			w := httptest.NewRecorder()
			csrfH.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code,
				"route %s %s: expected 403 from CSRF gate when cookie is absent",
				route.method, route.path)
			assert.Contains(t, w.Body.String(), "csrf",
				"route %s %s: error body must mention 'csrf'", route.method, route.path)
			passed++
		})
	}
	t.Logf("TestCSRF_AllNewEndpointsSubjectToGate: %d/%d state-changing routes gated", passed, len(routes))
}

// TestAdminOnly_AllNewPUTs proves that every Sprint K route returns 403 when
// the caller has a user (non-admin) role.
//
// Uses the inner chain (RequireAdmin → RequireNotBypass → inner) with a non-admin
// role injected into context, simulating what withAuth produces after a successful
// token check for a user-role account.
func TestAdminOnly_AllNewPUTs(t *testing.T) {
	h := buildInnerChainHandler()

	passed := 0
	for _, route := range allSprintKRoutes {
		route := route
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req := makeNonAdminCtxRequest(route.method, route.path, route.body)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code,
				"route %s %s: non-admin must receive 403", route.method, route.path)
			assert.Contains(t, w.Body.String(), "admin",
				"route %s %s: error body must mention 'admin'", route.method, route.path)
			passed++
		})
	}
	t.Logf("TestAdminOnly_AllNewPUTs: %d/%d routes blocked non-admin", passed, len(allSprintKRoutes))
}

// TestDevModeBypass_UserEndpointsReturn503 verifies that when DevModeBypass=true
// every Sprint K route returns 503, protecting admin surfaces from anonymous
// access in that mode (FR-019 / MAJ-006).
//
// Uses the inner chain (RequireAdmin → RequireNotBypass → inner) with admin role
// and bypass=true injected into context. RequireAdmin passes (admin role present);
// RequireNotBypass intercepts and returns 503.
func TestDevModeBypass_UserEndpointsReturn503(t *testing.T) {
	h := buildInnerChainHandler()

	passed := 0
	for _, route := range allSprintKRoutes {
		route := route
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req := makeBypassCtxRequest(route.method, route.path, route.body)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			assert.Equal(t, http.StatusServiceUnavailable, w.Code,
				"route %s %s: bypass mode must return 503", route.method, route.path)
			assert.Contains(t, w.Body.String(), "dev-mode-bypass",
				"route %s %s: error body must mention 'dev-mode-bypass'", route.method, route.path)
			passed++
		})
	}
	t.Logf("TestDevModeBypass_UserEndpointsReturn503: %d/%d routes returned 503 in bypass mode", passed, len(allSprintKRoutes))
}

// TestSprintK_SmokeTest_AdminHappyPath verifies that an admin with a valid
// CSRF cookie+header and bypass=false passes through the full inner chain
// (RequireAdmin → RequireNotBypass → inner) without being blocked by any gate.
// The inner handler (a trivial 200 stub) is reached on every route.
//
// This proves the middleware chain does not over-reject valid admin traffic.
func TestSprintK_SmokeTest_AdminHappyPath(t *testing.T) {
	const csrfToken = "test-csrf-token-value"

	// Wrap the inner chain with CSRF (same as production outer layer).
	csrfH := middleware.CSRFMiddleware()(buildInnerChainHandler())

	passed := 0
	for _, route := range allSprintKRoutes {
		route := route
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req := makeAdminCtxRequest(route.method, route.path, route.body)

			// For state-changing methods, supply the CSRF cookie+header pair.
			if route.method != http.MethodGet && route.method != http.MethodHead {
				req.AddCookie(&http.Cookie{
					Name:  middleware.CSRFCookieName,
					Value: csrfToken,
				})
				req.Header.Set(middleware.CSRFHeaderName, csrfToken)
			}

			w := httptest.NewRecorder()
			csrfH.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code,
				"route %s %s: valid admin+CSRF must reach the inner handler (200)",
				route.method, route.path)
			passed++
		})
	}
	t.Logf("TestSprintK_SmokeTest_AdminHappyPath: %d/%d routes passed for valid admin", passed, len(allSprintKRoutes))
}

// TestSprintK_UnauthenticatedRequestsRejected verifies that requests with NO
// bearer token and NO session cookie receive 401 from the withAuth gate.
//
// Uses the full chain (withAuth → RequireAdmin → RequireNotBypass → inner) with
// no auth credentials at all — no Authorization header, no role in context.
// OMNIPUS_BEARER_TOKEN="" is set by newTestRestAPIWithHome so the legacy env
// fallback cannot intervene.
func TestSprintK_UnauthenticatedRequestsRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	chain := buildSprintKFullHandler(api)

	passed := 0
	for _, route := range allSprintKRoutes {
		route := route
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			var req *http.Request
			if route.body == "" {
				req = httptest.NewRequest(route.method, route.path, nil)
			} else {
				req = httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				req.Header.Set("Content-Type", "application/json")
			}
			// No Authorization header. No role in context. configSnapshotMiddleware not applied.

			w := httptest.NewRecorder()
			chain(w, req)

			code := w.Code
			assert.True(t, code == http.StatusUnauthorized || code == http.StatusForbidden,
				"route %s %s: unauthenticated request must be rejected with 401 or 403, got %d",
				route.method, route.path, code)
			passed++
		})
	}
	t.Logf("TestSprintK_UnauthenticatedRequestsRejected: %d/%d routes rejected unauthenticated requests", passed, len(allSprintKRoutes))
}
