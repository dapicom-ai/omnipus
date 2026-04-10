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
	status := sandbox.DescribeBackend(backend)
	jsonOK(w, status)
}
