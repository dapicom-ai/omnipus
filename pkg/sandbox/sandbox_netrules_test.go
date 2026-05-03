// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sandbox

import "testing"

// TestDefaultPolicy_NetRulesNilByDefault verifies that callers who pass
// (nil, nil) for the network port lists get a SandboxPolicy with no
// BindPortRules / ConnectPortRules. This is the legacy default and ensures
// pre-ABI-v4 kernels remain unrestricted on the network axis.
func TestDefaultPolicy_NetRulesNilByDefault(t *testing.T) {
	policy := DefaultPolicy("/tmp/home", nil, nil, nil, nil)
	if len(policy.BindPortRules) != 0 {
		t.Errorf("BindPortRules: got %d, want 0", len(policy.BindPortRules))
	}
	if len(policy.ConnectPortRules) != 0 {
		t.Errorf("ConnectPortRules: got %d, want 0", len(policy.ConnectPortRules))
	}
}

// TestDefaultPolicy_NetRulesExpanded verifies the gateway expansion: a
// dev-server range plus gateway+preview ports become one NetPortRule per port.
// 18000-18002 is three bind ports and three connect ports; gateway+preview
// add two more connect ports for a total of five.
func TestDefaultPolicy_NetRulesExpanded(t *testing.T) {
	bindPorts := []uint16{18000, 18001, 18002}
	connectPorts := []uint16{18000, 18001, 18002, 5000, 5001}
	policy := DefaultPolicy("/tmp/home", nil, nil, bindPorts, connectPorts)

	if got, want := len(policy.BindPortRules), 3; got != want {
		t.Errorf("BindPortRules count: got %d, want %d", got, want)
	}
	if got, want := len(policy.ConnectPortRules), 5; got != want {
		t.Errorf("ConnectPortRules count: got %d, want %d", got, want)
	}
	// Order is preserved — the gateway relies on this when emitting
	// rules in the same order they appear in cfg.Sandbox.DevServerPortRange.
	for i, want := range bindPorts {
		if got := policy.BindPortRules[i].Port; got != want {
			t.Errorf("BindPortRules[%d]: got %d, want %d", i, got, want)
		}
	}
	for i, want := range connectPorts {
		if got := policy.ConnectPortRules[i].Port; got != want {
			t.Errorf("ConnectPortRules[%d]: got %d, want %d", i, got, want)
		}
	}
}

// TestDefaultPolicy_NetRulesEmptyVsNil ensures a zero-length non-nil slice
// behaves the same as nil — both leave the policy's net-rule slices empty.
// Important because callers may build a slice and then never append.
func TestDefaultPolicy_NetRulesEmptyVsNil(t *testing.T) {
	empty := []uint16{}
	policy := DefaultPolicy("/tmp/home", nil, nil, empty, empty)
	if len(policy.BindPortRules) != 0 {
		t.Errorf("BindPortRules from empty slice: got %d, want 0", len(policy.BindPortRules))
	}
	if len(policy.ConnectPortRules) != 0 {
		t.Errorf("ConnectPortRules from empty slice: got %d, want 0", len(policy.ConnectPortRules))
	}
}
