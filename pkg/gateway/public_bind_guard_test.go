//go:build !cgo

// Contract test: Plan 3 §1 acceptance decision — binding to a public IP with no
// users and no dev_mode_bypass must cause a fatal boot error.
//
// BDD: Given gateway config with host=0.0.0.0, no users, dev_mode_bypass=false,
//
//	When the gateway validates its boot config, Then it must return a fatal error.
//
// Acceptance decision: Plan 3 §1 "Public-IP binding with no users and no dev_mode_bypass: fatal at boot"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/gateway/public_bind_guard_test.go

package gateway

import (
	"testing"
)

// TestPublicBindNoAuthFatal verifies that a gateway configured with a public-facing
// IP, no registered users, and no dev_mode_bypass is considered an unsafe configuration.
//
// The production guard (RunContext refusing to start) is not yet implemented —
// this test is honest-skipped rather than testing a local helper that reimplements
// production logic without calling it.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestPublicBindNoAuthFatal
func TestPublicBindNoAuthFatal(t *testing.T) {
	t.Skip(
		"production guard not yet implemented; tracked as plan §1 — " +
			"requires follow-up in PR-D security: RunContext must reject " +
			"host=0.0.0.0 + no users + no dev_mode_bypass at boot",
	)
}
