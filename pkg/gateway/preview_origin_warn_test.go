//go:build !cgo

// Tests for the boot-time WARN emitted when the preview listener is bound on
// 0.0.0.0/:: and gateway.preview_origin is empty (R3 in the per-agent sandbox
// plan, PR 1).

package gateway

import (
	"testing"
)

// TestShouldWarnPreviewOrigin verifies the pure predicate that guards the
// slog.Warn call in the gateway boot path. Because shouldWarnPreviewOrigin is
// a pure function with no I/O or global state, we test it directly here
// instead of spinning up the full gateway.
func TestShouldWarnPreviewOrigin(t *testing.T) {
	cases := []struct {
		name          string
		previewHost   string
		previewOrigin string
		want          bool
	}{
		// Wildcard bind + no preview_origin → WARN.
		{"0.0.0.0_no_origin", "0.0.0.0", "", true},
		{"ipv6_wildcard_no_origin", "::", "", true},

		// Loopback bind + no preview_origin → no WARN (operator is presumably
		// local-only; the URL will be reachable as localhost).
		{"loopback_no_origin", "127.0.0.1", "", false},
		{"loopback_ipv6_no_origin", "::1", "", false},

		// preview_origin set → never warn, regardless of bind address.
		{"0.0.0.0_origin_set", "0.0.0.0", "https://example.com:6061", false},
		{"loopback_origin_set", "127.0.0.1", "https://example.com:6061", false},
		{"specific_ip_origin_set", "10.0.0.5", "https://example.com:6061", false},

		// Specific public IP + no preview_origin → no WARN (the computed URL will
		// contain a reachable IP, not the literal "0.0.0.0").
		{"specific_ip_no_origin", "10.0.0.5", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldWarnPreviewOrigin(tc.previewHost, tc.previewOrigin)
			if got != tc.want {
				t.Errorf("shouldWarnPreviewOrigin(%q, %q) = %v, want %v",
					tc.previewHost, tc.previewOrigin, got, tc.want)
			}
		})
	}
}
