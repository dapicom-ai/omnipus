//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"net/http"
)

// rest_security_wave4.go — Wave 4 operator-facing REST endpoint (SEC-26).
//
// This file wires one endpoint:
//   - GET /api/v1/security/rate-limits
//
// The endpoint exposes the current rate-limiting configuration and the
// accumulated daily cost so the UI can display a budget indicator without
// needing direct access to the in-memory registry.

// HandleRateLimits handles GET /api/v1/security/rate-limits.
//
// Returns the current per-agent rate-limit configuration (from config.json)
// and the accumulated daily cost from the in-memory registry. The response
// always has the full shape with zeroed values when no limits are configured
// or the registry is unavailable, so the UI does not need a nil-check path.
//
// Response shape:
//
//	{
//	  "enabled":                         bool,
//	  "daily_cost_usd":                  float64,
//	  "daily_cost_cap":                  float64,
//	  "max_agent_llm_calls_per_hour":    int,
//	  "max_agent_tool_calls_per_minute": int,
//	}
func (a *restAPI) HandleRateLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rlCfg := a.agentLoop.GetConfig().Sandbox.RateLimits
	enabled := rlCfg.DailyCostCapUSD > 0 ||
		rlCfg.MaxAgentLLMCallsPerHour > 0 ||
		rlCfg.MaxAgentToolCallsPerMinute > 0

	var dailyCost float64
	if registry := a.agentLoop.RateLimiter(); registry != nil {
		dailyCost = registry.GetDailyCost()
	} else {
		// Registry missing implies a broken init path; force enabled=false so
		// the UI never shows "enabled" when the in-memory accumulator is gone.
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
