// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- minimal test tool for MCP registry tests ---

type mcpTestTool struct {
	name string
}

func (t *mcpTestTool) Name() string               { return t.name }
func (t *mcpTestTool) Description() string        { return "mcp test tool: " + t.name }
func (t *mcpTestTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t *mcpTestTool) Scope() ToolScope           { return ScopeGeneral }
func (t *mcpTestTool) RequiresAdminAsk() bool     { return false }
func (t *mcpTestTool) Category() ToolCategory     { return CategoryCode }
func (t *mcpTestTool) Execute(_ context.Context, _ map[string]any) *ToolResult {
	return SilentResult("ok")
}

// --- TestMCPRegistry_DynamicAddRemove ---

// TestMCPRegistry_DynamicAddRemove verifies that tools can be dynamically added
// via RegisterServerTools and removed via EvictServer.
//
// BDD: Given an empty MCPRegistry,
//
//	When RegisterServerTools is called for "server-a" with two tools,
//	Then All() returns both tools;
//	When EvictServer("server-a") is called,
//	Then All() returns an empty slice.
//
// Traces to: pkg/tools/mcp_registry.go — RegisterServerTools + EvictServer.
func TestMCPRegistry_DynamicAddRemove(t *testing.T) {
	builtins := NewBuiltinRegistry()
	reg := NewMCPRegistry()

	tools := []Tool{
		&mcpTestTool{name: "mcp.read"},
		&mcpTestTool{name: "mcp.write"},
	}
	collisions := reg.RegisterServerTools("server-a", tools, builtins)
	require.Empty(t, collisions, "no collisions expected for fresh registration")

	all := reg.All()
	require.Len(t, all, 2, "All() must return both registered tools")

	reg.EvictServer("server-a")
	assert.Empty(t, reg.All(), "All() must be empty after eviction")
}

// TestMCPRegistry_FirstServerWins verifies the first-server-wins collision rule:
// when two MCP servers register a tool with the same name, the first registration
// wins and a collision is recorded for the second (FR-034).
//
// BDD: Given server-a registers "mcp.search",
//
//	When server-b also tries to register "mcp.search",
//	Then server-b's registration is rejected (collision returned);
//	And Get("mcp.search") still returns server-a's tool.
//
// Traces to: pkg/tools/mcp_registry.go — RegisterServerTools first-server-wins (FR-034).
func TestMCPRegistry_FirstServerWins(t *testing.T) {
	builtins := NewBuiltinRegistry()
	reg := NewMCPRegistry()

	toolA := &mcpTestTool{name: "mcp.search"}
	collisionsA := reg.RegisterServerTools("server-a", []Tool{toolA}, builtins)
	require.Empty(t, collisionsA, "first registration must not produce collisions")

	toolB := &mcpTestTool{name: "mcp.search"}
	collisionsB := reg.RegisterServerTools("server-b", []Tool{toolB}, builtins)
	require.Len(t, collisionsB, 1, "second registration of same name must produce a collision")
	assert.Equal(t, "mcp.search", collisionsB[0].ToolName, "collision must record the conflicting name")
	assert.Equal(t, "server-a", collisionsB[0].ConflictWith, "ConflictWith must name the first (winning) server")

	// server-a's tool must still be accessible.
	got, ok := reg.Get("mcp.search")
	require.True(t, ok, "Get must still return the first-registered tool")
	assert.Equal(t, "mcp.search", got.Name())
}

// TestMCPRegistry_EvictServer_OnlyEvictsTargetServer verifies that evicting one
// server does not remove tools from other servers.
//
// BDD: Given server-a and server-b each register a distinct tool,
//
//	When EvictServer("server-a") is called,
//	Then server-b's tool is still returned by All().
//
// Traces to: pkg/tools/mcp_registry.go — EvictServer (per-server isolation).
func TestMCPRegistry_EvictServer_OnlyEvictsTargetServer(t *testing.T) {
	builtins := NewBuiltinRegistry()
	reg := NewMCPRegistry()

	reg.RegisterServerTools("server-a", []Tool{&mcpTestTool{name: "a.tool"}}, builtins)
	reg.RegisterServerTools("server-b", []Tool{&mcpTestTool{name: "b.tool"}}, builtins)

	reg.EvictServer("server-a")

	all := reg.All()
	require.Len(t, all, 1, "only server-b's tool should remain")
	assert.Equal(t, "b.tool", all[0].Name())
}

// TestMCPRegistry_SystemPrefixRejected verifies that MCP tools with "system."
// prefix are rejected by RegisterServerTools (FR-060).
//
// BDD: Given a builtin registry,
//
//	When RegisterServerTools registers a tool named "system.some_tool",
//	Then a collision is returned (rejected);
//	And All() does not contain the system-prefixed tool.
//
// Traces to: pkg/tools/mcp_registry.go — system prefix guard (FR-060).
func TestMCPRegistry_SystemPrefixRejected(t *testing.T) {
	builtins := NewBuiltinRegistry()
	reg := NewMCPRegistry()

	collisions := reg.RegisterServerTools("mcp-server", []Tool{
		&mcpTestTool{name: "system.hijack"},
		&mcpTestTool{name: "safe.tool"},
	}, builtins)

	require.Len(t, collisions, 1, "one collision for the system-prefix tool")
	assert.Equal(t, "system.hijack", collisions[0].ToolName)

	all := reg.All()
	require.Len(t, all, 1, "only the safe tool should be registered")
	assert.Equal(t, "safe.tool", all[0].Name())
}
