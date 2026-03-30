//go:build !cgo

// This test file uses //go:build !cgo so it compiles when CGO is disabled.

package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- E3: CORS tests ---

// TestIsAllowedOrigin verifies the isAllowedOrigin function for all significant cases
// from the E3 test dataset.
//
// isAllowedOrigin(reqOrigin, host, configuredOrigin) bool
//
// BDD: Given various origin combinations,
// When isAllowedOrigin is called,
// Then correct allow/deny decision is returned.
// Traces to: wave5a-wire-ui-spec.md — Scenario: CORS origin validation (E3)
func TestIsAllowedOrigin(t *testing.T) {
	tests := []struct {
		name             string
		reqOrigin        string
		host             string
		configuredOrigin string
		want             bool
	}{
		// E3 dataset row 1: evil origin with localhost configured — should be denied
		{
			name:             "evil.com with localhost configured → denied",
			reqOrigin:        "http://evil.com",
			host:             "localhost:19000",
			configuredOrigin: "http://localhost:19000",
			want:             false,
		},
		// E3 dataset row 2: localhost origin with no host/config — localhost always allowed
		{
			name:             "localhost:3000 with no config → allowed (dev loopback)",
			reqOrigin:        "http://localhost:3000",
			host:             "",
			configuredOrigin: "",
			want:             true,
		},
		// E3 dataset row 3: same-origin (IP matches host) — should be allowed
		{
			name:             "same IP origin matches host → allowed",
			reqOrigin:        "http://146.190.89.151:19000",
			host:             "146.190.89.151:19000",
			configuredOrigin: "",
			want:             true,
		},
		// E3 dataset row 4: localhost.evil.com subdomain spoofing — should be denied
		{
			name:             "localhost.evil.com subdomain → denied",
			reqOrigin:        "http://localhost.evil.com",
			host:             "localhost:19000",
			configuredOrigin: "",
			want:             false,
		},
		// E3 dataset row 5: empty origin — should be denied
		{
			name:             "empty origin → denied",
			reqOrigin:        "",
			host:             "localhost:19000",
			configuredOrigin: "",
			want:             false,
		},
		// E3 dataset row 6: 127.0.0.1 loopback — should be allowed
		{
			name:             "127.0.0.1 loopback → allowed",
			reqOrigin:        "http://127.0.0.1:19000",
			host:             "",
			configuredOrigin: "",
			want:             true,
		},
		// Additional: configured origin exact match → allowed
		{
			name:             "exact configured origin match → allowed",
			reqOrigin:        "http://localhost:19000",
			host:             "",
			configuredOrigin: "http://localhost:19000",
			want:             true,
		},
		// Additional: different port same hostname → denied (port is part of origin)
		// isAllowedOrigin now compares hostname AND port for same-host requests to
		// prevent cross-port CORS escalation (review finding #7).
		{
			name:             "different port but same hostname → denied (port mismatch)",
			reqOrigin:        "http://192.168.1.1:8080",
			host:             "192.168.1.1:9000",
			configuredOrigin: "",
			want:             false,
		},
		// Additional: malformed URL in origin → denied
		{
			name:             "malformed origin URL → denied",
			reqOrigin:        "://no-scheme-here",
			host:             "localhost:3000",
			configuredOrigin: "",
			want:             false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAllowedOrigin(tc.reqOrigin, tc.host, tc.configuredOrigin)
			assert.Equal(t, tc.want, got,
				"isAllowedOrigin(%q, %q, %q)", tc.reqOrigin, tc.host, tc.configuredOrigin)
		})
	}
}
