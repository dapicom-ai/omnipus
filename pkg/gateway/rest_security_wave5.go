//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"net/http"

	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// rest_security_wave5.go — Wave 5 operator-facing REST endpoint (SEC-01/02/03).
//
// GET /api/v1/security/sandbox-status returns the active sandbox backend,
// its capabilities (Landlock ABI version, blocked syscalls, kernel vs fallback),
// and whether seccomp filtering is active.

// HandleSandboxStatus handles GET /api/v1/security/sandbox-status.
//
// Sprint-J: the response now includes the resolved Mode, DisabledBy, and
// Landlock/Seccomp enforcement flags so operators can distinguish enforce
// from permissive (audit-only) from off (disabled) states. FR-J-008 and
// the BDD scenario "Fresh boot applies Landlock and seccomp" both verify
// the "Apply() has not been called" note is gone after a successful wire.
func (a *restAPI) HandleSandboxStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Guard against a nil agentLoop rather than relying on method dispatch
	// safety. This matches the pattern in rest_security_wave3.go and keeps
	// the handler honest during startup windows and test harnesses.
	if a.agentLoop == nil {
		jsonErr(w, http.StatusServiceUnavailable, "sandbox: agent loop not initialized")
		return
	}
	backend := a.agentLoop.SandboxBackend()
	// Sprint-J: enrich the response with gateway-owned state (mode,
	// disabled_by, landlock_enforced, seccomp_enforced, audit_only).
	// When sandboxResult is nil (legacy path or test harness that skipped
	// applySandbox), fall back to the bare backend description — the
	// response will have the same shape but with Mode empty.
	var state sandbox.ApplyState
	var bindCount, connectCount int
	if a.sandboxResult != nil {
		state = a.sandboxResult.ApplyState
		bindCount = len(a.sandboxResult.Policy.BindPortRules)
		connectCount = len(a.sandboxResult.Policy.ConnectPortRules)
	}
	status := sandbox.DescribeBackendWithState(backend, state)
	// Wrap the status with the network-rule counts so operators can curl
	// /api/v1/security/sandbox-status and verify the bind/connect allow-list
	// is the size they expect (per cfg.Sandbox.DevServerPortRange + gateway
	// port + preview port). Counts are zero on FallbackBackend, on Mode=Off,
	// and on Landlock ABI < 4 — exactly the cases where no kernel net rules
	// were installed.
	resp := struct {
		sandbox.Status
		BindPortsCount    int `json:"bind_ports_count"`
		ConnectPortsCount int `json:"connect_ports_count"`
	}{
		Status:            status,
		BindPortsCount:    bindCount,
		ConnectPortsCount: connectCount,
	}
	jsonOK(w, resp)
}
