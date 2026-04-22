//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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

// sandboxConfigUpdate is the JSON shape accepted on PUT. Every field is a
// pointer so the handler can distinguish "operator left this unset" from
// "operator explicitly set it to zero/empty". Fields that are nil on the
// wire are left untouched on disk.
type sandboxConfigUpdate struct {
	Mode                 *string   `json:"mode,omitempty"`
	AllowNetworkOutbound *bool     `json:"allow_network_outbound,omitempty"`
	AllowedPaths         *[]string `json:"allowed_paths,omitempty"`
	SSRFEnabled          *bool     `json:"ssrf_enabled,omitempty"`
	SSRFAllowInternal    *[]string `json:"ssrf_allow_internal,omitempty"`
}

// validSandboxModes is the canonical set the UI radio group maps to. The
// string values match what sandbox.ParseMode expects (case-insensitive).
var validSandboxModes = map[string]struct{}{
	"enforce":    {},
	"permissive": {},
	"off":        {},
}

// HandleSandboxConfig handles GET/PUT /api/v1/security/sandbox-config.
//
// PUT is the sanctioned path for UI-driven sandbox edits — the generic
// PUT /api/v1/config blocks the "sandbox" top-level key on purpose (see
// rest.go:1622) because sandbox edits require process restart (FR-J-015,
// no hot-reload), and the operator needs an unambiguous UX signal that
// a restart is coming. Mixing sandbox writes into the generic config PUT
// would silently save the change and leave the operator thinking it took
// effect.
//
// Admin-only: enforced by middleware.RequireAdmin at route-mount time.
// CSRF: state-changing PUT passes through the normal double-submit gate;
// admins have the __Host-csrf (TLS) or csrf (HTTP) cookie by this point.
func (a *restAPI) HandleSandboxConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getSandboxConfig(w)
	case http.MethodPut:
		a.putSandboxConfig(w, r)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) getSandboxConfig(w http.ResponseWriter) {
	if a.agentLoop == nil {
		jsonErr(w, http.StatusServiceUnavailable, "sandbox: agent loop not initialized")
		return
	}
	cfg := a.agentLoop.GetConfig()
	applied := ""
	if a.sandboxResult != nil {
		applied = string(a.sandboxResult.ApplyState.Mode)
	}
	resp := sandboxConfigResponse{
		Mode:                 cfg.Sandbox.ResolvedMode(),
		AllowNetworkOutbound: cfg.Sandbox.AllowNetworkOutbound,
		AllowedPaths:         append([]string(nil), cfg.Sandbox.AllowedPaths...),
		SSRFEnabled:          cfg.Sandbox.SSRF.Enabled,
		SSRFAllowInternal:    append([]string(nil), cfg.Sandbox.SSRF.AllowInternal...),
		AppliedMode:          applied,
	}
	jsonOK(w, resp)
}

func (a *restAPI) putSandboxConfig(w http.ResponseWriter, r *http.Request) {
	var upd sandboxConfigUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&upd); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate mode before touching disk so a malformed value is rejected
	// atomically rather than caught halfway through the merge.
	var normalizedMode string
	if upd.Mode != nil {
		normalizedMode = strings.ToLower(strings.TrimSpace(*upd.Mode))
		if _, ok := validSandboxModes[normalizedMode]; !ok {
			jsonErr(w, http.StatusBadRequest,
				"invalid sandbox mode; must be one of: enforce, permissive, off")
			return
		}
	}

	// Merge the supplied fields into the sandbox sub-map of config.json.
	// Fields not present in the request body are left untouched — pointer-
	// null semantics gives us partial updates without the operator having
	// to resend the whole sandbox block.
	err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		sb, _ := m["sandbox"].(map[string]any)
		if sb == nil {
			sb = make(map[string]any)
			m["sandbox"] = sb
		}
		if upd.Mode != nil {
			sb["mode"] = normalizedMode
			// Clear the legacy Enabled bool when an explicit mode is set, so
			// the two fields don't disagree after the write. ResolvedMode
			// prefers Mode when present, but mismatched persisted state
			// confuses humans reading config.json.
			delete(sb, "enabled")
		}
		if upd.AllowNetworkOutbound != nil {
			sb["allow_network_outbound"] = *upd.AllowNetworkOutbound
		}
		if upd.AllowedPaths != nil {
			sb["allowed_paths"] = *upd.AllowedPaths
		}
		if upd.SSRFEnabled != nil || upd.SSRFAllowInternal != nil {
			ssrf, _ := sb["ssrf"].(map[string]any)
			if ssrf == nil {
				ssrf = make(map[string]any)
				sb["ssrf"] = ssrf
			}
			if upd.SSRFEnabled != nil {
				ssrf["enabled"] = *upd.SSRFEnabled
			}
			if upd.SSRFAllowInternal != nil {
				ssrf["allow_internal"] = *upd.SSRFAllowInternal
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("sandbox-config: write failed", "error", err)
		jsonErr(w, http.StatusInternalServerError, "could not save sandbox config")
		return
	}

	// Hot-reload is locked out for sandbox config by FR-J-015 — the change
	// lives on disk but won't take effect until restart. Return the state
	// that's actually running so the UI can display "saved 'enforce',
	// currently running 'off' — restart to apply".
	cfg := a.agentLoop.GetConfig()
	applied := ""
	if a.sandboxResult != nil {
		applied = string(a.sandboxResult.ApplyState.Mode)
	}
	jsonOK(w, sandboxConfigResponse{
		Mode:                 cfg.Sandbox.ResolvedMode(),
		AllowNetworkOutbound: cfg.Sandbox.AllowNetworkOutbound,
		AllowedPaths:         append([]string(nil), cfg.Sandbox.AllowedPaths...),
		SSRFEnabled:          cfg.Sandbox.SSRF.Enabled,
		SSRFAllowInternal:    append([]string(nil), cfg.Sandbox.SSRF.AllowInternal...),
		AppliedMode:          applied,
		RequiresRestart:      true,
	})

	// Ensure sandbox reference stays in scope; prevents goimports from
	// dropping the import since this file historically only used sandbox
	// for DescribeBackendWithState. HandleSandboxStatus still uses it.
	_ = sandbox.Status{}
}
