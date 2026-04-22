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
// PUT /api/v1/security/sandbox-config. Both fields are pointer-slices so
// we can distinguish "omitted" (nil) from "explicitly set to empty list"
// (non-nil, len 0). Only fields present in the request body are touched
// on disk; untouched fields retain their existing values.
type sandboxConfigPutBody struct {
	AllowedPaths *[]string                   `json:"allowed_paths,omitempty"`
	SSRF         *sandboxConfigPutBodySSRF   `json:"ssrf,omitempty"`
}

// sandboxConfigPutBodySSRF carries the ssrf sub-object. We intentionally
// only expose allow_internal here — inventing new config keys would break
// backward compatibility. Any other ssrf field the client sends is ignored.
type sandboxConfigPutBodySSRF struct {
	AllowInternal *[]string `json:"allow_internal,omitempty"`
}

// HandleSandboxConfig handles GET/PUT /api/v1/security/sandbox-config.
//
// GET returns:
//
//	{
//	  "mode":          string,
//	  "allowed_paths": []string,
//	  "ssrf": {
//	    "enabled":        bool,
//	    "allow_internal": []string
//	  }
//	}
//
// PUT accepts a partial body — any subset of {allowed_paths, ssrf.allow_internal}.
// On validation success each changed field is persisted atomically via
// safeUpdateConfigJSON; the response reports requires_restart=true iff
// allowed_paths was in the body (restart-gated per RestartGatedKeys).
// ssrf.allow_internal is hot-reload.
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
	cfg := a.agentLoop.GetConfig()

	allowedPaths := cfg.Sandbox.AllowedPaths
	if allowedPaths == nil {
		allowedPaths = []string{}
	}
	allowInternal := cfg.Sandbox.SSRF.AllowInternal
	if allowInternal == nil {
		allowInternal = []string{}
	}

	jsonOK(w, map[string]any{
		"mode":          cfg.Sandbox.ResolvedMode(),
		"allowed_paths": allowedPaths,
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

	changedAllowedPaths := body.AllowedPaths != nil
	changedAllowInternal := body.SSRF != nil && body.SSRF.AllowInternal != nil

	if !changedAllowedPaths && !changedAllowInternal {
		jsonErr(w, http.StatusBadRequest, "no recognized fields in body — expected allowed_paths or ssrf.allow_internal")
		return
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
		warnings, err := validateSSRFAllowInternal(*body.SSRF.AllowInternal)
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
		oldAllowedPaths  []string
		oldAllowInternal []string
	)

	if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		sandbox, _ := m["sandbox"].(map[string]any)
		if sandbox == nil {
			sandbox = map[string]any{}
			m["sandbox"] = sandbox
		}

		// Snapshot old values from the just-read map so the audit diff is
		// consistent with the actual atomic state, not a pre-lock race copy.
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
		if changedAllowInternal {
			ssrf, _ := sandbox["ssrf"].(map[string]any)
			if ssrf == nil {
				ssrf = map[string]any{}
				sandbox["ssrf"] = ssrf
			}
			if raw, ok := ssrf["allow_internal"].([]any); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						oldAllowInternal = append(oldAllowInternal, s)
					}
				}
			}
			ssrf["allow_internal"] = toAnySlice(*body.SSRF.AllowInternal)
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
					oldAllowInternal, *body.SSRF.AllowInternal,
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
		for _, entry := range ssrfWarnings {
			slog.Warn("ssrf: wildcard allow_internal accepted",
				"event", "ssrf_wildcard_accepted",
				"entry", entry,
				"actor", actor)
		}
	}

	jsonOK(w, map[string]any{
		"saved":            true,
		"requires_restart": changedAllowedPaths,
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
