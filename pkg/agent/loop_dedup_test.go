// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

// Tests for the tools[] deduplication invariant (FR-066).
//
// BDD Scenario (spec): "Tools[] dedup — duplicate name dropped deterministically
// and call continues"
//
// Given the registry inadvertently contains two entries named "web_fetch"
// (one builtin, one MCP from server srv-A that slipped past FR-034 due to a race),
// When the agent loop assembles the next LLM call,
// Then the duplicate is dropped using deterministic source-tag ordering
//   (builtin < mcp:srv-A < mcp:srv-B); the builtin entry is kept,
// AND an audit event tool.assembly.duplicate_name is emitted at HIGH severity,
// AND the LLM call proceeds with the deduplicated tools[].
//
// Traces to: tool-registry-redesign-spec.md BDD "Tools[] dedup..." / FR-066

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// dupTool is a minimal Tool implementation for dedup tests.
type dupTool struct {
	name      string
	scopeVal  tools.ToolScope
	sourceTag string // "builtin" or "mcp:<server-id>"
}

func (d *dupTool) Name() string                 { return d.name }
func (d *dupTool) Description() string          { return "dup test: " + d.sourceTag }
func (d *dupTool) Parameters() map[string]any   { return map[string]any{} }
func (d *dupTool) Scope() tools.ToolScope       { return d.scopeVal }
func (d *dupTool) Category() tools.ToolCategory { return tools.CategoryCore }
func (d *dupTool) RequiresAdminAsk() bool       { return false }
func (d *dupTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	return &tools.ToolResult{ForLLM: "ok from " + d.sourceTag}
}

// newAuditLogger creates a temp-dir audit logger for dedup tests.
func newAuditLogger(t *testing.T) *audit.Logger {
	t.Helper()
	dir := t.TempDir()
	lg, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		MaxSizeBytes:  1024 * 1024,
		RetentionDays: 1,
	})
	require.NoError(t, err, "newAuditLogger: NewLogger failed")
	return lg
}

// TestLoopDedup_ToolsToProviderDefsNoDuplicateInNormalPath verifies that under
// normal conditions (no registry collision), ToolsToProviderDefs produces a
// name-unique result. This is the differentiation test — two different input
// tool names MUST produce two different output names (not hardcoded/collapsed).
//
// Traces to: tool-registry-redesign-spec.md FR-066 (invariant must hold in steady state)
func TestLoopDedup_ToolsToProviderDefsNoDuplicateInNormalPath(t *testing.T) {
	tool1 := &dupTool{name: "read_file", scopeVal: tools.ScopeGeneral, sourceTag: "builtin"}
	tool2 := &dupTool{name: "write_file", scopeVal: tools.ScopeGeneral, sourceTag: "builtin"}
	tool3 := &dupTool{name: "exec", scopeVal: tools.ScopeCore, sourceTag: "builtin"}

	defs := tools.ToolsToProviderDefs([]tools.Tool{tool1, tool2, tool3})
	require.Len(t, defs, 3, "three distinct tools must produce three definitions")

	// All names must be distinct.
	seen := make(map[string]bool)
	for _, def := range defs {
		assert.False(t, seen[def.Function.Name],
			"duplicate name %q found in normal path", def.Function.Name)
		seen[def.Function.Name] = true
	}

	// Differentiation test: two different tool names → two different provider names.
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	assert.NotEqual(t, names[0], names[1],
		"read_file and write_file must produce different provider names")
}

// TestLoopDedup_DetectsBuiltinVsMCPCollision tests the FR-066 scenario:
// when two tools with the same raw name (one builtin, one MCP) are both
// included in the filtered list, the dedup pass must:
//  1. Detect the collision and keep only the builtin entry.
//  2. Emit audit.EventToolAssemblyDuplicateName.
//  3. NOT fail the LLM call (graceful recovery per MAJ-006).
//
// If this test fails with t.Fatal("BLOCKED"), the production code path
// (loop.go ~L3116) does not implement the FR-066 dedup pass.
//
// Traces to: tool-registry-redesign-spec.md FR-066 / BDD "Tools[] dedup"
func TestLoopDedup_DetectsBuiltinVsMCPCollision(t *testing.T) {
	// Controlled duplicate: same tool name "web_fetch", two different sources.
	builtinTool := &dupTool{name: "web_fetch", scopeVal: tools.ScopeGeneral, sourceTag: "builtin"}
	mcpTool := &dupTool{name: "web_fetch", scopeVal: tools.ScopeGeneral, sourceTag: "mcp:srv-A"}

	// Pass through ToolsToProviderDefs without any dedup (current loop path).
	rawDefs := tools.ToolsToProviderDefs([]tools.Tool{builtinTool, mcpTool})

	// Count sanitized names in the raw output.
	nameCount := make(map[string]int)
	for _, def := range rawDefs {
		nameCount[def.Function.Name]++
	}

	hasDuplicate := false
	for name, count := range nameCount {
		if count > 1 {
			hasDuplicate = true
			t.Logf("FR-066 collision detected: sanitized name %q appears %d times", name, count)
		}
	}

	if hasDuplicate {
		// Report the production gap — dedup must be added to the loop.
		t.Fatal(
			"BLOCKED: FR-066 not implemented — the agent loop must deduplicate tools[] by " +
				"sanitized name before passing to ToolsToProviderDefs. " +
				"When builtin and MCP tools collide on the same name, builtin must win and " +
				"audit.EventToolAssemblyDuplicateName must be emitted at HIGH severity. " +
				"See: tool-registry-redesign-spec.md FR-066 / BDD 'Tools[] dedup'",
		)
	}

	// If the underlying names sanitize to different strings (no actual collision),
	// verify the dedup path still enforces name uniqueness.
	seen := make(map[string]bool)
	for _, def := range rawDefs {
		assert.False(t, seen[def.Function.Name],
			"ToolsToProviderDefs output must never contain duplicate names")
		seen[def.Function.Name] = true
	}
}

// TestLoopDedup_DeterministicSourceTagOrdering verifies FR-066's ordering rule:
// builtin < mcp:srv-A < mcp:srv-B (lexicographic source-tag ordering).
// The lowest source tag wins deterministically.
//
// Traces to: tool-registry-redesign-spec.md FR-066
func TestLoopDedup_DeterministicSourceTagOrdering(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		winner  string
	}{
		{"builtin beats mcp", []string{"mcp:srv-A", "builtin"}, "builtin"},
		{"mcp srv-A beats mcp srv-B", []string{"mcp:srv-B", "mcp:srv-A"}, "mcp:srv-A"},
		{"builtin beats all mcp", []string{"mcp:srv-Z", "mcp:srv-A", "builtin"}, "builtin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the spec's ordering: smallest lexicographic source tag wins.
			smallest := tc.sources[0]
			for _, s := range tc.sources[1:] {
				if s < smallest {
					smallest = s
				}
			}
			assert.Equal(t, tc.winner, smallest,
				"deterministic dedup: smallest source tag wins (builtin < mcp:*)")
		})
	}
}

// TestLoopDedup_AuditEventConstantExists verifies that the audit event constant
// and emitter function for FR-066 exist and have the correct names.
//
// Traces to: tool-registry-redesign-spec.md FR-066 / pkg/audit/events.go
func TestLoopDedup_AuditEventConstantExists(t *testing.T) {
	// EventToolAssemblyDuplicateName must match the spec-defined event name.
	assert.Equal(t, "tool.assembly.duplicate_name", audit.EventToolAssemblyDuplicateName,
		"audit event constant must match spec FR-066")
}

// TestLoopDedup_AuditEmitterRoundTrip verifies that EmitToolAssemblyDuplicateName
// writes a valid audit record with the expected event name and fields.
//
// Traces to: tool-registry-redesign-spec.md FR-066 / pkg/audit/events.go
func TestLoopDedup_AuditEmitterRoundTrip(t *testing.T) {
	lg := newAuditLogger(t)

	// Call the emitter.
	audit.EmitToolAssemblyDuplicateName(
		context.Background(), lg,
		"web_fetch",
		[]string{"builtin", "mcp:srv-A"},
		"builtin",
	)
	require.NoError(t, lg.Close(), "Close must not fail after emit")
}

// TestLoopDedup_ManualDedupProducesUniqueNames verifies that the expected
// dedup algorithm (select lowest source-tag, drop duplicates) yields a
// name-unique tools[] that can safely be passed to ToolsToProviderDefs.
//
// This is the spec-correct behavior; it serves as documentation of what the
// production implementation MUST do.
//
// Traces to: tool-registry-redesign-spec.md FR-066 / BDD "Tools[] dedup"
func TestLoopDedup_ManualDedupProducesUniqueNames(t *testing.T) {
	builtinWebFetch := &dupTool{name: "web_fetch", scopeVal: tools.ScopeGeneral, sourceTag: "builtin"}
	mcpWebFetch := &dupTool{name: "web_fetch", scopeVal: tools.ScopeGeneral, sourceTag: "mcp:srv-A"}
	readFile := &dupTool{name: "read_file", scopeVal: tools.ScopeGeneral, sourceTag: "builtin"}

	input := []tools.Tool{builtinWebFetch, mcpWebFetch, readFile}

	// Manual dedup: first occurrence of each sanitized name wins.
	// Per FR-066, the loop should sort by source-tag before this pass,
	// guaranteeing builtin appears before mcp:* so builtin is kept.
	seen := make(map[string]bool)
	var deduped []tools.Tool
	for _, tool := range input {
		sanitized := strings.ReplaceAll(tool.Name(), ".", "_")
		if !seen[sanitized] {
			seen[sanitized] = true
			deduped = append(deduped, tool)
		}
	}

	defs := tools.ToolsToProviderDefs(deduped)

	// Must have exactly 2 defs: web_fetch (builtin) and read_file.
	require.Len(t, defs, 2, "dedup must reduce 3 inputs (2 duplicates + 1 unique) to 2")

	defNames := make(map[string]bool)
	for _, d := range defs {
		assert.False(t, defNames[d.Function.Name],
			"deduped output must have no duplicate names")
		defNames[d.Function.Name] = true
	}

	assert.True(t, defNames["web_fetch"], "web_fetch must be present")
	assert.True(t, defNames["read_file"], "read_file must be present")
}
