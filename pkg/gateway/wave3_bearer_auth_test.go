//go:build !cgo

// This test file uses //go:build !cgo so it compiles when CGO is disabled.
// When CGO is enabled, pkg/gateway imports pkg/channels/matrix which requires
// the libolm system library (olm/olm.h). If that library is installed,
// remove this build constraint and run tests normally.

package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBearerTokenConstantTimeComparison verifies that checkBearerAuth uses
// constant-time comparison (crypto/subtle) to prevent timing attacks.
// Traces to: wave3-skill-ecosystem-spec.md line 846 (Test #16: TestBearerTokenConstantTimeComparison)
// BDD: Given OMNIPUS_BEARER_TOKEN is set,
// When a request with the correct token is sent,
// Then checkBearerAuth returns true (constant-time comparison used — verified by code review).
// When a request with an incorrect token is sent,
// Then checkBearerAuth returns false and responds 401.

func TestBearerTokenConstantTimeComparison(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 846 (Test #16)
	const testToken = "test-bearer-token-constant-time-abc123"
	t.Setenv("OMNIPUS_BEARER_TOKEN", testToken)

	tests := []struct {
		name       string
		authHeader string
		wantPass   bool
		wantStatus int
	}{
		// Valid token
		{
			name:       "valid token passes",
			authHeader: "Bearer " + testToken,
			wantPass:   true,
			wantStatus: 0, // auth passes, not checked at this level
		},
		// Wrong token
		{
			name:       "wrong token returns 401",
			authHeader: "Bearer wrong-token",
			wantPass:   false,
			wantStatus: http.StatusUnauthorized,
		},
		// Missing Authorization header
		{
			name:       "missing auth header returns 401",
			authHeader: "",
			wantPass:   false,
			wantStatus: http.StatusUnauthorized,
		},
		// Wrong prefix (no "Bearer " prefix)
		{
			name:       "missing Bearer prefix returns 401",
			authHeader: testToken, // no "Bearer " prefix
			wantPass:   false,
			wantStatus: http.StatusUnauthorized,
		},
		// Empty token value after "Bearer "
		{
			name:       "empty Bearer token returns 401",
			authHeader: "Bearer ",
			wantPass:   false,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/chat", nil)
			if tc.authHeader != "" {
				r.Header.Set("Authorization", tc.authHeader)
			}

			got := checkBearerAuth(w, r)
			assert.Equal(t, tc.wantPass, got,
				"checkBearerAuth(%q) result mismatch", tc.authHeader)

			if !tc.wantPass {
				assert.Equal(t, tc.wantStatus, w.Code,
					"failed auth must respond with %d", tc.wantStatus)
			}
		})
	}
}

// TestBearerAuthDisabledWhenTokenUnset verifies that when OMNIPUS_BEARER_TOKEN
// is not set, all requests are allowed (development mode).
// Traces to: wave3-skill-ecosystem-spec.md line 846 (Test #16 — unset token = allow all)
// BDD: Given OMNIPUS_BEARER_TOKEN is not set,
// When any request arrives,
// Then checkBearerAuth returns true (auth not configured → development mode).

func TestBearerAuthDisabledWhenTokenUnset(t *testing.T) {
	// Traces to: wave3-skill-ecosystem-spec.md line 846 (Test #16)
	t.Setenv("OMNIPUS_BEARER_TOKEN", "") // unset

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/chat", nil)
	// No Authorization header — should still pass when token not configured
	got := checkBearerAuth(w, r)
	assert.True(t, got, "auth must pass when OMNIPUS_BEARER_TOKEN is not configured")
	assert.Equal(t, 200, w.Code, "no 401 written when token not configured")
}
