// Tier 3 run_in_workspace JSON-schema proof tests for FR-008a / CR-03.
//
// These verify the migration from SilentResult(<English sentence>) to
// NewToolResult(<JSON>) carrying both a structured payload AND a human
// _summary so the LLM continues to see natural-language explanation in
// its message history.

package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalServedSubdirsStub satisfies ServedSubdirsRegistry for tests that
// need Execute() to succeed without a real agent.ServedSubdirs (avoiding
// an import cycle from pkg/tools → pkg/agent).
type minimalServedSubdirsStub struct {
	token string
}

func (s *minimalServedSubdirsStub) Register(agentID, _ string, _ time.Duration) (string, time.Time, error) {
	return s.token, time.Now().Add(5 * time.Minute), nil
}

func (s *minimalServedSubdirsStub) ActiveForAgent(_ string) (string, time.Time, bool) {
	return "", time.Time{}, false
}

// TestRunInWorkspaceTool_JSONResultShape verifies FR-008a: the tool
// result ForLLM is valid JSON containing the required keys on Linux,
// or returns the OS-unsupported error on non-Linux (F-18: replaces the
// previous buildRunInWorkspaceJSONForTest synthetic-string assertion).
//
// On Linux this exercises the real Execute() path up to the nil-registry
// check; the successful JSON shape (path/url/expires_at/command/port/_summary)
// is covered by pkg/agent integration tests that run a real dev server.
func TestRunInWorkspaceTool_JSONResultShape(t *testing.T) {
	tool := NewRunInWorkspaceTool(
		RunInWorkspaceConfig{
			PortRange:       [2]int32{18000, 18999},
			MaxConcurrent:   2,
			AuditFailClosed: false,
		},
		t.TempDir(),            // workspace
		nil,                    // registry — nil triggers "registry not configured" error on Linux
		nil,                    // proxy
		nil,                    // auditLogger
		"http://127.0.0.1:5001", // gatewayHost
	)

	ctx := WithAgentID(context.Background(), "fr008a-agent")
	result := tool.Execute(ctx, map[string]any{
		"command":     "next dev",
		"expose_port": float64(18000),
	})

	if runtime.GOOS != "linux" {
		// Non-Linux: OS gate returns the documented error string.
		require.True(t, result.IsError, "non-Linux must return an error")
		assert.Equal(t, Tier3UnsupportedMessage, result.ForLLM,
			"FR-017: error wording must match spec v4 (no version string)")
		return
	}

	// Linux with nil registry: the tool should return "registry not configured".
	require.True(t, result.IsError, "nil registry on Linux must return an error")
	assert.Contains(t, result.ForLLM, "registry not configured",
		"error must say registry not configured")
}

// TestRunInWorkspaceTool_Tier3UnsupportedMessage verifies FR-017: the
// constant no longer contains a version string (was "in v3 — Linux only").
func TestRunInWorkspaceTool_Tier3UnsupportedMessage(t *testing.T) {
	assert.NotContains(t, Tier3UnsupportedMessage, "v3",
		"FR-017: Tier3UnsupportedMessage must not reference 'v3' (spec is at v4)")
	assert.NotContains(t, Tier3UnsupportedMessage, "v4",
		"FR-017: message should be version-agnostic")
	assert.Contains(t, Tier3UnsupportedMessage, "Linux",
		"FR-017: message must state the Linux requirement")
}

// TestServeWorkspace_ResultPathField verifies FR-008: serve_workspace
// emits both `path` and `url` in its JSON result via a real Execute()
// call with a stub ServedSubdirsRegistry (F-18: replaces the previous
// hardcoded JSON string assertion).
func TestServeWorkspace_ResultPathField(t *testing.T) {
	dir := t.TempDir()
	stub := &minimalServedSubdirsStub{token: "stubtoken42"}

	tool := NewServeWorkspaceTool(
		dir,
		"fr008-agent",
		"http://127.0.0.1:5001",
		stub,
		60,
		86400,
	)

	ctx := WithAgentID(context.Background(), "fr008-agent")
	result := tool.Execute(ctx, map[string]any{
		"path":     ".",
		"duration": float64(300),
	})

	require.False(t, result.IsError, "Execute must succeed: %s", result.ForLLM)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.ForLLM), &parsed),
		"FR-008: tool result must be valid JSON")

	for _, key := range []string{"path", "url", "expires_at"} {
		assert.Contains(t, parsed, key, "FR-008: result must include %q key", key)
	}

	pathVal, _ := parsed["path"].(string)
	assert.True(t, strings.HasPrefix(pathVal, "/serve/"),
		"FR-008: path must start with /serve/, got %q", pathVal)

	urlVal, _ := parsed["url"].(string)
	assert.True(t, strings.HasSuffix(urlVal, pathVal),
		"FR-008: url must end with path (replay-safe), url=%q path=%q", urlVal, pathVal)
}
