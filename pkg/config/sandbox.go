// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

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

// OmnipusSandboxConfig holds Wave 2 kernel-level sandboxing configuration per
// BRD SEC-01 through SEC-20 (Landlock, seccomp, Job Objects, RBAC, audit log).
//
// All fields default to the most restrictive safe value when omitted.
// Populated from config.json under the "sandbox" key.
//
// This struct is intentionally empty in Wave 1 — enforcement is implemented
// in pkg/security/ (Wave 2). The config struct is defined now so config.json
// can carry sandbox keys without parse errors during the transition.
type OmnipusSandboxConfig struct {
	// Enabled activates kernel-level sandboxing. Default: false (Wave 1).
	// Set to true once pkg/security/ backends are available (Wave 2).
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
	PromptInjectionLevel string `json:"prompt_injection_level,omitempty"`

	// RateLimits configures per-agent LLM/tool call limits and the global
	// daily cost cap (SEC-26). All fields default to 0 (no limit).
	RateLimits OmnipusRateLimitsConfig `json:"rate_limits,omitempty"`
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
