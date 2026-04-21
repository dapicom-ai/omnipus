// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

import "testing"

// TestOmnipusSandboxConfig_ResolvedMode_Precedence verifies the Sprint-J
// legacy mapping between the new Mode field and the deprecated Enabled
// bool. Mode (when set) always wins; Enabled is a fallback.
func TestOmnipusSandboxConfig_ResolvedMode_Precedence(t *testing.T) {
	cases := []struct {
		name string
		cfg  OmnipusSandboxConfig
		want string
	}{
		{"explicit enforce", OmnipusSandboxConfig{Mode: "enforce"}, "enforce"},
		{"explicit permissive", OmnipusSandboxConfig{Mode: "permissive"}, "permissive"},
		{"explicit off", OmnipusSandboxConfig{Mode: "off"}, "off"},
		{"mode wins over enabled=true", OmnipusSandboxConfig{Mode: "off", Enabled: true}, "off"},
		{"mode wins over enabled=false", OmnipusSandboxConfig{Mode: "enforce", Enabled: false}, "enforce"},
		{"legacy enabled=true", OmnipusSandboxConfig{Enabled: true}, "enforce"},
		{"legacy enabled=false", OmnipusSandboxConfig{Enabled: false}, "off"},
		{"zero value", OmnipusSandboxConfig{}, "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.ResolvedMode()
			if got != tc.want {
				t.Errorf("ResolvedMode() = %q, want %q", got, tc.want)
			}
		})
	}
}
