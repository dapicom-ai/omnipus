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
	mcpCaller MCPCaller             // may be nil — MCP discovery is skipped when nil
	auditor   *policy.PolicyAuditor // wraps Evaluator + audit logging (ADR W-3)
	evaluator *policy.Evaluator     // direct evaluator fallback when auditor is nil
	registry  *ToolRegistry
}

// NewToolCompositor creates a ToolCompositor with a PolicyAuditor that
// automatically logs every policy evaluation to the audit log (ADR W-3).
// loader, auditor and registry are required; mcpCaller may be nil.
func NewToolCompositor(
	loader *skills.SkillsLoader,
	mcpCaller MCPCaller,
	auditor *policy.PolicyAuditor,
	registry *ToolRegistry,
) *ToolCompositor {
	return &ToolCompositor{
		loader:    loader,
		mcpCaller: mcpCaller,
		auditor:   auditor,
		registry:  registry,
	}
}

// NewToolCompositorWithEvaluator creates a ToolCompositor with a direct policy
// evaluator (no audit logging). Use NewToolCompositor for production wiring.
// This constructor exists for backward compatibility and testing.
func NewToolCompositorWithEvaluator(
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
	// Use PolicyAuditor (which auto-logs decisions) when available; fall back to
	// direct Evaluator for backward compatibility.
	registered := 0
	for _, dt := range candidates {
		var decision policy.Decision
		if tc.auditor != nil {
			decision = tc.auditor.EvaluateTool(agentID, dt.ToolName)
		} else if tc.evaluator != nil {
			decision = tc.evaluator.EvaluateTool(agentID, dt.ToolName)
		} else {
			// No evaluator configured — deny by default (fail closed).
			decision = policy.Decision{Allowed: false, PolicyRule: "no policy evaluator configured (deny by default)"}
		}
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

// passesScopeGate reports whether a tool with the given scope is structurally
// accessible to agentType. This is the outer guard — it cannot be bypassed by
// policy or visibility configuration.
//
//   - ScopeSystem: only the system agent may use these tools.
//   - ScopeCore: system and core agents pass by default; custom agents require an
//     explicit override (callers check their own visibility/policy layer).
//   - ScopeGeneral: any agent type passes.
//   - Unknown/zero-value scopes: denied (fail-closed, per CLAUDE.md hard constraint 6).
func passesScopeGate(scope ToolScope, agentType string) bool {
	switch scope {
	case ScopeSystem:
		return agentType == "system"
	case ScopeCore:
		return agentType == "system" || agentType == "core"
	case ScopeGeneral:
		return true
	default:
		return false // deny unknown scopes
	}
}

// FilterToolsByVisibility returns the subset of tools that the given agent type
// and tools config allow. It implements a 2-layer scope + visibility filter:
//
//  1. Scope gate: ScopeSystem → only system agents; ScopeCore → system+core
//     agents (custom agents only if explicitly listed); ScopeGeneral → all.
//  2. Explicit mode: if cfg.Mode == "explicit", only tools named in
//     cfg.Visible pass (scope gate still applies as outer guard).
//
// MCP server-level filtering is not yet implemented; all MCP tools that pass
// the scope and visibility gates are returned.
//
// The existing policy evaluator (allow/deny lists) remains a backstop run by
// the caller — this function only handles scope + visibility.
func FilterToolsByVisibility(allTools []Tool, agentType string, cfg *ToolVisibilityCfg) []Tool {
	if cfg == nil {
		cfg = &ToolVisibilityCfg{Mode: "inherit"}
	}

	// Unrecognized mode → treat as "explicit" with empty list (deny all).
	// This is safer than defaulting to "inherit" which would grant MORE access.
	switch cfg.Mode {
	case "explicit", "inherit", "":
		// valid
	default:
		slog.Warn("FilterToolsByVisibility: unrecognized mode, treating as explicit (deny-by-default)",
			"mode", cfg.Mode)
		cfg.Mode = "explicit"
	}

	// Build a fast lookup set for explicit mode. Always build the set when
	// mode is explicit, even if Visible is empty — an empty explicit list
	// means zero tools (deny-by-default per CLAUDE.md hard constraint 6).
	var visibleSet map[string]struct{}
	if cfg.Mode == "explicit" {
		visibleSet = make(map[string]struct{}, len(cfg.Visible))
		for _, name := range cfg.Visible {
			visibleSet[name] = struct{}{}
		}
	}

	out := make([]Tool, 0, len(allTools))
	for _, t := range allTools {
		scope := t.Scope()

		// Layer 1: scope gate based on agent type.
		// For core-scoped tools, custom agents are allowed through only if the
		// tool is explicitly listed in the visibility set (callers set visibleSet
		// when cfg.Mode == "explicit"). This is checked here rather than in
		// passesScopeGate so the helper remains a pure structural gate.
		if scope == ScopeCore && !passesScopeGate(scope, agentType) {
			// Custom (non-system, non-core) agent: only allow if explicitly listed.
			if visibleSet == nil {
				continue
			}
			if _, ok := visibleSet[t.Name()]; !ok {
				continue
			}
		} else if !passesScopeGate(scope, agentType) {
			continue
		}

		// Layer 2: explicit visibility filter.
		if cfg.Mode == "explicit" {
			if _, ok := visibleSet[t.Name()]; !ok {
				continue
			}
		}

		out = append(out, t)
	}
	return out
}

// ToolVisibilityCfg is a simplified view of the agent's tool visibility config,
// used by FilterToolsByVisibility. Callers convert from config.AgentToolsCfg.
type ToolVisibilityCfg struct {
	Mode    string   // "inherit" or "explicit"
	Visible []string // tool names when Mode == "explicit"
}

// ToolPolicyCfg is the per-agent tool policy configuration.
// Used by FilterToolsByPolicy.
type ToolPolicyCfg struct {
	DefaultPolicy string            // "allow", "ask", or "deny"
	Policies      map[string]string // per-tool overrides

	// GlobalPolicies holds the operator-level global tool policy overrides.
	// Applied before agent-level policies; deny always wins (deny > ask > allow).
	GlobalPolicies        map[string]string // per-tool global overrides
	GlobalDefaultPolicy   string            // fallback global policy when tool not in GlobalPolicies
}

// FilterToolsByPolicy returns the subset of tools that pass the scope gate
// and are not denied by policy. Also returns a map of tool name → resolved
// policy ("allow" or "ask") for tools that passed the filter.
// Tools with policy "deny" (globally or by agent config) are removed from the result.
//
// Resolution order (strictest wins: deny > ask > allow):
//  1. Global policy (GlobalPolicies / GlobalDefaultPolicy)
//  2. Agent policy (Policies / DefaultPolicy)
func FilterToolsByPolicy(allTools []Tool, agentType string, cfg *ToolPolicyCfg) ([]Tool, map[string]string) {
	if cfg == nil {
		cfg = &ToolPolicyCfg{DefaultPolicy: "allow"}
	}
	defaultAgentPolicy := cfg.DefaultPolicy
	if defaultAgentPolicy == "" {
		defaultAgentPolicy = "allow"
	}
	defaultGlobalPolicy := cfg.GlobalDefaultPolicy
	if defaultGlobalPolicy == "" {
		defaultGlobalPolicy = "allow"
	}

	resolveGlobal := func(toolName string) string {
		if p, ok := cfg.GlobalPolicies[toolName]; ok {
			return p
		}
		return defaultGlobalPolicy
	}

	resolveAgent := func(toolName string) string {
		if p, ok := cfg.Policies[toolName]; ok {
			return p
		}
		return defaultAgentPolicy
	}

	resolveEffective := func(toolName string) string {
		g := resolveGlobal(toolName)
		a := resolveAgent(toolName)
		if g == "deny" || a == "deny" {
			return "deny"
		}
		if g == "ask" || a == "ask" {
			return "ask"
		}
		return "allow"
	}

	out := make([]Tool, 0, len(allTools))
	policyMap := make(map[string]string)

	for _, t := range allTools {
		scope := t.Scope()

		// Layer 1: scope gate (structural constraint — cannot be bypassed by policy).
		// For core-scoped tools, custom agents are allowed through only if their
		// effective policy is allow or ask (not deny).
		if scope == ScopeCore && !passesScopeGate(scope, agentType) {
			// Custom agent: allowed only if effective policy is not "deny".
			p := resolveEffective(t.Name())
			if p == "deny" {
				continue
			}
		} else if !passesScopeGate(scope, agentType) {
			continue
		}

		// Layer 2: effective policy gate — deny removes the tool entirely.
		effectivePolicy := resolveEffective(t.Name())
		if effectivePolicy == "deny" {
			continue
		}

		out = append(out, t)
		policyMap[t.Name()] = effectivePolicy
	}
	return out, policyMap
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
func (a *mcpToolAdapter) Scope() ToolScope           { return ScopeGeneral }

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
