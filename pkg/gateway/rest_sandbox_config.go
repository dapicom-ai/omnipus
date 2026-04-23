//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
)

// sandboxConfigPutBody mirrors the partial-update contract for
// PUT /api/v1/security/sandbox-config. Pointer types allow us to distinguish
// "omitted" (nil) from "explicitly set to empty list/string" (non-nil).
// Only fields present in the request body are touched on disk; untouched
// fields retain their existing values.
//
// Accepts both flat fields (ssrf_enabled, ssrf_allow_internal,
// allow_network_outbound) and nested ssrf object (ssrf.allow_internal).
// Flat fields take precedence when both are present.
type sandboxConfigPutBody struct {
	Mode                 *string                   `json:"mode,omitempty"`
	AllowNetworkOutbound *bool                     `json:"allow_network_outbound,omitempty"`
	AllowedPaths         *[]string                 `json:"allowed_paths,omitempty"`
	SSRFEnabled          *bool                     `json:"ssrf_enabled,omitempty"`
	SSRFAllowInternal    *[]string                 `json:"ssrf_allow_internal,omitempty"`
	SSRF                 *sandboxConfigPutBodySSRF `json:"ssrf,omitempty"`
}

// sandboxConfigPutBodySSRF carries the nested ssrf sub-object for
// clients that send ssrf.allow_internal. Flat ssrf_allow_internal takes
// precedence when both are present in the same request.
type sandboxConfigPutBodySSRF struct {
	AllowInternal *[]string `json:"allow_internal,omitempty"`
}

// validSandboxModes is the canonical set accepted by putSandboxConfig.
var validSandboxModes = map[string]bool{
	"off":        true,
	"permissive": true,
	"enforce":    true,
}

// HandleSandboxConfig handles GET/PUT /api/v1/security/sandbox-config.
//
// GET returns the full sandbox config. See pkg/gateway/rest_sandbox_config_test.go
// for the exact response and request shapes.
//
// PUT accepts a partial body — any subset of
// {mode, allow_network_outbound, allowed_paths, ssrf_enabled,
// ssrf_allow_internal, ssrf.allow_internal}. On validation success each
// changed field is persisted atomically via safeUpdateConfigJSON.
// mode and allowed_paths are restart-gated (requires_restart=true).
// ssrf.allow_internal is hot-reload (requires_restart=false).
//
// Admin-only: non-admin PUT returns 403.
func (a *restAPI) HandleSandboxConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getSandboxConfig(w, r)
	case http.MethodPut:
		a.putSandboxConfig(w, r)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) getSandboxConfig(w http.ResponseWriter, r *http.Request) {
	if a.agentLoop == nil {
		jsonErr(w, http.StatusServiceUnavailable, "sandbox: agent loop not initialized")
		return
	}
	cfg := a.agentLoop.GetConfig()

	allowedPaths := cfg.Sandbox.AllowedPaths
	if allowedPaths == nil {
		allowedPaths = []string{}
	}
	allowInternal := cfg.Sandbox.SSRF.AllowInternal
	if allowInternal == nil {
		allowInternal = []string{}
	}

	// applied_mode reflects what the gateway is ACTUALLY running with. It
	// differs from mode when the operator saved a change but hasn't restarted.
	applied := ""
	if a.sandboxResult != nil {
		applied = string(a.sandboxResult.ApplyState.Mode)
	}

	// Return both the flat-field shape and the nested ssrf object.
	// The flat fields are the canonical wire format; the nested ssrf block is
	// included for backward-compatible clients. Both are safe to include — JSON
	// consumers pick what they need.
	jsonOK(w, map[string]any{
		"mode":                   cfg.Sandbox.ResolvedMode(),
		"allow_network_outbound": cfg.Sandbox.AllowNetworkOutbound,
		"allowed_paths":          allowedPaths,
		"ssrf_enabled":           cfg.Sandbox.SSRF.Enabled,
		"ssrf_allow_internal":    allowInternal,
		"applied_mode":           applied,
		// Nested ssrf object for backward-compatible clients.
		"ssrf": map[string]any{
			"enabled":        cfg.Sandbox.SSRF.Enabled,
			"allow_internal": allowInternal,
		},
	})
}

func (a *restAPI) putSandboxConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var body sandboxConfigPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Resolve which fields are being updated. Flat ssrf_allow_internal takes
	// precedence over nested ssrf.allow_internal when both are present.
	changedMode := body.Mode != nil
	changedAllowNetworkOutbound := body.AllowNetworkOutbound != nil
	changedAllowedPaths := body.AllowedPaths != nil
	changedSSRFEnabled := body.SSRFEnabled != nil

	// Resolve allow_internal source: flat field takes precedence over nested.
	var resolvedAllowInternal *[]string
	if body.SSRFAllowInternal != nil {
		resolvedAllowInternal = body.SSRFAllowInternal
	} else if body.SSRF != nil && body.SSRF.AllowInternal != nil {
		resolvedAllowInternal = body.SSRF.AllowInternal
	}
	changedAllowInternal := resolvedAllowInternal != nil

	if !changedMode && !changedAllowNetworkOutbound && !changedAllowedPaths &&
		!changedSSRFEnabled && !changedAllowInternal {
		jsonErr(
			w,
			http.StatusBadRequest,
			"at least one field required — expected mode, allowed_paths, or ssrf.allow_internal",
		)
		return
	}

	// Validate mode value before any disk writes.
	if changedMode {
		if !validSandboxModes[*body.Mode] {
			jsonErr(w, http.StatusBadRequest, `invalid sandbox mode — must be one of "off", "permissive", "enforce"`)
			return
		}
	}

	// Strict validation — one bad entry fails the whole PUT, nothing persists.
	if changedAllowedPaths {
		if err := validateAllowedPaths(*body.AllowedPaths); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var ssrfWarnings []string
	if changedAllowInternal {
		warnings, err := validateSSRFAllowInternal(*resolvedAllowInternal)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		ssrfWarnings = warnings
	}

	// Capture old values for auditing inside the safeUpdateConfigJSON callback
	// so the snapshot is taken atomically with the write. Reading before the
	// lock can yield a stale value when two writers race.
	var (
		oldMode          string
		oldAllowedPaths  []string
		oldAllowInternal []string
	)

	if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		sandbox := ensureMap(m, "sandbox")

		// Snapshot old values from the just-read map so the audit diff is
		// consistent with the actual atomic state, not a pre-lock race copy.
		if changedMode {
			if s, ok := sandbox["mode"].(string); ok {
				oldMode = s
			}
			sandbox["mode"] = *body.Mode
			// Clear the legacy Enabled bool when an explicit mode is set, so
			// ResolvedMode() and humans reading config.json agree.
			delete(sandbox, "enabled")
		}
		if changedAllowNetworkOutbound {
			sandbox["allow_network_outbound"] = *body.AllowNetworkOutbound
		}
		if changedAllowedPaths {
			if raw, ok := sandbox["allowed_paths"].([]any); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						oldAllowedPaths = append(oldAllowedPaths, s)
					}
				}
			}
			sandbox["allowed_paths"] = toAnySlice(*body.AllowedPaths)
		}
		if changedSSRFEnabled || changedAllowInternal {
			ssrf := ensureMap(m, "sandbox", "ssrf")
			if changedSSRFEnabled {
				ssrf["enabled"] = *body.SSRFEnabled
			}
			if changedAllowInternal {
				if raw, ok := ssrf["allow_internal"].([]any); ok {
					for _, v := range raw {
						if s, ok := v.(string); ok {
							oldAllowInternal = append(oldAllowInternal, s)
						}
					}
				}
				ssrf["allow_internal"] = toAnySlice(*resolvedAllowInternal)
			}
		}
		return nil
	}); err != nil {
		slog.Error("rest: update sandbox config", "error", err)
		jsonErr(w, http.StatusInternalServerError, "could not save config")
		return
	}

	// Audit each changed field. Errors are logged, never surface to the
	// caller — the mutation has already been persisted atomically.
	if a.agentLoop != nil {
		if auditLogger := a.agentLoop.AuditLogger(); auditLogger != nil {
			if changedMode {
				if err := audit.EmitSecuritySettingChange(
					r.Context(), auditLogger,
					"sandbox.mode",
					oldMode, *body.Mode,
				); err != nil {
					slog.Error("rest: audit emit sandbox.mode change", "error", err)
				}
			}
			if changedAllowedPaths {
				if err := audit.EmitSecuritySettingChange(
					r.Context(), auditLogger,
					"sandbox.allowed_paths",
					oldAllowedPaths, *body.AllowedPaths,
				); err != nil {
					slog.Error("rest: audit emit allowed_paths change", "error", err)
				}
			}
			if changedAllowInternal {
				if err := audit.EmitSecuritySettingChange(
					r.Context(), auditLogger,
					"sandbox.ssrf.allow_internal",
					oldAllowInternal, *resolvedAllowInternal,
				); err != nil {
					slog.Error("rest: audit emit ssrf.allow_internal change", "error", err)
				}
			}
		}
	}

	// Wildcard SSRF entries (0.0.0.0/0, ::/0) validate successfully but
	// effectively disable internal-block protection. Log each one with
	// the actor username so security review catches the divergence.
	if len(ssrfWarnings) > 0 {
		actor := actorUsername(r)
		for _, warn := range ssrfWarnings {
			slog.Warn("ssrf: wildcard allow_internal accepted",
				"event", "ssrf_wildcard_accepted",
				"entry", warn,
				"actor", actor)
		}
	}

	// mode and allowed_paths are restart-gated (sandbox applied once at boot).
	// ssrf.allow_internal is hot-reload via config poll.
	partialRestartRequired := changedMode || changedAllowedPaths

	// Return the updated config so the UI can cache-update without a follow-up GET.
	// Include both flat fields and nested ssrf object for backward-compatible clients.
	if a.agentLoop != nil {
		cfg := a.agentLoop.GetConfig()
		allowedPaths := cfg.Sandbox.AllowedPaths
		if allowedPaths == nil {
			allowedPaths = []string{}
		}
		allowInternal := cfg.Sandbox.SSRF.AllowInternal
		if allowInternal == nil {
			allowInternal = []string{}
		}
		applied := ""
		if a.sandboxResult != nil {
			applied = string(a.sandboxResult.ApplyState.Mode)
		}
		jsonOK(w, map[string]any{
			"saved":                  true,
			"mode":                   cfg.Sandbox.ResolvedMode(),
			"allow_network_outbound": cfg.Sandbox.AllowNetworkOutbound,
			"allowed_paths":          allowedPaths,
			"ssrf_enabled":           cfg.Sandbox.SSRF.Enabled,
			"ssrf_allow_internal":    allowInternal,
			"applied_mode":           applied,
			"requires_restart":       partialRestartRequired,
			"ssrf": map[string]any{
				"enabled":        cfg.Sandbox.SSRF.Enabled,
				"allow_internal": allowInternal,
			},
		})
		return
	}

	// Fallback when agentLoop is nil (test harness or startup race).
	jsonOK(w, map[string]any{
		"saved":            true,
		"requires_restart": partialRestartRequired,
	})
}

// toAnySlice converts []string to []any so the JSON map round-trip
// through safeUpdateConfigJSON produces a stable shape (map[string]any
// parse would turn a []string marshal into []any anyway — doing it
// here keeps the mutation function honest).
func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// actorUsername pulls the authenticated username from the request's
// context, falling back to "" when no user is attached (system-initiated
// or test request). Mirrors the extractor in pkg/audit but lives here so
// the gateway does not need to import audit internals.
func actorUsername(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := r.Context().Value(ctxkey.UserContextKey{})
	if v == nil {
		return ""
	}
	if u, ok := v.(*config.UserConfig); ok && u != nil {
		return u.Username
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
