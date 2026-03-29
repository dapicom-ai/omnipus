// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package security

// DiagnosticConfig holds the configuration fields relevant for security diagnostics.
type DiagnosticConfig struct {
	// ExecToolEnabled is true when exec tool is available to agents.
	ExecToolEnabled bool
	// ExecProxyEnabled is true when the exec HTTP proxy (SEC-28) is running.
	ExecProxyEnabled bool
	// ExecAllowedBinaries is the configured exec allowlist (SEC-05).
	ExecAllowedBinaries []string
}

// DiagnosticWarning is a single diagnostic finding.
type DiagnosticWarning struct {
	Code    string // e.g., "SEC-29"
	Message string
}

// CheckExecEgress checks whether the exec tool is enabled without adequate
// network egress control, per FR-030 / SEC-29. Returns warnings for `omnipus doctor`.
func CheckExecEgress(cfg DiagnosticConfig) []DiagnosticWarning {
	if !cfg.ExecToolEnabled {
		return nil
	}

	var warnings []DiagnosticWarning

	if !cfg.ExecProxyEnabled {
		warnings = append(warnings, DiagnosticWarning{
			Code:    "SEC-29",
			Message: "Exec tool is enabled but the exec HTTP proxy (SEC-28) is not running. Child processes can make unfiltered outbound requests. Enable the exec proxy or disable the exec tool.",
		})
	}

	if len(cfg.ExecAllowedBinaries) == 0 {
		warnings = append(warnings, DiagnosticWarning{
			Code:    "SEC-05",
			Message: "Exec tool is enabled but no binary allowlist is configured. Any binary can be executed. Configure security.policy.exec.allowed_binaries to restrict exec.",
		})
	}

	return warnings
}
