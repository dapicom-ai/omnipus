// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

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
	// Valid values: "block_unverified", "warn_unverified" (default), "allow_all".
	// "allow_all" disables hash verification and triggers an omnipus doctor warning.
	SkillTrust string `json:"skill_trust,omitempty"`
}
