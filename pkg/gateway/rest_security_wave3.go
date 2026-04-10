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
)

// rest_security_wave3.go — Wave 3 operator-facing REST endpoints.
//
// This file wires two endpoints:
//   - GET  /api/v1/security/exec-proxy-status   (SEC-28 lifecycle inspection)
//   - GET/PUT /api/v1/security/prompt-guard      (SEC-25 strictness control)
//
// Both endpoints follow the Wave 2 pattern established by
// HandleExecAllowlist: GET is a direct config read, PUT mutates config.json
// via safeUpdateConfigJSON so credential refs elsewhere in the file survive
// the rewrite, and every mutation is audit-logged regardless of whether the
// caller was authenticated. `restart_required: true` tells the UI the change
// is not yet live because security-critical config is loaded once at startup
// per SEC-12.

// HandleExecProxyStatus handles GET /api/v1/security/exec-proxy-status.
//
// Returns the runtime state of the exec SSRF proxy (SEC-28). Operators need
// this to distinguish "proxy disabled by config" from "proxy failed to bind"
// from "proxy running normally", which directly affects the threat model the
// environment is actually enforcing.
//
// Response shape:
//
//	{
//	  "enabled": bool,   // cfg.Tools.Exec.EnableProxy
//	  "running": bool,   // listener currently bound
//	  "address": string, // "127.0.0.1:PORT" when running, omitted otherwise
//	}
func (a *restAPI) HandleExecProxyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := a.agentLoop.GetConfig()
	enabled := cfg.Tools.Exec.EnableProxy

	proxy := a.agentLoop.ExecProxy()
	if proxy == nil {
		// Proxy disabled OR failed to bind at startup. The enabled flag
		// distinguishes the two cases for the UI.
		jsonOK(w, map[string]any{
			"enabled": enabled,
			"running": false,
		})
		return
	}

	jsonOK(w, map[string]any{
		"enabled": enabled,
		"running": true,
		"address": proxy.Addr(),
	})
}

// HandlePromptGuard handles GET/PUT /api/v1/security/prompt-guard.
//
// GET returns the current strictness level (SEC-25). PUT accepts
// {"strictness": "low"|"medium"|"high"} and persists to config.json. The
// runtime guard is NOT hot-reloaded — the updated value takes effect on next
// agent-loop restart per SEC-12, so PUT responses always include
// restart_required: true.
func (a *restAPI) HandlePromptGuard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.agentLoop.GetConfig()
		level := cfg.Sandbox.PromptInjectionLevel
		if level == "" {
			level = "medium"
		}
		jsonOK(w, map[string]any{
			"strictness":       level,
			"restart_required": false,
		})
	case http.MethodPut:
		var body struct {
			Strictness string `json:"strictness"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		// Validate against the closed set of strictness levels. Unknown
		// values must fail closed with 400, not silently default — config
		// fixups at this layer would hide operator misconfiguration.
		switch body.Strictness {
		case "low", "medium", "high":
		default:
			jsonErr(w, http.StatusBadRequest, "strictness must be 'low', 'medium', or 'high'")
			return
		}
		if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
			sandbox, _ := m["sandbox"].(map[string]any)
			if sandbox == nil {
				sandbox = map[string]any{}
				m["sandbox"] = sandbox
			}
			sandbox["prompt_injection_level"] = body.Strictness
			return nil
		}); err != nil {
			slog.Error("rest: update prompt guard strictness", "error", err)
			jsonErr(w, http.StatusInternalServerError, "could not save config")
			return
		}

		// SEC-15: Audit-log the policy change. The prompt guard is a
		// security-critical config; every mutation must be recorded even
		// when audit logging itself failed to initialize at startup.
		if a.agentLoop != nil {
			if auditLogger := a.agentLoop.AuditLogger(); auditLogger != nil {
				if err := auditLogger.Log(&audit.Entry{
					Event:    audit.EventPolicyEval,
					Decision: audit.DecisionAllow,
					Details: map[string]any{
						"action":     "prompt_guard_update",
						"strictness": body.Strictness,
					},
				}); err != nil {
					slog.Warn("rest: audit log prompt guard update", "error", err)
				}
			}
		}

		slog.Info("rest: prompt guard strictness updated", "strictness", body.Strictness)

		jsonOK(w, map[string]any{
			"strictness":       body.Strictness,
			"restart_required": true,
		})
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
