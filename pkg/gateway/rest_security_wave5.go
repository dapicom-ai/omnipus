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
	if a.sandboxResult != nil {
		state = a.sandboxResult.ApplyState
	}
	status := sandbox.DescribeBackendWithState(backend, state)
	jsonOK(w, status)
}

// sandboxConfigResponse is the JSON shape surfaced on GET/PUT
// /api/v1/security/sandbox-config. It mirrors the subset of
// config.OmnipusSandboxConfig that operators are allowed to edit through the
// UI. Other sandbox-adjacent fields (prompt-injection tier, rate limits,
// tool policies) have their own editors elsewhere in Settings → Security
// and are intentionally NOT exposed here — this endpoint is scoped to the
// sandbox/SSRF surface the Settings → Security → Process Sandbox panel
// renders.
type sandboxConfigResponse struct {
	// Current sandbox config as persisted in config.json.
	Mode                 string   `json:"mode"`
	AllowNetworkOutbound bool     `json:"allow_network_outbound"`
	AllowedPaths         []string `json:"allowed_paths"`
	SSRFEnabled          bool     `json:"ssrf_enabled"`
	SSRFAllowInternal    []string `json:"ssrf_allow_internal"`

	// AppliedMode reflects what the gateway is ACTUALLY running with. It can
	// differ from Mode above when the operator saved a new mode but hasn't
	// restarted yet (Sprint J FR-J-015 locks out hot-reload for sandbox).
	AppliedMode string `json:"applied_mode"`

	// RequiresRestart is true after a successful PUT — the UI renders a
	// persistent banner telling the operator to restart the gateway for
	// the change to take effect.
	RequiresRestart bool `json:"requires_restart,omitempty"`
}

// Ensure sandbox import stays in scope; HandleSandboxStatus uses it.
// HandleSandboxConfig, getSandboxConfig, and putSandboxConfig are declared
// in rest_sandbox_config.go (Sprint K + PR #137 merge).
var _ = sandbox.Status{}
