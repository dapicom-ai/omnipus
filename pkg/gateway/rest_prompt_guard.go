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
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
)

// HandlePromptGuard handles GET/PUT /api/v1/security/prompt-guard.
//
// GET returns the current prompt-injection level (SEC-25):
//
//	{"level": "low"|"medium"|"high", "requires_restart": false}
//
// PUT accepts {"level": "low"|"medium"|"high"} (case-sensitive), persists to
// config.sandbox.prompt_injection_level via safeUpdateConfigJSON, triggers a
// hot-reload via awaitReload, and emits a security_setting_change audit entry.
// Changes take effect immediately — requires_restart is always false (FR-004).
// PUT is admin-only; non-admin requests receive 403.
func (a *restAPI) HandlePromptGuard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.agentLoop.GetConfig()
		level := cfg.Sandbox.PromptInjectionLevel
		if level == "" {
			level = "medium"
		}
		jsonOK(w, map[string]any{
			"level":            level,
			"requires_restart": false,
		})

	case http.MethodPut:
		a.adminPromptGuardOnce.Do(func() {
			a.adminPromptGuardHandler = middleware.RequireAdmin(
				http.HandlerFunc(a.putPromptGuard),
			)
		})
		a.adminPromptGuardHandler.ServeHTTP(w, r)

	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// putPromptGuard is the admin-only body of PUT /api/v1/security/prompt-guard.
// It is called only after RequireAdmin has confirmed the caller holds admin role.
func (a *restAPI) putPromptGuard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var body struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	switch body.Level {
	case "low", "medium", "high":
	default:
		jsonErr(w, http.StatusBadRequest, `level must be one of: "low", "medium", "high"`)
		return
	}

	oldLevel := a.agentLoop.GetConfig().Sandbox.PromptInjectionLevel
	if oldLevel == "" {
		oldLevel = "medium"
	}

	if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		sandbox, _ := m["sandbox"].(map[string]any)
		if sandbox == nil {
			sandbox = map[string]any{}
			m["sandbox"] = sandbox
		}
		sandbox["prompt_injection_level"] = body.Level
		return nil
	}); err != nil {
		slog.Error("rest: update prompt_injection_level", "error", err)
		jsonErr(w, http.StatusInternalServerError, "could not save config")
		return
	}

	_ = audit.EmitSecuritySettingChange(r.Context(), a.agentLoop.AuditLogger(),
		"sandbox.prompt_injection_level", oldLevel, body.Level)

	a.awaitReload()

	slog.Info("rest: prompt guard level updated", "level", body.Level)

	jsonOK(w, map[string]any{
		"saved":            true,
		"requires_restart": false,
		"applied_level":    body.Level,
	})
}
