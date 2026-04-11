// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package config

// Runtime environment variable keys for the omnipus process.
// These control the location of files and binaries at runtime and are read
// directly via os.Getenv / os.LookupEnv. All omnipus-specific keys use the
// OMNIPUS_ prefix. Reference these constants instead of inline string
// literals to keep all supported knobs visible in one place and to prevent
// typos.
const (
	// EnvHome overrides the base directory for all omnipus data
	// (config, workspace, skills, auth store, …).
	// Default: ~/.omnipus
	// EnvHome uses OMNIPUS_HOME for backward compatibility with Omnipus ecosystem. Omnipus inherits this convention per CLAUDE.md ecosystem compatibility constraint.
	EnvHome = "OMNIPUS_HOME"

	// EnvConfig overrides the full path to the JSON config file.
	// Default: $OMNIPUS_HOME/config.json
	EnvConfig = "OMNIPUS_CONFIG"

	// EnvBuiltinSkills overrides the directory from which built-in
	// skills are loaded.
	// Default: <cwd>/skills
	EnvBuiltinSkills = "OMNIPUS_BUILTIN_SKILLS"

	// EnvBinary overrides the path to the omnipus executable.
	// Used by the web launcher when spawning the gateway subprocess.
	// Default: resolved from the same directory as the current executable.
	EnvBinary = "OMNIPUS_BINARY"

	// EnvGatewayHost overrides the host address for the gateway server.
	// Default: "127.0.0.1"
	EnvGatewayHost = "OMNIPUS_GATEWAY_HOST"
)
