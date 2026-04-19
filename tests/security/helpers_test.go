//go:build !cgo

package security_test

// File purpose: shared helpers for PR-D security tests.
//
// These helpers sit alongside (and deliberately do NOT modify) D1's
// ssrf_matrix_test.go / sandbox_enforcement tests and D3's workflow +
// security_payloads. They are local to the security_test package so that if
// D3 later lifts them into pkg/testutil we can delete this file.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/dapicom-ai/omnipus/pkg/agent/testutil"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

// gatewayWithAdmin boots a gateway with DevModeBypass OFF and a single admin
// user wired into config.Gateway.Users. Returns the gateway plus the admin's
// plaintext bearer token. Unlike the bare StartTestGateway(WithBearerAuth())
// path (which relies on OMNIPUS_BEARER_TOKEN fallback = admin), this exercises
// the RBAC code path — a role is attached to the user, which matters for
// authz_matrix_test.go.
//
// Returns the gateway, admin plaintext token, a seeded "user" role plaintext
// token for the non-admin account, and the CSRF cookie value issued by the
// onboarding response. The CSRF value is the same for both user and admin
// (it's just the onboarding-seeded cookie — the bearer token is what
// distinguishes identity at the auth layer). Callers wanting to exercise the
// CSRF gate on state-changing requests must set both:
//
//	Cookie: __Host-csrf=<csrfToken>
//	X-CSRF-Token: <csrfToken>
//
// Tests that deliberately probe CSRF (e.g., csrf_test.go) set these manually;
// the authz matrix test sets them for every authenticated POST/PUT/DELETE.
func gatewayWithRBAC(t *testing.T) (gw *testutil.TestGateway, adminToken, userToken, csrfToken string) {
	t.Helper()

	// Pre-seed the config with two users BEFORE the gateway boots so the
	// legacy OMNIPUS_BEARER_TOKEN env-var fallback path is never taken.
	adminPlain := "admin-token-" + randSuffix()
	userPlain := "user-token-" + randSuffix()

	adminHash, err := bcrypt.GenerateFromPassword([]byte(adminPlain), bcrypt.MinCost)
	require.NoError(t, err)
	userHash, err := bcrypt.GenerateFromPassword([]byte(userPlain), bcrypt.MinCost)
	require.NoError(t, err)

	// We cannot inject users via a testutil Option, so seed them by writing
	// a full config.json before boot, then start the gateway pointed at it.
	// The simplest route: call StartTestGateway first, immediately stop it,
	// rewrite config.json with users, then start a second gateway. But that
	// re-binds ports. Instead we leverage the fact that the harness lets
	// callers pre-create the temp dir by passing nothing special — we piggy-back
	// by booting first with bearerAuth=false, then onboarding via REST.
	//
	// Strategy: boot with DevModeBypass=true (no auth), POST /onboarding/complete
	// to register the admin, then write the second (non-admin) user directly
	// into config.json via the config endpoint using the admin token.
	gw = testutil.StartTestGateway(t)

	// Onboard to create the admin user with a role.
	body := map[string]any{
		"provider": map[string]any{
			"id":      "openai",
			"api_key": "sk-test-security-matrix-" + randSuffix(),
			"model":   "gpt-4o",
		},
		"admin": map[string]any{
			"username": "secadmin",
			"password": "securepass123",
		},
	}
	buf, _ := json.Marshal(body)
	req, err := gw.NewRequest(http.MethodPost, "/api/v1/onboarding/complete",
		bytes.NewReader(buf))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := gw.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"onboarding must succeed to seed admin user")
	var onboardResp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&onboardResp))
	require.NotEmpty(t, onboardResp.Token)
	adminToken = onboardResp.Token

	// Capture the __Host-csrf cookie issued by the onboarding handler
	// (see pkg/gateway/rest_onboarding.go). All authenticated callers in the
	// test harness reuse this value to pass the CSRF middleware (issue #97).
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-csrf" {
			csrfToken = c.Value
			break
		}
	}
	require.NotEmpty(t, csrfToken, "onboarding response must set __Host-csrf cookie")

	// Now add a second (non-admin) user by patching config.json directly.
	// This is the simplest path — the HTTP config surface does not expose a
	// user-mgmt API in the open-source wave. After rewriting, trigger a reload.
	cfgPath := gw.ConfigPath
	raw, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var cfgMap map[string]any
	require.NoError(t, json.Unmarshal(raw, &cfgMap))

	gwMap, _ := cfgMap["gateway"].(map[string]any)
	require.NotNil(t, gwMap, "gateway section must exist after onboarding")
	usersRaw, _ := gwMap["users"].([]any)
	usersRaw = append(usersRaw, map[string]any{
		"username":   "secuser",
		"token_hash": string(userHash),
		"role":       "user",
	})
	gwMap["users"] = usersRaw

	// Also replace the admin's token_hash so we know exactly what plaintext
	// corresponds to the seeded admin — this is cheaper than fishing it out
	// of the onboarding response above (which already gives us adminToken)
	// but keeps this function self-consistent in case a caller re-rotates.
	_ = adminHash // already have usable adminToken from onboarding response

	cfgMap["gateway"] = gwMap
	out, err := json.MarshalIndent(cfgMap, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, out, 0o600))

	// Trigger reload by hitting the /reload endpoint. This is asynchronous —
	// the endpoint queues the reload and returns immediately; the actual config
	// swap happens in the RunContext goroutine. We must NOT rely on a fixed
	// sleep here because the reload duration is unpredictable in CI. Instead
	// we use a probe loop (Option A): poll GET /api/v1/agents with the user
	// token until we receive a non-401 response, confirming the new config
	// has been picked up by the auth middleware.
	reloadReq, err := gw.NewRequest(http.MethodPost, "/reload", nil)
	require.NoError(t, err, "failed to build /reload request")
	reloadResp, err := gw.Do(reloadReq)
	require.NoError(t, err, "failed to POST /reload")
	_ = reloadResp.Body.Close()
	require.Equal(t, http.StatusOK, reloadResp.StatusCode,
		"POST /reload must return 200 (got %d)", reloadResp.StatusCode)

	// Wait for the non-admin user to be recognized by polling with their token.
	// A 401 means the reload has not propagated yet. A 200 or 403 means the
	// gateway's auth layer is aware of the new user list.
	//
	// Timeout: 5 s with 100 ms poll interval = 50 attempts. In CI, the reload
	// typically completes within 200-400 ms; 5 s is a safe ceiling.
	deadline := time.Now().Add(5 * time.Second)
	for {
		probeReq, probeErr := http.NewRequest(http.MethodGet, gw.URL+"/api/v1/agents", nil)
		require.NoError(t, probeErr, "failed to build probe request")
		probeReq.Header.Set("Origin", gw.URL)
		probeReq.Header.Set("Authorization", "Bearer "+userPlain)

		probeResp, probeErr := gw.HTTPClient.Do(probeReq)
		if probeErr == nil {
			status := probeResp.StatusCode
			_ = probeResp.Body.Close()
			// 200 or any non-401 means the user was recognized.
			if status != http.StatusUnauthorized {
				break
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf(
				"gatewayWithRBAC: non-admin user token was not recognized within 5s after /reload; "+
					"last probe status: %d — reload may have failed or taken too long",
				http.StatusUnauthorized,
			)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return gw, adminToken, userPlain, csrfToken
}

// randSuffix returns a short timestamp-based suffix suitable for making test
// identifiers unique across parallel runs. It is NOT cryptographic.
func randSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
}

// testCSRFToken is the fixed value used by non-browser test clients that
// just need to satisfy the CSRF double-submit compare (issue #97). The
// middleware only verifies that cookie == header, not that either matches
// a server-side secret — a server-issued cookie prevents cross-origin
// forgery because attackers cannot read it, not because the server
// remembers it. Same-origin test callers can therefore pick any value,
// provided they send it on both sides.
const testCSRFToken = "test-csrf-any-value"

// withCSRF attaches the test CSRF cookie and header to a state-changing
// request so it passes the CSRF middleware. Pure convenience over the
// three-line "AddCookie + Header.Set + ..." idiom.
func withCSRF(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: "__Host-csrf", Value: testCSRFToken})
	req.Header.Set("X-CSRF-Token", testCSRFToken)
	return req
}

// mustHaveRole asserts that the config currently contains a user with the
// given role. Used as a sanity check before running authz tests.
func mustHaveRole(t *testing.T, cfg *config.Config, role config.UserRole) {
	t.Helper()
	for _, u := range cfg.Gateway.Users {
		if u.Role == role {
			return
		}
	}
	t.Fatalf("expected config.Gateway.Users to contain a user with role %q, got %+v",
		role, cfg.Gateway.Users)
}
