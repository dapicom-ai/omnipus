//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
)

// rest_rate_limits.go — rate-limits endpoint.
//
// GET  /api/v1/security/rate-limits — returns current config + live daily cost.
// PUT  /api/v1/security/rate-limits — partial update to config.sandbox.rate_limits.
//
// PUT is admin-only. Strict type validation rejects JSON strings in numeric
// fields, floats in integer fields, negative values, NaN/Inf, and overflow.
// Changes are hot-reloaded (requires_restart: false) via the 2-second config
// poll in the agent loop.

// HandleRateLimits handles GET and PUT /api/v1/security/rate-limits.
func (a *restAPI) HandleRateLimits(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getRateLimits(w, r)
	case http.MethodPut:
		middleware.RequireAdmin(http.HandlerFunc(a.putRateLimits)).ServeHTTP(w, r)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// getRateLimits returns the current rate-limit config and live daily cost.
func (a *restAPI) getRateLimits(w http.ResponseWriter, r *http.Request) {
	rlCfg := a.agentLoop.GetConfig().Sandbox.RateLimits
	enabled := rlCfg.DailyCostCapUSD > 0 ||
		rlCfg.MaxAgentLLMCallsPerHour > 0 ||
		rlCfg.MaxAgentToolCallsPerMinute > 0

	var dailyCost float64
	if registry := a.agentLoop.RateLimiter(); registry != nil {
		dailyCost = registry.GetDailyCost()
	} else {
		enabled = false
	}

	jsonOK(w, map[string]any{
		"enabled":                         enabled,
		"daily_cost_usd":                  dailyCost,
		"daily_cost_cap":                  rlCfg.DailyCostCapUSD,
		"max_agent_llm_calls_per_hour":    rlCfg.MaxAgentLLMCallsPerHour,
		"max_agent_tool_calls_per_minute": rlCfg.MaxAgentToolCallsPerMinute,
	})
}

// putRateLimits is the admin-only body of PUT /api/v1/security/rate-limits.
// Called only after RequireAdmin has confirmed the caller holds admin role.
func (a *restAPI) putRateLimits(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Decode to map[string]json.RawMessage so we can inspect each field's
	// JSON token type independently and reject type mismatches strictly.
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Parse and validate each present field.
	var newCap *float64
	var newLLM, newTool *int64

	if v, ok := raw["daily_cost_cap_usd"]; ok {
		f, err := parseFloat64Field("daily_cost_cap_usd", v)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		newCap = &f
	}

	if v, ok := raw["max_agent_llm_calls_per_hour"]; ok {
		i, err := parseInt64Field("max_agent_llm_calls_per_hour", v)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		newLLM = &i
	}

	if v, ok := raw["max_agent_tool_calls_per_minute"]; ok {
		i, err := parseInt64Field("max_agent_tool_calls_per_minute", v)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		newTool = &i
	}

	// Snapshot old values for the audit entry before mutation.
	oldCfg := a.agentLoop.GetConfig().Sandbox.RateLimits

	if err := a.safeUpdateConfigJSON(func(m map[string]any) error {
		sandbox, _ := m["sandbox"].(map[string]any)
		if sandbox == nil {
			sandbox = map[string]any{}
			m["sandbox"] = sandbox
		}
		rl, _ := sandbox["rate_limits"].(map[string]any)
		if rl == nil {
			rl = map[string]any{}
			sandbox["rate_limits"] = rl
		}
		if newCap != nil {
			rl["daily_cost_cap_usd"] = *newCap
		}
		if newLLM != nil {
			rl["max_agent_llm_calls_per_hour"] = *newLLM
		}
		if newTool != nil {
			rl["max_agent_tool_calls_per_minute"] = *newTool
		}
		return nil
	}); err != nil {
		slog.Error("rest: update rate limits", "error", err)
		jsonErr(w, http.StatusInternalServerError, "could not save config")
		return
	}

	if reloadErr := a.awaitReload(); reloadErr != nil {
		if auditLogger := a.agentLoop.AuditLogger(); auditLogger != nil {
			newCfg := a.agentLoop.GetConfig().Sandbox.RateLimits
			if err := audit.EmitSecuritySettingChange(
				r.Context(), auditLogger, "sandbox.rate_limits",
				map[string]any{
					"daily_cost_cap_usd":              oldCfg.DailyCostCapUSD,
					"max_agent_llm_calls_per_hour":    oldCfg.MaxAgentLLMCallsPerHour,
					"max_agent_tool_calls_per_minute": oldCfg.MaxAgentToolCallsPerMinute,
				},
				map[string]any{
					"daily_cost_cap_usd":              newCfg.DailyCostCapUSD,
					"max_agent_llm_calls_per_hour":    newCfg.MaxAgentLLMCallsPerHour,
					"max_agent_tool_calls_per_minute": newCfg.MaxAgentToolCallsPerMinute,
				},
			); err != nil {
				slog.Error("rest: audit log rate limits update", "error", err)
			}
		}
		jsonOK(w, map[string]any{
			"saved":            true,
			"requires_restart": true,
			"warning":          "config saved to disk but hot-reload failed; restart the gateway to apply",
		})
		return
	}

	// Build new snapshot for audit and response.
	newCfg := a.agentLoop.GetConfig().Sandbox.RateLimits

	if auditLogger := a.agentLoop.AuditLogger(); auditLogger != nil {
		if err := audit.EmitSecuritySettingChange(
			r.Context(),
			auditLogger,
			"sandbox.rate_limits",
			map[string]any{
				"daily_cost_cap_usd":              oldCfg.DailyCostCapUSD,
				"max_agent_llm_calls_per_hour":    oldCfg.MaxAgentLLMCallsPerHour,
				"max_agent_tool_calls_per_minute": oldCfg.MaxAgentToolCallsPerMinute,
			},
			map[string]any{
				"daily_cost_cap_usd":              newCfg.DailyCostCapUSD,
				"max_agent_llm_calls_per_hour":    newCfg.MaxAgentLLMCallsPerHour,
				"max_agent_tool_calls_per_minute": newCfg.MaxAgentToolCallsPerMinute,
			},
		); err != nil {
			slog.Error("rest: audit log rate limits update", "error", err)
		}
	}

	slog.Info("rest: rate limits updated",
		"daily_cost_cap_usd", newCfg.DailyCostCapUSD,
		"max_agent_llm_calls_per_hour", newCfg.MaxAgentLLMCallsPerHour,
		"max_agent_tool_calls_per_minute", newCfg.MaxAgentToolCallsPerMinute,
	)

	jsonOK(w, map[string]any{
		"saved":            true,
		"requires_restart": false,
		"applied": map[string]any{
			"daily_cost_cap_usd":              newCfg.DailyCostCapUSD,
			"max_agent_llm_calls_per_hour":    newCfg.MaxAgentLLMCallsPerHour,
			"max_agent_tool_calls_per_minute": newCfg.MaxAgentToolCallsPerMinute,
		},
	})
}

// parseFloat64Field decodes a JSON raw value as a strict float64.
// Rejects: JSON strings, null, NaN, Inf, and negative values.
func parseFloat64Field(name string, raw json.RawMessage) (float64, error) {
	// Reject JSON strings and null.
	if len(raw) > 0 && (raw[0] == '"' || string(raw) == "null") {
		return 0, fmt.Errorf("%s: must be a non-negative number", name)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("%s: must be a non-negative number", name)
	}
	f, err := n.Float64()
	if err != nil {
		return 0, fmt.Errorf("%s: must be a non-negative number", name)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s: NaN and Inf are not allowed", name)
	}
	if f < 0 {
		return 0, fmt.Errorf("%s: must be >= 0 (0 = unlimited)", name)
	}
	return f, nil
}

// parseInt64Field decodes a JSON raw value as a strict int64.
// Rejects: JSON strings, null, floats with fractional parts, negative values,
// and values exceeding math.MaxInt64.
func parseInt64Field(name string, raw json.RawMessage) (int64, error) {
	// Reject JSON strings and null.
	if len(raw) > 0 && (raw[0] == '"' || string(raw) == "null") {
		return 0, fmt.Errorf("%s: must be a non-negative integer", name)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("%s: must be a non-negative integer", name)
	}
	// Attempt exact integer parse first.
	i, intErr := n.Int64()
	if intErr == nil {
		if i < 0 {
			return 0, fmt.Errorf("%s: must be >= 0 (0 = unlimited)", name)
		}
		return i, nil
	}
	// If Int64() failed, check whether the value is a float.
	f, floatErr := n.Float64()
	if floatErr != nil {
		// Neither int nor float — token is probably too large.
		return 0, fmt.Errorf("%s: value overflows int64", name)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s: NaN and Inf are not allowed", name)
	}
	// If f > MaxInt64 it's an overflow.
	if f > math.MaxInt64 {
		return 0, fmt.Errorf("%s: value overflows int64", name)
	}
	// It parsed as a float but not as int64 — must be a fractional value.
	return 0, fmt.Errorf("%s: must be an integer, not a float", name)
}

