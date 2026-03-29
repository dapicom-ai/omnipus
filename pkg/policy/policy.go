// Package policy implements the declarative security policy engine for Omnipus.
//
// It handles SEC-04 (tool allow/deny), SEC-05 (per-binary exec control),
// SEC-07 (deny-by-default), SEC-11 (JSON policy files), SEC-12 (static policies),
// SEC-17 (explainable decisions), and SEC-30 (DM policy safety checks).
//
// Policies are loaded once at startup from the security section of config.json
// and are immutable after initialization. Concurrent reads are safe without locking.
package policy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// DefaultPolicy is a named type for the security default policy value.
type DefaultPolicy string

const (
	PolicyAllow DefaultPolicy = "allow"
	PolicyDeny  DefaultPolicy = "deny"
)

// Decision represents the outcome of a policy evaluation.
type Decision struct {
	Allowed    bool   // Whether the action is permitted
	PolicyRule string // Human-readable explanation of which rule matched (SEC-17)
}

// AgentToolsPolicy defines the allow/deny tool lists for an agent.
type AgentToolsPolicy struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// AgentPolicy defines per-agent tool permissions.
type AgentPolicy struct {
	Tools AgentToolsPolicy `json:"tools,omitempty"`

	// Legacy fields for backward compatibility.
	ToolsAllow []string `json:"tools_allow,omitempty"`
	ToolsDeny  []string `json:"tools_deny,omitempty"`
}

// effectiveAllow returns the effective allow list.
func (ap *AgentPolicy) effectiveAllow() []string {
	if len(ap.Tools.Allow) > 0 {
		return ap.Tools.Allow
	}
	return ap.ToolsAllow
}

// effectiveDeny returns the effective deny list.
func (ap *AgentPolicy) effectiveDeny() []string {
	if len(ap.Tools.Deny) > 0 {
		return ap.Tools.Deny
	}
	return ap.ToolsDeny
}

// hasAllowList returns true if an allow list is explicitly set (even if empty).
func (ap *AgentPolicy) hasAllowList() bool {
	return ap.Tools.Allow != nil || ap.ToolsAllow != nil
}

// SSRFPolicy holds SSRF protection settings.
type SSRFPolicy struct {
	Enabled       bool     `json:"enabled,omitempty"`
	AllowInternal []string `json:"allow_internal,omitempty"`
}

// IsEnabled returns whether SSRF protection is enabled.
func (s *SSRFPolicy) IsEnabled() bool { return s.Enabled }

// AuditPolicy holds audit logging settings.
type AuditPolicy struct {
	Output            string   `json:"output,omitempty"`
	Redaction         bool     `json:"redaction,omitempty"`
	RedactionPatterns []string `json:"redaction_patterns,omitempty"`
	TamperEvident     bool     `json:"tamper_evident,omitempty"`
	RetentionDays     int      `json:"retention_days,omitempty"`
}

// IsRedactionEnabled returns whether log redaction is enabled.
func (a *AuditPolicy) IsRedactionEnabled() bool { return a.Redaction }

// ExecPolicy defines exec tool policy.
type ExecPolicy struct {
	AllowedBinaries []string `json:"allowed_binaries,omitempty"`
	Approval        string   `json:"approval,omitempty"`
}

// FilesystemPolicy defines allowed filesystem paths.
type FilesystemPolicy struct {
	AllowedPaths []string `json:"allowed_paths,omitempty"`
}

// PolicySection groups sub-policies (filesystem, exec).
type PolicySection struct {
	Filesystem FilesystemPolicy `json:"filesystem,omitempty"`
	Exec       ExecPolicy       `json:"exec,omitempty"`
}

// RateLimitsPolicy holds rate limiting configuration.
type RateLimitsPolicy struct {
	DailyCostCapUSD float64 `json:"daily_cost_cap_usd,omitempty"`
}

// SecurityConfig is the primary security configuration type.
type SecurityConfig struct {
	DefaultPolicy DefaultPolicy          `json:"default_policy,omitempty"`
	Agents        map[string]AgentPolicy `json:"agents,omitempty"`
	SSRF          SSRFPolicy             `json:"ssrf,omitempty"`
	Audit         AuditPolicy            `json:"audit,omitempty"`
	Policy        PolicySection          `json:"policy,omitempty"`
	RateLimits    RateLimitsPolicy       `json:"rate_limits,omitempty"`
}

// GetDefaultPolicy returns the effective default policy, defaulting to "deny"
// (deny-by-default per CLAUDE.md hard constraint #6).
func (sc *SecurityConfig) GetDefaultPolicy() DefaultPolicy {
	if sc.DefaultPolicy == "" {
		return PolicyDeny
	}
	return sc.DefaultPolicy
}

// ParseSecurityConfig parses a raw JSON byte slice into a SecurityConfig.
// Returns an error for malformed JSON or invalid values.
func ParseSecurityConfig(data []byte) (*SecurityConfig, error) {
	var cfg SecurityConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("security config: invalid JSON: %w", err)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateConfig(cfg *SecurityConfig) error {
	switch cfg.DefaultPolicy {
	case "", PolicyAllow, PolicyDeny:
		// valid
	default:
		return fmt.Errorf("security.default_policy: invalid value %q (must be \"allow\" or \"deny\")", cfg.DefaultPolicy)
	}

	switch cfg.Audit.Output {
	case "", "file", "stdout", "both":
		// valid
	default:
		return fmt.Errorf("security.audit.output: invalid value %q", cfg.Audit.Output)
	}

	switch cfg.Policy.Exec.Approval {
	case "", "ask", "off":
		// valid
	default:
		return fmt.Errorf("security.policy.exec.approval: invalid value %q", cfg.Policy.Exec.Approval)
	}

	// Validate filesystem paths are absolute or start with ~
	for _, p := range cfg.Policy.Filesystem.AllowedPaths {
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~/") {
			return fmt.Errorf("security.policy.filesystem.allowed_paths: path %q must be absolute or start with ~/", p)
		}
	}

	return nil
}

// IsSystemAgent returns true if agentID is the system agent, which is exempt
// from rate limits and certain policy restrictions.
func IsSystemAgent(agentID string) bool {
	return agentID == "omnipus-system"
}

// ChannelConfig describes a channel configuration for DM safety checks.
type ChannelConfig struct {
	Name      string
	Enabled   bool
	AllowFrom []string
}

// CheckDMSafety checks channel configurations for overly permissive DM policies (SEC-30).
func CheckDMSafety(channels []ChannelConfig) []string {
	var warnings []string
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if len(ch.AllowFrom) == 0 {
			name := strings.Title(ch.Name) //nolint:staticcheck // strings.Title deprecated but functional
			warnings = append(warnings, fmt.Sprintf(
				"%s channel accepts messages from anyone. Set policies.allow_from to restrict access.", name))
		}
	}
	return warnings
}

