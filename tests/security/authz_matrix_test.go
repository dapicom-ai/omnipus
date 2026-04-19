//go:build !cgo

package security_test

// File purpose: authorization matrix tests for state-changing REST endpoints (PR-D Axis-7).
//
// Threat model: the REST API must enforce three levels of access:
//   - anonymous (no bearer token) → reject with 401
//   - user (valid token, UserRoleUser) → allow only read surface + own resources
//   - admin (valid token, UserRoleAdmin) → allow everything
//
// The matrix is populated from a manual reading of pkg/gateway/rest.go's
// registerAdditionalEndpoints() list (≥30 rows per task spec). The matrix
// asserts actual current behavior, not aspirational behavior — any cell that
// does not match current behavior is a REAL RBAC GAP the test will flag.
//
// Plan reference: temporal-puzzling-melody.md §6 PR-D.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// authRole enumerates the three principal classes we test.
type authRole string

const (
	roleAnon  authRole = "anonymous"
	roleUser  authRole = "user"
	roleAdmin authRole = "admin"
)

// matrixCase is one row in the role × method × endpoint matrix.
type matrixCase struct {
	role   authRole
	method string
	path   string
	// body, if non-empty, is sent with Content-Type: application/json.
	body string
	// One of:
	//   - a specific status code (e.g., 200)
	//   - a range start (e.g., 200, where we accept 200-299)
	expectStatus []int
	// note describes expected behavior for debugging.
	note string
	// wantBodyContains, if non-empty, asserts the response body contains the
	// given substring. Checked only after status assertion passes.
	wantBodyContains string
}

// authzMatrix returns the full ≥30 row matrix. The cases below reflect TODAY'S
// behavior, not a wishlist. Endpoints that should be admin-only but are not
// are flagged with "GAP:" notes.
func authzMatrix() []matrixCase {
	return []matrixCase{
		// ---- Read surface: all three roles ----
		{roleAnon, http.MethodGet, "/api/v1/agents", "", []int{401}, "anon must be rejected", ""},
		{roleUser, http.MethodGet, "/api/v1/agents", "", []int{200}, "user may read agent list", ""},
		{roleAdmin, http.MethodGet, "/api/v1/agents", "", []int{200}, "admin may read agent list", ""},

		{roleAnon, http.MethodGet, "/api/v1/config", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/config", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/config", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/status", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/status", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/status", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/tasks", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/tasks", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/tasks", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/tools", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/tools", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/tools", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/sessions", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/sessions", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/sessions", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/security/sandbox-status", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/security/sandbox-status", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/security/sandbox-status", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/security/tool-policies", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/security/tool-policies", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/security/tool-policies", "", []int{200}, "", ""},

		{roleAnon, http.MethodGet, "/api/v1/security/rate-limits", "", []int{401}, "", ""},
		{roleUser, http.MethodGet, "/api/v1/security/rate-limits", "", []int{200}, "", ""},
		{roleAdmin, http.MethodGet, "/api/v1/security/rate-limits", "", []int{200}, "", ""},

		{role: roleAnon, method: http.MethodGet, path: "/api/v1/audit-log", expectStatus: []int{401}},
		{
			role:             roleUser,
			method:           http.MethodGet,
			path:             "/api/v1/audit-log",
			expectStatus:     []int{403},
			note:             "admin-only: user must receive 403 (Issue #98)",
			wantBodyContains: "admin required",
		},
		{role: roleAdmin, method: http.MethodGet, path: "/api/v1/audit-log", expectStatus: []int{200}},

		// ---- Write surface: createAgent (POST) ----
		// Anon on a state-changing route: CSRF middleware runs before auth and
		// returns 403 "csrf cookie missing" for a caller without a __Host-csrf
		// cookie (issue #97). Stricter than the original 401, still a hard deny.
		{
			roleAnon, http.MethodPost, "/api/v1/agents",
			`{"name":"a1","model":"scripted-model"}`,
			[]int{401, 403},
			"anon on state-changing route: CSRF (403) or auth (401) is a hard deny",
			"",
		},
		{
			roleUser, http.MethodPost, "/api/v1/agents",
			`{"name":"authz-user-a","model":"scripted-model"}`,
			[]int{200, 201},
			"GAP: user can create agents (admin-only?)", "",
		},
		{
			roleAdmin, http.MethodPost, "/api/v1/agents",
			`{"name":"authz-admin-a","model":"scripted-model"}`,
			[]int{200, 201},
			"", "",
		},

		// ---- Write surface: sessions POST ----
		// Anon + CSRF: see note on POST /api/v1/agents above.
		{
			roleAnon, http.MethodPost, "/api/v1/sessions",
			`{"agent_id":"omnipus-system","type":"chat"}`,
			[]int{401, 403},
			"anon on state-changing route: CSRF (403) or auth (401) is a hard deny",
			"",
		},
		{
			roleUser, http.MethodPost, "/api/v1/sessions",
			`{"agent_id":"omnipus-system","type":"chat"}`,
			[]int{201, 400},
			"agent existence depends on seed", "",
		},
		{
			roleAdmin, http.MethodPost, "/api/v1/sessions",
			`{"agent_id":"omnipus-system","type":"chat"}`,
			[]int{201, 400},
			"agent existence depends on seed", "",
		},

		// ---- Config PUT (admin-only in any reasonable model) ----
		// Anon + CSRF: see note on POST /api/v1/agents above.
		{
			roleAnon, http.MethodPut, "/api/v1/config",
			`{"agents":{"defaults":{}}}`,
			[]int{401, 403},
			"anon on state-changing route: CSRF (403) or auth (401) is a hard deny",
			"",
		},
		{
			role: roleUser, method: http.MethodPut, path: "/api/v1/config",
			body:             `{"agents":{"defaults":{}}}`,
			expectStatus:     []int{403},
			note:             "admin-only: user must be rejected with 403 (Issue #98)",
			wantBodyContains: "admin required",
		},
		{
			roleAdmin, http.MethodPut, "/api/v1/config",
			`{"agents":{"defaults":{}}}`,
			[]int{200, 400},
			"", "",
		},

		// ---- tool-policies PUT (admin-only in any reasonable model) ----
		// Anon + CSRF: see note on POST /api/v1/agents above.
		{
			roleAnon, http.MethodPut, "/api/v1/security/tool-policies",
			`{"tool_policies":{}}`,
			[]int{401, 403},
			"anon on state-changing route: CSRF (403) or auth (401) is a hard deny",
			"",
		},
		{
			role: roleUser, method: http.MethodPut, path: "/api/v1/security/tool-policies",
			body:             `{"tool_policies":{}}`,
			expectStatus:     []int{403},
			note:             "admin-only: user must be rejected with 403 (Issue #98)",
			wantBodyContains: "admin required",
		},
		{
			roleAdmin, http.MethodPut, "/api/v1/security/tool-policies",
			`{"tool_policies":{}}`,
			[]int{200, 400},
			"", "",
		},

		// ---- Credentials (admin-only even in simple models) ----
		{roleAnon, http.MethodGet, "/api/v1/credentials", "", []int{401}, "", ""},
		{
			role: roleUser, method: http.MethodGet, path: "/api/v1/credentials",
			expectStatus:     []int{403},
			note:             "admin-only: user must be rejected with 403 (Issue #98)",
			wantBodyContains: "admin required",
		},
		{roleAdmin, http.MethodGet, "/api/v1/credentials", "", []int{200}, "", ""},
	}
}

func TestAuthorizationMatrix(t *testing.T) {
	gw, adminToken, userToken, csrfToken := gatewayWithRBAC(t)

	// Sanity check: the seeded config has both roles.
	cfg := findTestConfig(t, gw.ConfigPath)
	mustHaveRole(t, cfg, "admin")
	mustHaveRole(t, cfg, "user")

	matrix := authzMatrix()
	require.GreaterOrEqual(t, len(matrix), 30,
		"matrix must have at least 30 rows per task spec (got %d)", len(matrix))

	for i, tc := range matrix {
		name := matrixName(i, tc)
		t.Run(name, func(t *testing.T) {
			var token string
			switch tc.role {
			case roleAnon:
				token = ""
			case roleUser:
				token = userToken
			case roleAdmin:
				token = adminToken
			}

			var body io.Reader
			if tc.body != "" {
				body = bytes.NewReader([]byte(tc.body))
			}
			req, err := http.NewRequest(tc.method, gw.URL+tc.path, body)
			if err != nil {
				t.Fatalf("build req: %v", err)
			}
			req.Header.Set("Origin", gw.URL)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
				// Authenticated callers attach the CSRF cookie + header on
				// state-changing methods so the CSRF middleware does not
				// short-circuit the request before auth runs (issue #97).
				// Anon rows deliberately omit both to exercise the CSRF gate.
				switch tc.method {
				case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
					req.AddCookie(&http.Cookie{Name: "__Host-csrf", Value: csrfToken})
					req.Header.Set("X-Csrf-Token", csrfToken)
				}
			}
			resp, err := gw.HTTPClient.Do(req)
			if err != nil {
				t.Fatalf("do req: %v", err)
			}
			defer resp.Body.Close()

			raw, _ := io.ReadAll(resp.Body)
			rawStr := string(raw)

			ok := false
			for _, want := range tc.expectStatus {
				if resp.StatusCode == want {
					ok = true
					break
				}
			}
			if !ok {
				note := tc.note
				if note == "" {
					note = "(no note)"
				}
				t.Fatalf("role=%s %s %s: got status %d, want one of %v. Body: %s. Note: %s",
					tc.role, tc.method, tc.path, resp.StatusCode,
					tc.expectStatus, truncate(rawStr, 200), note)
			}
			if tc.note != "" {
				t.Logf("note: %s (role=%s %s %s -> %d)",
					tc.note, tc.role, tc.method, tc.path, resp.StatusCode)
			}
			// Body substring assertion for admin-enforced 403 responses (Issue #98).
			if tc.wantBodyContains != "" {
				assert.Contains(t, rawStr, tc.wantBodyContains,
					"role=%s %s %s: response body must contain %q",
					tc.role, tc.method, tc.path, tc.wantBodyContains)
			}
			assert.Less(t, resp.StatusCode, 500,
				"server must not 5xx for any matrix row (role=%s %s %s)",
				tc.role, tc.method, tc.path)
		})
	}
}

// findTestConfig reads the on-disk config.json and parses enough of it to let
// mustHaveRole() verify that both an admin and a user role are present.
func findTestConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read config at %s", path)

	// The JSON on disk uses a nested "gateway.users" array; the Config struct
	// deserializes it directly.
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config.json: %v", err)
	}
	return &cfg
}

// matrixName produces a readable t.Run name.
func matrixName(i int, tc matrixCase) string {
	path := strings.NewReplacer("/", "_").Replace(strings.TrimPrefix(tc.path, "/api/v1/"))
	return string(tc.role) + "_" + strings.ToLower(tc.method) + "_" + path + "_" + itoa3(i)
}
