// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

import (
	"encoding/json"
	"fmt"
)

// SkillTrustLevel controls how skills without a verifiable SHA-256 hash are handled (SEC-09).
type SkillTrustLevel string

const (
	// SkillTrustBlockUnverified blocks installation when hash cannot be verified.
	SkillTrustBlockUnverified SkillTrustLevel = "block_unverified"
	// SkillTrustWarnUnverified warns but allows unverified installs (default).
	SkillTrustWarnUnverified SkillTrustLevel = "warn_unverified"
	// SkillTrustAllowAll skips all hash verification. omnipus doctor warns when set.
	SkillTrustAllowAll SkillTrustLevel = "allow_all"
)

// UnmarshalJSON validates and deserializes a SkillTrustLevel from JSON.
// Rejects unknown values at decode time so config.json with a typo fails fast
// at boot instead of silently resolving to the zero value.
func (l *SkillTrustLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch SkillTrustLevel(s) {
	case SkillTrustBlockUnverified, SkillTrustWarnUnverified, SkillTrustAllowAll:
		*l = SkillTrustLevel(s)
		return nil
	case "":
		// empty string means field was omitted — keep zero value
		*l = ""
		return nil
	default:
		return fmt.Errorf("invalid skill_trust: %q (must be one of: block_unverified, warn_unverified, allow_all)", s)
	}
}

// PromptInjectionLevel controls prompt guard aggressiveness (SEC-25).
type PromptInjectionLevel string

const (
	// PromptInjectionLow applies minimal prompt sanitization.
	PromptInjectionLow PromptInjectionLevel = "low"
	// PromptInjectionMedium applies moderate prompt sanitization (default).
	PromptInjectionMedium PromptInjectionLevel = "medium"
	// PromptInjectionHigh applies aggressive prompt sanitization.
	PromptInjectionHigh PromptInjectionLevel = "high"
)

// UnmarshalJSON validates and deserializes a PromptInjectionLevel from JSON.
// Empty string is accepted (config may legitimately omit it — the handler
// defaults to "medium"). Only genuinely unknown non-empty values are rejected.
func (l *PromptInjectionLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch PromptInjectionLevel(s) {
	case PromptInjectionLow, PromptInjectionMedium, PromptInjectionHigh:
		*l = PromptInjectionLevel(s)
		return nil
	case "":
		// empty string means field was omitted — keep zero value
		*l = ""
		return nil
	default:
		return fmt.Errorf("invalid prompt_injection_level: %q (must be one of: low, medium, high)", s)
	}
}

// OmnipusSandboxConfig holds Wave 2 kernel-level sandboxing configuration per
// BRD SEC-01 through SEC-20 (Landlock, seccomp, Job Objects, RBAC, audit log)
// and Sprint-J sandbox-apply wiring (FR-J-001..016).
//
// All fields default to the most restrictive safe value when omitted.
// Populated from config.json under the "sandbox" key.
type OmnipusSandboxConfig struct {
	// Mode selects how the sandbox enforces policy at boot (Sprint J).
	// Valid values: "enforce" (default on capable kernels), "permissive"
	// (audit-only), "off" (disabled — development only).
	//
	// When Mode is empty, the legacy Enabled field controls behavior
	// (Enabled=true → enforce, Enabled=false → off) for backwards
	// compatibility with configs written before Sprint J.
	Mode string `json:"mode,omitempty"`

	// Enabled activates kernel-level sandboxing. Deprecated: use Mode
	// instead. Kept for backwards compatibility — Enabled=true maps to
	// Mode=enforce and Enabled=false maps to Mode=off when Mode is empty.
	//
	// Deprecated: use Mode ("enforce", "permissive", "off").
	Enabled bool `json:"enabled,omitempty"`

	// AllowNetworkOutbound permits sandboxed processes to make outbound TCP
	// connections. When false (default), outbound connections are blocked
	// at the Landlock/seccomp layer. Requires Enabled: true.
	AllowNetworkOutbound bool `json:"allow_network_outbound,omitempty"`

	// AllowedPaths lists additional filesystem paths the sandbox may read.
	// Paths outside this list (and the agent workspace) are inaccessible.
	AllowedPaths []string `json:"allowed_paths,omitempty"`

	// AuditLog enables the structured security audit log per SEC-17.
	// Written to ~/.omnipus/system/audit.jsonl.
	AuditLog bool `json:"audit_log,omitempty"`

	// SkillTrust controls how skills without a verifiable SHA-256 hash are handled (SEC-09).
	// Valid values: SkillTrustBlockUnverified, SkillTrustWarnUnverified (default), SkillTrustAllowAll.
	// SkillTrustAllowAll disables hash verification and triggers an omnipus doctor warning.
	SkillTrust SkillTrustLevel `json:"skill_trust,omitempty"`

	// PromptInjectionLevel controls how aggressively the prompt guard
	// sanitizes untrusted tool results (SEC-25). Valid: "low", "medium"
	// (default), "high". Affects web_search, web_fetch, browser_*, read_file
	// results before they enter the LLM's context.
	PromptInjectionLevel PromptInjectionLevel `json:"prompt_injection_level,omitempty"`

	// RateLimits configures per-agent LLM/tool call limits and the global
	// daily cost cap (SEC-26). All fields default to 0 (no limit).
	RateLimits OmnipusRateLimitsConfig `json:"rate_limits,omitempty"`

	// ToolPolicies holds global per-tool access policies. Keys are tool names;
	// values are "allow", "ask", or "deny". Takes precedence over agent-level
	// policies when stricter (deny > ask > allow).
	ToolPolicies map[string]string `json:"tool_policies,omitempty"`

	// DefaultToolPolicy is the fallback global policy for tools not listed in
	// ToolPolicies. Valid values: "allow" (default), "ask", "deny".
	DefaultToolPolicy string `json:"default_tool_policy,omitempty"`

	// SSRF configures outbound-HTTP SSRF protection (SEC-24).
	// When Enabled is true, all tool HTTP clients (web_search, skills installer,
	// browser, exec proxy) route through the SSRFChecker which blocks
	// connections to private/internal IP ranges and cloud metadata endpoints.
	// AllowInternal lists hosts, IPs, or CIDRs that are exempted from SSRF
	// blocking (e.g. ["localhost", "10.0.0.0/8"] to allow an internal search
	// service while still blocking all other private ranges).
	SSRF OmnipusSSRFConfig `json:"ssrf,omitempty"`
}

// OmnipusSSRFConfig holds SSRF protection settings for outbound HTTP clients (SEC-24).
type OmnipusSSRFConfig struct {
	// Enabled activates SSRF protection for all outbound HTTP tool clients.
	// Default: false (not enabled). Set to true to block private-IP connections.
	Enabled bool `json:"enabled,omitempty"`

	// AllowInternal lists hostnames, exact IPs, or CIDR ranges that are exempt
	// from SSRF blocking even when Enabled is true. Entries may be:
	//   - Exact IPv4/IPv6:  "127.0.0.1", "::1"
	//   - CIDR range:       "10.0.0.0/8", "192.168.0.0/16"
	//   - Hostname:         "localhost", "internal.corp"
	AllowInternal []string `json:"allow_internal,omitempty"`
}

// OmnipusRateLimitsConfig holds Wave 4 rate limit configuration (SEC-26).
// All fields default to 0, meaning no limit is enforced.
type OmnipusRateLimitsConfig struct {
	// DailyCostCapUSD is the global daily cost cap in USD. 0 = no cap.
	DailyCostCapUSD float64 `json:"daily_cost_cap_usd,omitempty"`
	// MaxAgentLLMCallsPerHour limits LLM calls per agent per hour. 0 = no limit.
	MaxAgentLLMCallsPerHour int `json:"max_agent_llm_calls_per_hour,omitempty"`
	// MaxAgentToolCallsPerMinute limits tool calls per agent per minute. 0 = no limit.
	MaxAgentToolCallsPerMinute int `json:"max_agent_tool_calls_per_minute,omitempty"`
}

// ResolvedMode returns the effective sandbox mode string, applying the
// legacy Enabled→Mode mapping when Mode is empty:
//
//	Mode set            → Mode (normalized via strings.ToLower-trim)
//	Mode empty, Enabled → "enforce" (backwards compat)
//	Mode empty, !Enabled → "off"    (backwards compat — explicit disable)
//
// Note: on a fresh config where neither field is set, Enabled defaults to
// false, so this returns "off". Callers that want the "enforce on capable
// kernels" default behavior should apply it at a higher layer (e.g. the
// gateway boot path) rather than here — this helper only reports what the
// config file says.
func (s OmnipusSandboxConfig) ResolvedMode() string {
	if s.Mode != "" {
		return s.Mode
	}
	if s.Enabled {
		return "enforce"
	}
	return "off"
}
