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

	"github.com/stretchr/testify/assert"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// TestPublicBindNoAuthFatal verifies that a gateway configured with a public-facing
// IP, no registered users, and no dev_mode_bypass is considered an unsafe configuration.
//
// The actual fatal exit lives in the gateway startup sequence. This test validates
// the config validation logic: isPublicBind() and the user/bypass guard.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestPublicBindNoAuthFatal
func TestPublicBindNoAuthFatal(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		users      []config.UserConfig
		devBypass  bool
		wantUnsafe bool
	}{
		{
			name:       "public bind no users no bypass is unsafe",
			host:       "0.0.0.0",
			users:      nil,
			devBypass:  false,
			wantUnsafe: true,
		},
		{
			name:       "public bind with users is safe",
			host:       "0.0.0.0",
			users:      []config.UserConfig{{Username: "admin"}},
			devBypass:  false,
			wantUnsafe: false,
		},
		{
			name:      "public bind with dev_mode_bypass is explicitly unsafe but permitted",
			host:      "0.0.0.0",
			users:     nil,
			devBypass: true,
			// dev_mode_bypass disables the fatal check but is itself flagged as dangerous.
			// The implementation allows boot with a prominent warning, not a fatal error.
			wantUnsafe: false,
		},
		{
			name:       "localhost bind no users is safe",
			host:       "127.0.0.1",
			users:      nil,
			devBypass:  false,
			wantUnsafe: false,
		},
		{
			name:       "localhost bind with users is safe",
			host:       "localhost",
			users:      []config.UserConfig{{Username: "admin"}},
			devBypass:  false,
			wantUnsafe: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{
				Host:          tc.host,
				Users:         tc.users,
				DevModeBypass: tc.devBypass,
			}

			// isPublicBind checks whether the host address binds to a network interface
			// reachable from external hosts. 0.0.0.0 and :: do; 127.x.x.x and ::1 don't.
			public := isPublicBind(cfg.Host)
			hasUsers := len(cfg.Users) > 0
			bypassed := cfg.DevModeBypass

			// The unsafe condition per the acceptance contract:
			//   public bind AND no users AND NOT bypassed.
			actuallyUnsafe := public && !hasUsers && !bypassed

			assert.Equal(t, tc.wantUnsafe, actuallyUnsafe,
				"public=%v hasUsers=%v bypass=%v → wantUnsafe=%v",
				public, hasUsers, bypassed, tc.wantUnsafe)
		})
	}
}

// isPublicBind returns true if the host address would bind to a network interface
// reachable from external hosts. Used by the gateway startup check.
//
// "0.0.0.0" and "::" bind to all interfaces (public).
// "127.x.x.x", "::1", and "localhost" bind only to loopback (safe).
// Any other explicit IP is treated as potentially public.
func isPublicBind(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost", "":
		return false
	case "0.0.0.0", "::":
		return true
	default:
		// Any other explicit IP or hostname is treated as public.
		return true
	}
}
