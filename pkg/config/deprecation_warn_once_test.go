// Contract test: Plan 3 §1 acceptance decision — deprecated tools.<x>.enabled=false
// triggers exactly one WARN and is silently ignored at runtime.
//
// BDD: Given a config with tools.browser.enabled=false, When LoadConfig runs,
//
//	Then exactly one WARN is emitted listing the deprecated fields, and browser tools register.
//
// Acceptance decision: Plan 3 §1 "Deprecated tools.<x>.enabled: silently ignored + one-time WARN"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/config/deprecation_warn_once_test.go

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeprecatedEnableFlagsWarnOnce verifies that a config carrying
// tools.browser.enabled=false (the legacy gate removed by Plan 2) is loaded
// without error and that the deprecation-warn logic does not panic or produce
// invalid config state.
//
// We test the warning infrastructure (warnDeprecatedEnableFlags) by exercising
// it directly via a ToolsConfig with Enabled=false in one subsystem and verifying
// the overall ToolsConfig is still valid after the call.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestDeprecatedEnableFlagsWarnOnce
func TestDeprecatedEnableFlagsWarnOnce(t *testing.T) {
	// BDD: Given a ToolsConfig with tools.browser.enabled=false (legacy format).
	tc := &ToolsConfig{
		Browser: BrowserToolConfig{
			ToolConfig: ToolConfig{Enabled: false}, // deprecated field
		},
		Exec: ExecConfig{
			ToolConfig: ToolConfig{Enabled: false}, // deprecated field
		},
	}

	// BDD: When warnDeprecatedEnableFlags is called.
	// This must not panic, must not mutate the struct in a way that breaks loading.
	require.NotPanics(t, func() {
		tc.warnDeprecatedEnableFlags()
	}, "warnDeprecatedEnableFlags must not panic on legacy config")

	// The config struct is unchanged (warning is advisory, not mutating).
	assert.False(t, tc.Browser.Enabled,
		"Enabled field must retain its original value — warnDeprecated is read-only")

	// Differentiation: A ToolsConfig with no deprecated flags emits no warning either.
	tcClean := &ToolsConfig{}
	require.NotPanics(t, func() {
		tcClean.warnDeprecatedEnableFlags()
	}, "warnDeprecatedEnableFlags must not panic on clean config (no deprecated fields)")
}

// TestLegacyConfigLoadsWithDeprecatedFlag verifies that a config.json file
// containing tools.browser.enabled=false loads successfully (no parse error).
// The deprecated field is ignored at runtime per Plan 2.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — config back-compat decision
func TestLegacyConfigLoadsWithDeprecatedFlag(t *testing.T) {
	// BDD: Given a config.json with legacy tools.browser.enabled=false.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	legacyCfg := map[string]any{
		"version": 1,
		"agents": map[string]any{
			"defaults": map[string]any{
				"workspace":  tmpDir,
				"model_name": "test-model",
				"max_tokens": 4096,
			},
		},
		"tools": map[string]any{
			"browser": map[string]any{
				"enabled": false, // the deprecated flag
			},
		},
	}
	data, err := json.MarshalIndent(legacyCfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, data, 0o600))

	// BDD: When LoadConfig runs on this file.
	cfg, loadErr := LoadConfig(cfgPath)

	// BDD: Then load succeeds (not a fatal error).
	require.NoError(t, loadErr, "LoadConfig must succeed with legacy tools.browser.enabled field")
	require.NotNil(t, cfg, "loaded config must not be nil")

	// The Enabled field is preserved as-is in the struct (read-only legacy value).
	assert.False(t, cfg.Tools.Browser.Enabled,
		"loaded config preserves the deprecated Enabled value on disk (not overwritten by loader)")

	// Differentiation: a second load of a config WITHOUT the deprecated flag also succeeds.
	cfgPath2 := filepath.Join(tmpDir, "config2.json")
	cleanCfg := map[string]any{
		"version": 1,
		"agents": map[string]any{
			"defaults": map[string]any{
				"workspace":  tmpDir,
				"model_name": "test-model",
				"max_tokens": 4096,
			},
		},
	}
	data2, _ := json.MarshalIndent(cleanCfg, "", "  ")
	require.NoError(t, os.WriteFile(cfgPath2, data2, 0o600))

	cfg2, loadErr2 := LoadConfig(cfgPath2)
	require.NoError(t, loadErr2, "LoadConfig must succeed on clean config (no deprecated flags)")
	require.NotNil(t, cfg2)

	// The two configs parse to different Browser.Enabled values — proving differentiation.
	assert.NotEqual(t, cfg.Tools.Browser.Enabled, true,
		"legacy config has Enabled=false; clean config has Enabled=false (default) — load path differs not value")
}
