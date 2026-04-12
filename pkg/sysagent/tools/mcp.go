// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"log/slog"

	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// ---- system.mcp.add ----

type MCPAddTool struct{ deps *Deps }

func NewMCPAddTool(d *Deps) *MCPAddTool    { return &MCPAddTool{deps: d} }
func (t *MCPAddTool) Name() string          { return "system.mcp.add" }
func (t *MCPAddTool) Scope() tools.ToolScope { return tools.ScopeSystem }
func (t *MCPAddTool) Description() string {
	return "Add an MCP server.\nParameters: name (required), transport (stdio/sse/http), command, args, url, env, agent_ids."
}

func (t *MCPAddTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      map[string]any{"type": "string"},
			"transport": map[string]any{"type": "string"},
			"command":   map[string]any{"type": "string"},
			"args":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"url":       map[string]any{"type": "string"},
			"env":       map[string]any{"type": "object"},
			"agent_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"name", "transport"},
	}
}

func (t *MCPAddTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	transport, _ := args["transport"].(string)
	if name == "" || transport == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name and transport are required", ""))
	}
	slog.Info("sysagent: stub tool invoked", "tool", "system.mcp.add", "name", name)
	return tools.NewToolResult(successJSON(map[string]any{
		"name":   name,
		"status": "stub",
		"note":   "not yet implemented — this is a placeholder response",
	}))
}

// ---- system.mcp.remove ----

type MCPRemoveTool struct{ deps *Deps }

func NewMCPRemoveTool(d *Deps) *MCPRemoveTool { return &MCPRemoveTool{deps: d} }
func (t *MCPRemoveTool) Name() string              { return "system.mcp.remove" }
func (t *MCPRemoveTool) Scope() tools.ToolScope    { return tools.ScopeSystem }
func (t *MCPRemoveTool) Description() string {
	return "Remove an MCP server. Parameters: name (required), confirm (bool, must be true)."
}

func (t *MCPRemoveTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"confirm": map[string]any{"type": "boolean"},
		},
		"required": []string{"name", "confirm"},
	}
}

func (t *MCPRemoveTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	confirm, _ := args["confirm"].(bool)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	if !confirm {
		return tools.ErrorResult(errorJSON("CONFIRMATION_REQUIRED",
			"confirm must be true to remove an MCP server", ""))
	}
	slog.Info("sysagent: stub tool invoked", "tool", "system.mcp.remove", "name", name)
	return tools.NewToolResult(successJSON(map[string]any{
		"name":            name,
		"status":          "stub",
		"agents_affected": []string{},
		"note":            "not yet implemented — this is a placeholder response",
	}))
}

// ---- system.mcp.list ----

type MCPListTool struct{ deps *Deps }

func NewMCPListTool(d *Deps) *MCPListTool { return &MCPListTool{deps: d} }
func (t *MCPListTool) Name() string           { return "system.mcp.list" }
func (t *MCPListTool) Scope() tools.ToolScope  { return tools.ScopeSystem }
func (t *MCPListTool) Description() string {
	return "List all MCP servers with status and assigned agents. No parameters required."
}

func (t *MCPListTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *MCPListTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	slog.Info("sysagent: stub tool invoked", "tool", "system.mcp.list")
	return tools.NewToolResult(successJSON(map[string]any{
		"servers": []any{},
		"status":  "stub",
		"note":    "not yet implemented — this is a placeholder response",
	}))
}
