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

// HandleToolPolicies handles GET/PUT /api/v1/security/tool-policies.
//
// GET returns the current global tool policy configuration:
//
//	{
//	  "default_policy": "allow",
//	  "policies": {"exec": "ask", "browser.evaluate": "deny"}
//	}
//
// PUT accepts the same format, validates all policy values, and persists to
// config.json under sandbox.tool_policies and sandbox.default_tool_policy via
// safeUpdateConfigJSON (preserves credential refs). Changes are audit-logged
// per SEC-15. The new policy is reflected immediately after the config reload
// triggered by safeUpdateConfigJSON.
//
// Valid policy values: "allow", "ask", "deny".
func (a *restAPI) HandleToolPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.agentLoop.GetConfig()
		defaultPolicy := cfg.Sandbox.DefaultToolPolicy
		if defaultPolicy == "" {
			defaultPolicy = "allow"
		}
		// Return a non-nil map even when empty so the frontend always gets a
		// consistent object shape rather than null.
		policies := cfg.Sandbox.ToolPolicies
		if policies == nil {
			policies = map[string]string{}
		}
		jsonOK(w, map[string]any{
			"default_policy": defaultPolicy,
			"policies":       policies,
		})

	case http.MethodPut:
		var body struct {
			DefaultPolicy string            `json:"default_policy"`
			Policies      map[string]string `json:"policies"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Validate default_policy.
		switch body.DefaultPolicy {
		case "", "allow", "ask", "deny":
			// valid; empty string is treated as "allow"
		default:
			jsonErr(w, http.StatusBadRequest, "default_policy must be 'allow', 'ask', or 'deny'")
			return
		}
		if body.DefaultPolicy == "" {
			body.DefaultPolicy = "allow"
		}

		// Validate per-tool policies.
		for toolName, p := range body.Policies {
			switch p {
			case "allow", "ask", "deny":
				// valid
			default:
				jsonErr(w, http.StatusBadRequest,
					"policies["+toolName+"]: value must be 'allow', 'ask', or 'deny'")
				return
			}
		}

		// Persist to config.json under the sandbox key, preserving all other fields.
		if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
			sandbox, _ := m["sandbox"].(map[string]any)
			if sandbox == nil {
				sandbox = map[string]any{}
				m["sandbox"] = sandbox
			}
			sandbox["default_tool_policy"] = body.DefaultPolicy
			if body.Policies != nil {
				sandbox["tool_policies"] = body.Policies
			} else {
				// Explicit null from the client clears the map.
				sandbox["tool_policies"] = map[string]any{}
			}
			return nil
		}); err != nil {
			slog.Error("rest: update tool policies", "error", err)
			jsonErr(w, http.StatusInternalServerError, "could not save config")
			return
		}

		// SEC-15: audit-log the policy change.
		if a.agentLoop != nil {
			if auditLogger := a.agentLoop.AuditLogger(); auditLogger != nil {
				if err := auditLogger.Log(&audit.Entry{
					Event:    audit.EventPolicyEval,
					Decision: audit.DecisionAllow,
					Details: map[string]any{
						"action":         "tool_policies_update",
						"default_policy": body.DefaultPolicy,
						"policy_count":   len(body.Policies),
					},
				}); err != nil {
					slog.Warn("rest: audit log tool policies update", "error", err)
				}
			}
		}

		slog.Info("rest: global tool policies updated",
			"default_policy", body.DefaultPolicy,
			"policy_count", len(body.Policies),
		)

		// Return the persisted state. Changes take effect immediately because
		// safeUpdateConfigJSON hot-reloads the in-memory config after the write.
		jsonOK(w, map[string]any{
			"default_policy": body.DefaultPolicy,
			"policies":       body.Policies,
		})

	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
