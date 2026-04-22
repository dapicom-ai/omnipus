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
)

// HandleSandboxAuditLog handles PUT /api/v1/security/audit-log.
//
// PUT accepts {"enabled": bool} and persists to config.sandbox.audit_log via
// safeUpdateConfigJSON. Emits a security_setting_change audit entry before
// returning. Admin-only; non-admin requests receive 403.
//
// Response shape:
//
//	{
//	  "saved":            true,
//	  "requires_restart": true,
//	  "applied_enabled":  <bool — value before this save>
//	}
//
// GET returns the current flag value:
//
//	{"enabled": bool}
func (a *restAPI) HandleSandboxAuditLog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.agentLoop.GetConfig()
		jsonOK(w, map[string]any{
			"enabled": cfg.Sandbox.AuditLog,
		})

	case http.MethodPut:
		role, _ := r.Context().Value(RoleContextKey{}).(config.UserRole)
		if role != config.UserRoleAdmin {
			jsonErr(w, http.StatusForbidden, "admin required")
			return
		}

		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.Enabled == nil {
			jsonErr(w, http.StatusBadRequest, "enabled field is required")
			return
		}

		oldEnabled := a.agentLoop.GetConfig().Sandbox.AuditLog
		newEnabled := *body.Enabled

		if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
			sandbox, _ := m["sandbox"].(map[string]any)
			if sandbox == nil {
				sandbox = map[string]any{}
				m["sandbox"] = sandbox
			}
			sandbox["audit_log"] = newEnabled
			return nil
		}); err != nil {
			slog.Error("rest: update sandbox audit_log", "error", err)
			jsonErr(w, http.StatusInternalServerError, "could not save config")
			return
		}

		_ = audit.EmitSecuritySettingChange(r.Context(), a.agentLoop.AuditLogger(), "sandbox.audit_log", oldEnabled, newEnabled)

		a.awaitReload()

		jsonOK(w, map[string]any{
			"saved":            true,
			"requires_restart": true,
			"applied_enabled":  oldEnabled,
		})

	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
