// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

// ConfigKey is a dotted path into the gateway's config.json. Using a typed
// string (rather than a raw string literal) gives every consumer that
// references a specific path — blockedPaths, RestartGatedKeys, getAtPath
// callers — compile-time protection against typos and rename drift.
type ConfigKey string

const (
	SandboxMode          ConfigKey = "sandbox.mode"
	SandboxEnabled       ConfigKey = "sandbox.enabled"
	SandboxAuditLog      ConfigKey = "sandbox.audit_log"
	SandboxAllowedPaths  ConfigKey = "sandbox.allowed_paths"
	SessionDMScope       ConfigKey = "session.dm_scope"
	GatewayPort          ConfigKey = "gateway.port"
	GatewayUsers         ConfigKey = "gateway.users"
	GatewayDevModeBypass ConfigKey = "gateway.dev_mode_bypass"
)
