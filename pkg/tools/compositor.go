// Package tools — tool composition point (architect review B-2).
//
// ToolCompositor merges discovery from installed skills (SKILL.md) and MCP
// servers, applies the policy engine, and registers approved tools into a
// ToolRegistry.  This is the canonical wiring between auto-discovery and the
// agent loop; previously DiscoverAllTools and MCPBridge.DiscoverMCPTools
// produced results that nothing consumed.
package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dapicom-ai/omnipus/pkg/policy"
	"github.com/dapicom-ai/omnipus/pkg/skills"
)

// MCPCaller is the interface used by ToolCompositor for MCP tool discovery and
// execution.  pkg/mcp.Manager satisfies this interface; a test double may be
// used in tests.
type MCPCaller interface {
	GetAllTools() map[string][]*mcp.Tool
	CallTool(ctx context.Context, serverName, toolName string, arguments map[string]any) (*mcp.CallToolResult, error)
}

// ToolCompositor merges tool discovery from installed skills (SKILL.md
// allowed-tools) and MCP servers, applies the policy engine for a given agent,
// and registers approved tools into a ToolRegistry.
//
// Call ComposeAndRegister after skill installation or MCP server state changes.
type ToolCompositor struct {
	loader    *skills.SkillsLoader
	mcpCaller MCPCaller       // may be nil — MCP discovery is skipped when nil
	evaluator *policy.Evaluator
	registry  *ToolRegistry
}

// NewToolCompositor creates a ToolCompositor.
// loader, evaluator and registry are required; mcpCaller may be nil.
func NewToolCompositor(
	loader *skills.SkillsLoader,
	mcpCaller MCPCaller,
	evaluator *policy.Evaluator,
	registry *ToolRegistry,
) *ToolCompositor {
	return &ToolCompositor{
		loader:    loader,
		mcpCaller: mcpCaller,
		evaluator: evaluator,
		registry:  registry,
	}
}

// mcpToolRef pairs an mcp.Tool definition with the server name that provides it.
type mcpToolRef struct {
	serverName string
	tool       *mcp.Tool
}

// ComposeAndRegister discovers tools from installed skills and connected MCP
// servers, deduplicates by tool name (MCP tools take precedence over SKILL.md
// references), gates each via the policy engine for agentID, and registers
// approved tools into the ToolRegistry.
//
//   - MCP tools are wrapped in mcpToolAdapter and registered as hidden tools
//     (require PromoteTools + TTL before the agent loop exposes them).
//   - SKILL.md-declared tool names are promoted in the registry with TTL=1
//     (only if already registered as hidden tools).
//
// Returns the number of tools registered or promoted.
func (tc *ToolCompositor) ComposeAndRegister(agentID string) int {
	// Step 1: collect candidates from both sources.
	fileDiscovered := skills.DiscoverAllTools(tc.loader)

	mcpByName := make(map[string]*mcpToolRef)
	var mcpDiscovered []skills.DiscoveredTool
	if tc.mcpCaller != nil {
		for serverName, serverTools := range tc.mcpCaller.GetAllTools() {
			for _, t := range serverTools {
				if t == nil || t.Name == "" {
					continue
				}
				mcpByName[t.Name] = &mcpToolRef{serverName: serverName, tool: t}
				mcpDiscovered = append(mcpDiscovered, skills.DiscoveredTool{
					SkillName: serverName,
					ToolName:  t.Name,
					Source:    "mcp:" + serverName,
				})
			}
		}
	}

	// Step 2: deduplicate by tool name — MCP tools take precedence.
	seen := make(map[string]struct{}, len(mcpDiscovered)+len(fileDiscovered))
	candidates := make([]skills.DiscoveredTool, 0, len(mcpDiscovered)+len(fileDiscovered))
	for _, dt := range mcpDiscovered {
		if _, dup := seen[dt.ToolName]; dup {
			continue
		}
		seen[dt.ToolName] = struct{}{}
		candidates = append(candidates, dt)
	}
	for _, dt := range fileDiscovered {
		if _, dup := seen[dt.ToolName]; dup {
			continue
		}
		seen[dt.ToolName] = struct{}{}
		candidates = append(candidates, dt)
	}

	// Step 3: evaluate policy and register/promote approved tools.
	registered := 0
	for _, dt := range candidates {
		decision := tc.evaluator.EvaluateTool(agentID, dt.ToolName)
		if !decision.Allowed {
			slog.Debug("tool compositor: tool blocked by policy",
				"agent", agentID,
				"tool", dt.ToolName,
				"rule", decision.PolicyRule,
			)
			continue
		}

		if ref, isMCP := mcpByName[dt.ToolName]; isMCP {
			tc.registry.RegisterHidden(newMCPToolAdapter(ref.serverName, ref.tool, tc.mcpCaller))
			slog.Debug("tool compositor: registered MCP tool",
				"agent", agentID,
				"tool", dt.ToolName,
				"server", ref.serverName,
			)
		} else {
			// Promote existing hidden tool that the skill declared.
			tc.registry.PromoteTools([]string{dt.ToolName}, 1)
			slog.Debug("tool compositor: promoted skill-declared tool",
				"agent", agentID,
				"tool", dt.ToolName,
				"skill", dt.SkillName,
			)
		}
		registered++
	}

	slog.Info("tool compositor: composition complete",
		"agent", agentID,
		"candidates", len(candidates),
		"registered", registered,
	)
	return registered
}

// mcpToolAdapter wraps a single MCP tool as a Tool, forwarding Execute calls
// through the MCPCaller.  It is registered as a hidden tool (requires TTL
// promotion before the agent loop exposes it).
type mcpToolAdapter struct {
	serverName string
	toolDef    *mcp.Tool
	caller     MCPCaller
	params     map[string]any
}

func newMCPToolAdapter(serverName string, toolDef *mcp.Tool, caller MCPCaller) *mcpToolAdapter {
	params := make(map[string]any)
	if toolDef.InputSchema != nil {
		if m, ok := toolDef.InputSchema.(map[string]any); ok {
			params = m
		}
	}
	return &mcpToolAdapter{
		serverName: serverName,
		toolDef:    toolDef,
		caller:     caller,
		params:     params,
	}
}

func (a *mcpToolAdapter) Name() string               { return a.toolDef.Name }
func (a *mcpToolAdapter) Description() string        { return a.toolDef.Description }
func (a *mcpToolAdapter) Parameters() map[string]any { return a.params }

func (a *mcpToolAdapter) Execute(ctx context.Context, args map[string]any) *ToolResult {
	result, err := a.caller.CallTool(ctx, a.serverName, a.toolDef.Name, args)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP tool %q failed: %v", a.toolDef.Name, err))
	}
	text := mcpContentText(result.Content)
	if result.IsError {
		return ErrorResult(fmt.Sprintf("MCP tool %q error: %s", a.toolDef.Name, text))
	}
	return SilentResult(text)
}

// mcpContentText concatenates all TextContent entries from an MCP Content slice.
func mcpContentText(content []mcp.Content) string {
	var sb strings.Builder
	for _, c := range content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
