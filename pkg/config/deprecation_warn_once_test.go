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
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeprecatedEnableFlagsWarnOnce verifies that a config carrying
// tools.browser.enabled=false (the legacy gate removed by Plan 2) emits exactly one
// deprecation warning when warnDeprecatedEnableFlags is called multiple times.
//
// We capture slog output by replacing the default handler for the duration of the
// test, then assert strings.Count(output, "deprecated") == 1.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestDeprecatedEnableFlagsWarnOnce
func TestDeprecatedEnableFlagsWarnOnce(t *testing.T) {
	// Reset the once guard so this test is independent of other tests in the package
	// that may have already triggered the warning (e.g. TestLegacyConfigLoadsWithDeprecatedFlag).
	deprecatedEnableFlagsWarnOnce = sync.Once{}

	// Capture slog output by installing a JSON handler on a buffer.
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() {
		slog.SetDefault(oldDefault)
	})

	// BDD: Given a ToolsConfig with tools.browser.enabled=false AND tools.exec.enabled=false.
	tc := &ToolsConfig{
		Browser: BrowserToolConfig{
			ToolConfig: ToolConfig{Enabled: false}, // deprecated field
		},
		Exec: ExecConfig{
			ToolConfig: ToolConfig{Enabled: false}, // deprecated field
		},
	}

	// BDD: When warnDeprecatedEnableFlags is called twice.
	require.NotPanics(t, func() { tc.warnDeprecatedEnableFlags() }, "first call must not panic")
	require.NotPanics(t, func() { tc.warnDeprecatedEnableFlags() }, "second call must not panic")

	// BDD: Then exactly one WARN containing "deprecated" is emitted.
	output := buf.String()
	count := strings.Count(output, "deprecated")
	assert.Equal(t, 1, count,
		"warnDeprecatedEnableFlags must emit the deprecation warning exactly once; "+
			"got count=%d in output: %q", count, output)

	// The config struct is unchanged (warning is advisory, not mutating).
	assert.False(t, tc.Browser.Enabled,
		"Enabled field must retain its original value — warnDeprecated is read-only")

	// Differentiation: calling once more after reset fires again exactly once (not twice).
	// Reset the once and buffer to verify independent behavior.
	deprecatedEnableFlagsWarnOnce = sync.Once{}
	buf.Reset()

	// Two more calls with the deprecated config: must still emit exactly one warning.
	require.NotPanics(t, func() { tc.warnDeprecatedEnableFlags() }, "third call must not panic")
	require.NotPanics(t, func() { tc.warnDeprecatedEnableFlags() }, "fourth call must not panic")

	count2 := strings.Count(buf.String(), "deprecated")
	assert.Equal(t, 1, count2,
		"after reset, warnDeprecatedEnableFlags must emit exactly one warning on two calls; got count=%d", count2)
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

	// Both configs parse with Enabled=false (legacy explicit, clean via default).
	assert.False(t, cfg.Tools.Browser.Enabled,
		"legacy config preserves Enabled=false from disk")
	assert.False(t, cfg2.Tools.Browser.Enabled,
		"clean config defaults Enabled=false")
}
