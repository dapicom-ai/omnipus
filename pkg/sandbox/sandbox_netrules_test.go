// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sandbox

import "testing"

// TestDefaultPolicy_NetRulesNilByDefault verifies that callers who pass
// nil for the bind-port list get a SandboxPolicy with no BindPortRules.
// This is the legacy default and ensures pre-ABI-v4 kernels remain
// unrestricted on the network axis.
func TestDefaultPolicy_NetRulesNilByDefault(t *testing.T) {
	policy := DefaultPolicy("/tmp/home", nil, nil, nil)
	if len(policy.BindPortRules) != 0 {
		t.Errorf("BindPortRules: got %d, want 0", len(policy.BindPortRules))
	}
}

// TestDefaultPolicy_NetRulesExpanded verifies the gateway expansion: a
// dev-server range becomes one NetPortRule per port in BindPortRules.
// 18000-18002 is three bind ports.
func TestDefaultPolicy_NetRulesExpanded(t *testing.T) {
	bindPorts := []uint16{18000, 18001, 18002}
	policy := DefaultPolicy("/tmp/home", nil, nil, bindPorts)

	if got, want := len(policy.BindPortRules), 3; got != want {
		t.Errorf("BindPortRules count: got %d, want %d", got, want)
	}
	// Order is preserved — the gateway relies on this when emitting
	// rules in the same order they appear in cfg.Sandbox.DevServerPortRange.
	for i, want := range bindPorts {
		if got := policy.BindPortRules[i].Port; got != want {
			t.Errorf("BindPortRules[%d]: got %d, want %d", i, got, want)
		}
	}
}

// TestDefaultPolicy_NetRulesEmptyVsNil ensures a zero-length non-nil slice
// behaves the same as nil — both leave the policy's BindPortRules empty.
// Important because callers may build a slice and then never append.
func TestDefaultPolicy_NetRulesEmptyVsNil(t *testing.T) {
	empty := []uint16{}
	policy := DefaultPolicy("/tmp/home", nil, nil, empty)
	if len(policy.BindPortRules) != 0 {
		t.Errorf("BindPortRules from empty slice: got %d, want 0", len(policy.BindPortRules))
	}
}
