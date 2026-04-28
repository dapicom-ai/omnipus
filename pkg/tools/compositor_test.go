// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/policy"
	"github.com/dapicom-ai/omnipus/pkg/skills"
)

// --- test doubles ---

// testMCPCaller implements MCPCaller for compositor tests.
type testMCPCaller struct {
	serverTools map[string][]*mcp.Tool
}

func (m *testMCPCaller) GetAllTools() map[string][]*mcp.Tool {
	return m.serverTools
}

func (m *testMCPCaller) CallTool(
	_ context.Context,
	_, _ string,
	_ map[string]any,
) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "mcp result"},
		},
	}, nil
}

// testCompositorAuditLogger records policy decisions for compositor tests.
type testCompositorAuditLogger struct {
	entries []*policy.AuditEntry
}

func (m *testCompositorAuditLogger) LogPolicyDecision(entry *policy.AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

// --- helpers ---

// makeSkillLoader creates a temp workspace with one skill that declares the
// given allowed-tools and returns a SkillsLoader pointing at the workspace.
func makeSkillLoader(t *testing.T, skillName string, toolNames []string) *skills.SkillsLoader {
	t.Helper()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", skillName)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	frontmatter := "---\nname: " + skillName + "\ndescription: Test skill\n"
	if len(toolNames) > 0 {
		frontmatter += "allowed-tools:\n"
		for _, tool := range toolNames {
			frontmatter += "  - " + tool + "\n"
		}
	}
	frontmatter += "---\n# " + skillName + "\n"

	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(frontmatter), 0o644))

	return skills.NewSkillsLoader(dir, "", "")
}

// makeHiddenTool returns a mock Tool registered as hidden in reg.
func makeHiddenTool(reg *ToolRegistry, name string) {
	reg.RegisterHidden(&mockRegistryTool{
		name:   name,
		desc:   "test tool",
		params: map[string]any{"type": "object"},
		result: SilentResult("ok"),
	})
}

// --- tests ---

// TestToolCompositor_ComposeAndRegister_PolicyAuditor_LogsDecisions verifies
// that ComposeAndRegister routes every policy evaluation through the
// PolicyAuditor so audit entries are produced for both allowed and denied tools
// (SEC-17: explainable policy decisions, ADR W-3: auto-logging).
//
// Traces to: wave2-security-layer-spec.md line 184 — allow and deny entries must include policy_rule.
func TestToolCompositor_ComposeAndRegister_PolicyAuditor_LogsDecisions(t *testing.T) {
	const agentID = "researcher"
	const allowedTool = "web_search"
	const deniedTool = "exec"

	loader := makeSkillLoader(t, "my-skill", []string{allowedTool, deniedTool})

	auditLog := &testCompositorAuditLogger{}
	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		Agents: map[string]policy.AgentPolicy{
			agentID: {
				Tools: policy.AgentToolsPolicy{
					Allow: []string{allowedTool},
				},
			},
		},
	}
	eval := policy.NewEvaluator(cfg)
	auditor := policy.NewPolicyAuditor(eval, auditLog, "sess-comp")

	reg := NewToolRegistry()
	makeHiddenTool(reg, allowedTool)

	tc := NewToolCompositor(loader, nil, auditor, reg)
	n := tc.ComposeAndRegister(agentID)

	// Only the allowed tool is promoted.
	assert.Equal(t, 1, n, "only the allowed tool should be registered")

	// Both tools must produce audit entries.
	require.GreaterOrEqual(t, len(auditLog.entries), 2,
		"both allowed and denied tools must produce audit entries")

	// Locate each entry by tool name.
	var allowEntry, denyEntry *policy.AuditEntry
	for _, e := range auditLog.entries {
		switch e.Tool {
		case allowedTool:
			allowEntry = e
		case deniedTool:
			denyEntry = e
		}
	}

	// Differentiation test: allow and deny produce different Decision values.
	require.NotNil(t, allowEntry, "allow entry must exist for %q", allowedTool)
	assert.Equal(t, "allow", allowEntry.Decision)
	assert.NotEmpty(t, allowEntry.PolicyRule, "allow entry must include policy_rule")
	assert.Equal(t, "tool_call", allowEntry.Event)

	require.NotNil(t, denyEntry, "deny entry must exist for %q", deniedTool)
	assert.Equal(t, "deny", denyEntry.Decision)
	assert.NotEmpty(t, denyEntry.PolicyRule, "deny entry must include policy_rule")
}

// TestToolCompositor_ComposeAndRegister_DenyAll verifies that when the policy
// denies all tools, ComposeAndRegister registers nothing.
//
// Traces to: wave2-security-layer-spec.md line 102 — default_policy deny blocks all tools.
func TestToolCompositor_ComposeAndRegister_DenyAll(t *testing.T) {
	const agentID = "locked-agent"

	loader := makeSkillLoader(t, "skill-a", []string{"tool_alpha", "tool_beta"})

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyDeny}
	eval := policy.NewEvaluator(cfg)
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	reg := NewToolRegistry()
	tc := NewToolCompositor(loader, nil, auditor, reg)
	n := tc.ComposeAndRegister(agentID)

	assert.Equal(t, 0, n, "deny-all policy must register zero tools")
}

// TestToolCompositor_ComposeAndRegister_AllowAll verifies that when the policy
// allows all tools, all discovered skill tools are promoted.
//
// Traces to: wave2-security-layer-spec.md line 103 — default_policy allow is Omnipus-compatible.
func TestToolCompositor_ComposeAndRegister_AllowAll(t *testing.T) {
	const agentID = "open-agent"

	loader := makeSkillLoader(t, "skill-b", []string{"tool_x", "tool_y", "tool_z"})

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyAllow}
	eval := policy.NewEvaluator(cfg)
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	reg := NewToolRegistry()
	for _, name := range []string{"tool_x", "tool_y", "tool_z"} {
		makeHiddenTool(reg, name)
	}

	tc := NewToolCompositor(loader, nil, auditor, reg)
	n := tc.ComposeAndRegister(agentID)

	assert.Equal(t, 3, n, "all tools should be promoted when policy is allow-all")
}

// TestToolCompositor_ComposeAndRegister_MCPToolsRegisteredHidden verifies that
// MCP-discovered tools are registered as hidden (require TTL promotion before use).
//
// Traces to: compositor.go — MCP tools registered via RegisterHidden, require PromoteTools.
func TestToolCompositor_ComposeAndRegister_MCPToolsRegisteredHidden(t *testing.T) {
	const agentID = "mcp-agent"

	loader := makeSkillLoader(t, "empty-skill", []string{})

	mcpCaller := &testMCPCaller{
		serverTools: map[string][]*mcp.Tool{
			"github": {
				{Name: "create_issue", Description: "Create GitHub issue"},
			},
		},
	}

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyAllow}
	eval := policy.NewEvaluator(cfg)
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	reg := NewToolRegistry()
	tc := NewToolCompositor(loader, mcpCaller, auditor, reg)
	n := tc.ComposeAndRegister(agentID)

	// The tool is counted as registered.
	assert.Equal(t, 1, n, "MCP tool should be registered")

	// But not visible via Get() until TTL-promoted.
	_, visible := reg.Get("create_issue")
	assert.False(t, visible, "MCP tool must be hidden (TTL=0) until PromoteTools is called")
}

// TestToolCompositor_ComposeAndRegister_MCPTakesPrecedenceOverSKILL verifies
// that when the same tool name appears in both SKILL.md and MCP discovery,
// it is registered only once and the MCP version wins.
//
// Traces to: compositor.go — deduplication block; MCP tools take precedence.
func TestToolCompositor_ComposeAndRegister_MCPTakesPrecedenceOverSKILL(t *testing.T) {
	const agentID = "any-agent"
	const toolName = "shared_tool"

	loader := makeSkillLoader(t, "skill-c", []string{toolName})

	mcpCaller := &testMCPCaller{
		serverTools: map[string][]*mcp.Tool{
			"external-server": {
				{Name: toolName, Description: "MCP version"},
			},
		},
	}

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyAllow}
	eval := policy.NewEvaluator(cfg)
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	reg := NewToolRegistry()
	makeHiddenTool(reg, toolName) // skill-declared hidden tool

	tc := NewToolCompositor(loader, mcpCaller, auditor, reg)
	n := tc.ComposeAndRegister(agentID)

	// Exactly 1 registration — no duplicate.
	assert.Equal(t, 1, n, "duplicate tool name must be deduplicated to exactly one registration")
}

// TestToolCompositor_ComposeAndRegister_NoPolicyEvaluator_DenyByDefault verifies
// that when neither auditor nor evaluator is set, all tools are denied (fail-closed).
//
// Traces to: compositor.go — "no policy evaluator configured (deny by default)".
func TestToolCompositor_ComposeAndRegister_NoPolicyEvaluator_DenyByDefault(t *testing.T) {
	const agentID = "unconfigured-agent"

	loader := makeSkillLoader(t, "skill-d", []string{"some_tool"})

	reg := NewToolRegistry()
	// Both auditor and evaluator are nil — should fail closed.
	tc := &ToolCompositor{
		loader:   loader,
		registry: reg,
	}
	n := tc.ComposeAndRegister(agentID)

	assert.Equal(t, 0, n, "nil policy evaluator must deny all tools (fail-closed)")
}

// TestToolCompositor_ComposeAndRegister_WithEvaluatorFallback verifies that
// NewToolCompositorWithEvaluator (direct Evaluator, no auditor) still gates
// tools correctly. This tests the backward-compatibility path.
//
// Traces to: compositor.go — NewToolCompositorWithEvaluator constructor; evaluator != nil fallback path.
func TestToolCompositor_ComposeAndRegister_WithEvaluatorFallback(t *testing.T) {
	const agentID = "legacy-agent"

	loader := makeSkillLoader(t, "skill-e", []string{"legacy_tool"})

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyAllow}
	eval := policy.NewEvaluator(cfg)

	reg := NewToolRegistry()
	makeHiddenTool(reg, "legacy_tool")

	tc := NewToolCompositorWithEvaluator(loader, nil, eval, reg)
	n := tc.ComposeAndRegister(agentID)

	assert.Equal(t, 1, n, "evaluator fallback path should register approved tools")

	// Differentiation: deny-all config produces 0, not 1.
	reg2 := NewToolRegistry()
	makeHiddenTool(reg2, "legacy_tool")
	eval2 := policy.NewEvaluator(&policy.SecurityConfig{DefaultPolicy: policy.PolicyDeny})
	tc2 := NewToolCompositorWithEvaluator(loader, nil, eval2, reg2)
	n2 := tc2.ComposeAndRegister(agentID)

	assert.Equal(t, 0, n2, "deny-all evaluator fallback must block all tools")
}

// TestMCPToolAdapter_Execute_TextContent verifies that mcpToolAdapter.Execute
// forwards the call through MCPCaller and returns concatenated text content.
//
// Traces to: compositor.go — mcpToolAdapter.Execute and mcpContentText.
func TestMCPToolAdapter_Execute_TextContent(t *testing.T) {
	caller := &testMCPCaller{serverTools: map[string][]*mcp.Tool{}}

	toolDef := &mcp.Tool{
		Name:        "search_code",
		Description: "Search codebase",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
		},
	}
	adapter := newMCPToolAdapter("my-server", toolDef, caller)

	assert.Equal(t, "search_code", adapter.Name(), "adapter name must match tool definition")
	assert.Equal(t, "Search codebase", adapter.Description(), "adapter description must match tool definition")
	assert.NotNil(t, adapter.Parameters(), "adapter parameters must not be nil")

	result := adapter.Execute(context.Background(), map[string]any{"query": "test"})

	assert.False(t, result.IsError, "successful MCP call must not produce error result")
	assert.Equal(t, "mcp result", result.ForLLM, "adapter must return MCPCaller text content")
}

// --- scopedMockTool — configurable-scope mock for FilterToolsByVisibility tests ---

// scopedMockTool is a mock Tool with a user-supplied ToolScope for testing
// FilterToolsByVisibility. The existing mockRegistryTool always returns
// ScopeGeneral; this variant allows tests to configure each tool's scope.
type scopedMockTool struct {
	name  string
	scope ToolScope
}

func (s *scopedMockTool) Name() string               { return s.name }
func (s *scopedMockTool) Description() string        { return "scoped mock tool" }
func (s *scopedMockTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (s *scopedMockTool) Scope() ToolScope           { return s.scope }
func (s *scopedMockTool) RequiresAdminAsk() bool     { return false }
func (s *scopedMockTool) Category() ToolCategory     { return CategoryCore }
func (s *scopedMockTool) Execute(_ context.Context, _ map[string]any) *ToolResult {
	return SilentResult("ok")
}

// makeScopedTool is a helper to create a *scopedMockTool wrapped as Tool.
func makeScopedTool(name string, scope ToolScope) Tool {
	return &scopedMockTool{name: name, scope: scope}
}

// allScopeTools returns a representative tool set covering both scopes (FR-045:
// ScopeSystem was retired; system.* tools now use ScopeCore).
func allScopeTools() []Tool {
	return []Tool{
		makeScopedTool("system.manage_agents", ScopeCore),
		makeScopedTool("exec", ScopeCore),
		makeScopedTool("web_search", ScopeGeneral),
	}
}

// --- FilterToolsByVisibility tests ---

// TestFilterToolsByVisibility_InheritMode_CoreAgent_SeesCoreAndGeneral verifies
// that a core agent in inherit mode sees all ScopeCore tools (including system.*
// named tools) and general tools. FR-045 retired ScopeSystem; system.* tools are
// now ScopeCore and the scope gate passes for agentType=="core".
//
// BDD: Given a core agent with nil/inherit config
//
//	When FilterToolsByVisibility is called with all tools (two ScopeCore, one ScopeGeneral)
//	Then all three tools are returned
//
// Traces to: compositor.go FilterToolsByVisibility — ScopeCore passes for "core" agentType (FR-045).
func TestFilterToolsByVisibility_InheritMode_CoreAgent_SeesCoreAndGeneral(t *testing.T) {
	tools := allScopeTools()
	cfg := &ToolVisibilityCfg{Mode: "inherit"}

	got := FilterToolsByVisibility(tools, "core", cfg)

	require.Len(t, got, 3, "core agent in inherit mode must see both ScopeCore tools and the general tool")

	names := make(map[string]bool, len(got))
	for _, t := range got {
		names[t.Name()] = true
	}
	assert.True(t, names["system.manage_agents"], "system.* tool (ScopeCore) must pass for core agent")
	assert.True(t, names["exec"], "core tool must pass for core agent")
	assert.True(t, names["web_search"], "general tool must pass for core agent")
}

// TestFilterToolsByVisibility_InheritMode_CustomAgent_SeesOnlyGeneral verifies
// that a custom agent in inherit mode sees only ScopeGeneral tools. ScopeCore
// tools (including system.* named tools) require an explicit policy entry.
//
// BDD: Given a custom agent with inherit config
//
//	When FilterToolsByVisibility is called with two ScopeCore tools and one ScopeGeneral
//	Then only the general tool passes
//
// Traces to: compositor.go FilterToolsByVisibility — ScopeCore blocks "custom" without visibleSet
func TestFilterToolsByVisibility_InheritMode_CustomAgent_SeesOnlyGeneral(t *testing.T) {
	tools := allScopeTools()
	cfg := &ToolVisibilityCfg{Mode: "inherit"}

	got := FilterToolsByVisibility(tools, "custom", cfg)

	require.Len(t, got, 1, "custom agent must see only general tools in inherit mode")
	assert.Equal(t, "web_search", got[0].Name(), "only the general-scope tool must pass")
}

// TestFilterToolsByVisibility_ExplicitMode_CustomAgent verifies that a custom
// agent with explicit mode sees exactly the named tools, including a core-scope
// tool if it is listed in Visible (per the spec's "custom agents only if
// explicitly listed" rule).
//
// BDD: Given a custom agent with explicit mode and Visible=["exec","web_search"]
//
//	When FilterToolsByVisibility is called
//	Then both "exec" (core) and "web_search" (general) pass; system tool does not
//
// Traces to: compositor.go FilterToolsByVisibility — ScopeCore custom path + explicit mode layer
func TestFilterToolsByVisibility_ExplicitMode_CustomAgent(t *testing.T) {
	tools := allScopeTools()
	cfg := &ToolVisibilityCfg{
		Mode:    "explicit",
		Visible: []string{"exec", "web_search"},
	}

	got := FilterToolsByVisibility(tools, "custom", cfg)

	require.Len(t, got, 2, "explicit mode must return exactly the two listed tools")

	names := make(map[string]bool, len(got))
	for _, t := range got {
		names[t.Name()] = true
	}
	assert.False(t, names["system.manage_agents"], "system tool must not pass even when agent is custom+explicit")
	assert.True(t, names["exec"], "core tool must pass when explicitly listed for custom agent")
	assert.True(t, names["web_search"], "general tool must pass when explicitly listed")
}

// TestFilterToolsByVisibility_ExplicitMode_CoreToolUnlistedBlocked verifies that
// a custom agent with explicit mode cannot see a ScopeCore tool that is not listed
// in its Visible set, even when Visible contains a different tool.
//
// BDD: Given a custom agent with explicit mode listing only "web_search"
//
//	When FilterToolsByVisibility is called with a ScopeCore tool "exec" not in Visible
//	Then "exec" is blocked; "web_search" passes
//
// Traces to: compositor.go FilterToolsByVisibility — ScopeCore custom agent gate (FR-045).
func TestFilterToolsByVisibility_ExplicitMode_CoreToolUnlistedBlocked(t *testing.T) {
	toolSet := []Tool{
		makeScopedTool("exec", ScopeCore),
		makeScopedTool("web_search", ScopeGeneral),
	}
	cfg := &ToolVisibilityCfg{
		Mode:    "explicit",
		Visible: []string{"web_search"},
	}

	got := FilterToolsByVisibility(toolSet, "custom", cfg)

	// Only the general tool may pass; core-scoped tool not in Visible is blocked.
	require.Len(t, got, 1, "ScopeCore tool not in explicit Visible must be blocked for custom agent")
	assert.Equal(t, "web_search", got[0].Name())
}

// TestFilterToolsByVisibility_NilConfig verifies that passing a nil cfg
// defaults to inherit mode so the function does not panic.
//
// BDD: Given a nil ToolVisibilityCfg
//
//	When FilterToolsByVisibility is called for a custom agent
//	Then it behaves identically to inherit mode
//
// Traces to: compositor.go FilterToolsByVisibility — nil cfg guard at top of function
func TestFilterToolsByVisibility_NilConfig(t *testing.T) {
	tools := allScopeTools()

	// Must not panic.
	got := FilterToolsByVisibility(tools, "custom", nil)

	// Inherit mode for custom: only general passes.
	require.Len(t, got, 1, "nil config must behave as inherit mode")
	assert.Equal(t, "web_search", got[0].Name())
}

// TestFilterToolsByVisibility_EmptyVisibleList verifies that explicit mode with
// an empty Visible list returns zero tools for non-system agents (nothing is
// explicitly named, so nothing can pass the explicit layer).
//
// BDD: Given a custom agent with explicit mode and empty Visible list
//
//	When FilterToolsByVisibility is called
//	Then no tools are returned
//
// Traces to: compositor.go FilterToolsByVisibility — explicit mode with nil visibleSet
func TestFilterToolsByVisibility_EmptyVisibleList(t *testing.T) {
	tools := allScopeTools()
	cfg := &ToolVisibilityCfg{
		Mode:    "explicit",
		Visible: []string{}, // intentionally empty
	}

	got := FilterToolsByVisibility(tools, "custom", cfg)

	// Explicit mode with empty Visible list: deny-by-default (CLAUDE.md hard constraint 6).
	// No tools should pass — the empty visibleSet blocks everything in Layer 2.
	require.Len(t, got, 0, "explicit mode with empty Visible must return zero tools")
}

// TestFilterToolsByVisibility_ExplicitMode_CoreAgent verifies that a core agent
// with explicit mode receives only the tools named in the Visible list, and that
// system-scoped tools are never returned regardless of the explicit list.
//
// BDD: Given a mix of system, core, and general tools,
//
//	When FilterToolsByVisibility is called with agentType="core", Mode="explicit",
//	and Visible=["web_search"],
//	Then only "web_search" is returned (the explicit list narrows the core agent's view),
//	And system tools are never returned.
//
// Traces to: compositor.go FilterToolsByVisibility — explicit mode layer 2 applies to core agents;
// scope gate layer 1 blocks system tools regardless of mode (PR #41 Per-Agent Tool Visibility).
func TestFilterToolsByVisibility_ExplicitMode_CoreAgent(t *testing.T) {
	// Given: one tool per scope (system, core, general)
	input := allScopeTools()
	// system.manage_agents (ScopeCore), exec (ScopeCore), web_search (ScopeGeneral) — FR-045

	cfg := &ToolVisibilityCfg{
		Mode:    "explicit",
		Visible: []string{"web_search"},
	}

	// When: core agent with explicit mode listing only "web_search"
	got := FilterToolsByVisibility(input, "core", cfg)

	// Then: only web_search is returned
	require.Len(t, got, 1, "explicit mode must filter core agent to exactly the listed tool")
	assert.Equal(t, "web_search", got[0].Name(),
		"explicit visible list must be the only tool returned")

	// Then: ScopeCore tools not in explicit Visible are filtered out (layer 2 of filter)
	names := make(map[string]bool, len(got))
	for _, t := range got {
		names[t.Name()] = true
	}
	assert.False(t, names["system.manage_agents"],
		"system.* tool (ScopeCore) not in explicit Visible must be filtered out")
	assert.False(t, names["exec"],
		"core-scoped tool not in explicit Visible must be filtered out")
}

// --- FilterToolsByPolicy tests ---

// allPolicyTools returns a representative set covering both scopes (FR-045:
// ScopeSystem was retired; system.* tools now use ScopeCore).
func allPolicyTools() []Tool {
	return []Tool{
		makeScopedTool("system.agent.list", ScopeCore),
		makeScopedTool("exec", ScopeCore),
		makeScopedTool("web_search", ScopeGeneral),
	}
}

// TestFilterToolsByPolicy_GlobalDeny_RemovesTool verifies that a global "deny"
// on a specific tool removes it from the output regardless of agent policy.
//
// BDD: Given global policy denies "web_search",
// When FilterToolsByPolicy is called for a core agent,
// Then "web_search" is absent from the result.
//
// Traces to: compositor.go FilterToolsByPolicy — resolveEffective deny wins.
func TestFilterToolsByPolicy_GlobalDeny_RemovesTool(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		GlobalPolicies:      map[string]string{"web_search": "deny"},
		GlobalDefaultPolicy: "allow",
	}

	got, policyMap := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	for _, t := range got {
		if t.Name() == "web_search" {
			panic("web_search must be removed when globally denied")
		}
	}
	if _, exists := policyMap["web_search"]; exists {
		panic("denied tool must not appear in policyMap")
	}
}

// TestFilterToolsByPolicy_GlobalAsk_AgentAllow_EffectiveAsk verifies that when
// global policy is "ask" and agent policy is "allow", the effective result is "ask"
// (strictest wins: ask > allow).
//
// BDD: Given global policy "ask" for "web_search" and agent policy "allow",
// When FilterToolsByPolicy is called,
// Then "web_search" is in the result with policy "ask".
//
// Traces to: compositor.go resolveEffective — ask > allow.
func TestFilterToolsByPolicy_GlobalAsk_AgentAllow_EffectiveAsk(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		Policies:            map[string]string{"web_search": "allow"},
		GlobalPolicies:      map[string]string{"web_search": "ask"},
		GlobalDefaultPolicy: "allow",
	}

	_, policyMap := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	if p, ok := policyMap["web_search"]; !ok || p != "ask" {
		t.Errorf("expected effective policy 'ask' for web_search, got %q (ok=%v)", p, ok)
	}
}

// TestFilterToolsByPolicy_GlobalAllow_AgentDeny_EffectiveDeny verifies that
// agent-level "deny" wins over global "allow".
//
// BDD: Given global policy "allow" and agent policy "deny" for "web_search",
// When FilterToolsByPolicy is called,
// Then "web_search" is absent from the result.
//
// Traces to: compositor.go resolveEffective — deny always wins.
func TestFilterToolsByPolicy_GlobalAllow_AgentDeny_EffectiveDeny(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		Policies:            map[string]string{"web_search": "deny"},
		GlobalPolicies:      map[string]string{},
		GlobalDefaultPolicy: "allow",
	}

	got, _ := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	for _, tool := range got {
		if tool.Name() == "web_search" {
			t.Error("web_search must be absent when agent policy is deny")
		}
	}
}

// TestFilterToolsByPolicy_GlobalAllow_AgentAsk_EffectiveAsk verifies that
// agent "ask" + global "allow" yields "ask".
//
// BDD: Given global "allow" and agent "ask" for "web_search",
// When FilterToolsByPolicy is called,
// Then "web_search" is in the result with policy "ask".
//
// Traces to: compositor.go resolveEffective — ask > allow.
func TestFilterToolsByPolicy_GlobalAllow_AgentAsk_EffectiveAsk(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		Policies:            map[string]string{"web_search": "ask"},
		GlobalPolicies:      map[string]string{},
		GlobalDefaultPolicy: "allow",
	}

	_, policyMap := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	if p, ok := policyMap["web_search"]; !ok || p != "ask" {
		t.Errorf("expected effective policy 'ask' for web_search, got %q (ok=%v)", p, ok)
	}
}

// TestFilterToolsByPolicy_AllAllow verifies that global "allow" + agent "allow"
// yields effective "allow".
//
// BDD: Given both global and agent policies are "allow" for "web_search",
// When FilterToolsByPolicy is called,
// Then "web_search" is in the result with policy "allow".
//
// Traces to: compositor.go resolveEffective — allow + allow = allow.
func TestFilterToolsByPolicy_AllAllow(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		Policies:            map[string]string{"web_search": "allow"},
		GlobalPolicies:      map[string]string{"web_search": "allow"},
		GlobalDefaultPolicy: "allow",
	}

	_, policyMap := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	if p, ok := policyMap["web_search"]; !ok || p != "allow" {
		t.Errorf("expected effective policy 'allow' for web_search, got %q (ok=%v)", p, ok)
	}
}

// TestFilterToolsByPolicy_SystemWildcardDeny_BlocksSystemTools verifies that a
// per-agent "system.*: deny" wildcard policy blocks system.* tools even though
// they are ScopeCore (FR-045: ScopeSystem retired; per-agent policy is the sole gate).
//
// BDD: Given a core agent with Policies={"system.*": "deny"} and defaultPolicy "allow",
// When FilterToolsByPolicy is called,
// Then "system.agent.list" is absent from the result (wildcard deny wins).
//
// Traces to: compositor.go FilterToolsByPolicy — resolveEffective deny wins; wildcard index (FR-045).
func TestFilterToolsByPolicy_SystemWildcardDeny_BlocksSystemTools(t *testing.T) {
	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		Policies:            map[string]string{"system.*": "deny"},
		GlobalDefaultPolicy: "allow",
	}

	got, _ := FilterToolsByPolicy(allPolicyTools(), "core", cfg)

	for _, tool := range got {
		if tool.Name() == "system.agent.list" {
			t.Error("system.* tool must be blocked when Policies[\"system.*\"]=deny")
		}
	}
}

// TestFilterToolsByPolicy_ScopeCore_BlockedForCustomUnlessExplicit verifies that
// a ScopeCore tool is blocked for a custom agent when the effective policy is
// "deny", but allowed when the effective policy is "allow".
//
// BDD: Given a core-scoped tool "exec" and a "custom" agent,
// When global policy is "allow" and agent policy is "deny" for "exec",
// Then "exec" is absent from the result.
// When both policies are "allow",
// Then "exec" is in the result.
//
// Traces to: compositor.go FilterToolsByPolicy — ScopeCore custom agent gate checks effective policy.
func TestFilterToolsByPolicy_ScopeCore_CustomAgent(t *testing.T) {
	// Custom agent + deny policy for exec → blocked
	denyCfg := &ToolPolicyCfg{
		DefaultPolicy:       "deny",
		GlobalDefaultPolicy: "allow",
	}
	got, _ := FilterToolsByPolicy(allPolicyTools(), "custom", denyCfg)
	for _, tool := range got {
		if tool.Name() == "exec" {
			t.Error("core-scoped tool must be blocked for custom agent with deny policy")
		}
	}

	// Custom agent + allow policy for exec → allowed through
	allowCfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		GlobalDefaultPolicy: "allow",
	}
	_, policyMap := FilterToolsByPolicy(allPolicyTools(), "custom", allowCfg)
	if _, ok := policyMap["exec"]; !ok {
		t.Error("core-scoped tool must be allowed for custom agent with allow policy")
	}
}

// TestFilterToolsByPolicy_EmptyConfig_DefaultsToAllow verifies that a nil config
// defaults to allow-all (the safe default for non-security-critical agents).
//
// BDD: Given a nil ToolPolicyCfg,
// When FilterToolsByPolicy is called for a core agent,
// Then all ScopeCore + ScopeGeneral tools pass with "allow" policy (FR-045).
//
// Traces to: compositor.go FilterToolsByPolicy — nil cfg defaults to allow.
func TestFilterToolsByPolicy_EmptyConfig_DefaultsToAllow(t *testing.T) {
	got, policyMap := FilterToolsByPolicy(allPolicyTools(), "core", nil)

	// All tools must be present for core agent with nil config.
	if len(got) != 3 {
		t.Errorf("expected 3 tools for core agent with nil config, got %d", len(got))
	}
	for _, name := range []string{"system.agent.list", "exec", "web_search"} {
		if p, ok := policyMap[name]; !ok || p != "allow" {
			t.Errorf("expected policy 'allow' for %q, got %q (ok=%v)", name, p, ok)
		}
	}
}

// TestFilterToolsByPolicy_UnknownScope_Denied verifies that a tool with an
// unknown/zero-value scope is denied (fail-closed, CLAUDE.md hard constraint 6).
//
// BDD: Given a tool with scope "" (unknown),
// When FilterToolsByPolicy is called,
// Then the tool is absent from the result regardless of policy.
//
// Traces to: compositor.go FilterToolsByPolicy — passesScopeGate returns false for unknown scope.
func TestFilterToolsByPolicy_UnknownScope_Denied(t *testing.T) {
	unknownScopeTool := makeScopedTool("mystery_tool", ToolScope("unknown"))
	tools := []Tool{unknownScopeTool, makeScopedTool("web_search", ScopeGeneral)}

	cfg := &ToolPolicyCfg{
		DefaultPolicy:       "allow",
		GlobalDefaultPolicy: "allow",
	}

	got, _ := FilterToolsByPolicy(tools, "core", cfg)

	for _, tool := range got {
		if tool.Name() == "mystery_tool" {
			t.Error("tool with unknown scope must be denied (fail-closed)")
		}
	}
	// web_search with ScopeGeneral must still pass
	found := false
	for _, tool := range got {
		if tool.Name() == "web_search" {
			found = true
		}
	}
	if !found {
		t.Error("general-scope tool must still pass alongside an unknown-scope tool")
	}
}

// TestMCPContentText_ConcatenatesTextContent verifies that mcpContentText
// joins TextContent entries without a separator and silently skips non-text items.
//
// Traces to: compositor.go mcpContentText.
func TestMCPContentText_ConcatenatesTextContent(t *testing.T) {
	tests := []struct {
		name     string
		content  []mcp.Content
		expected string
	}{
		{
			name:     "nil content returns empty string",
			content:  nil,
			expected: "",
		},
		{
			name:     "single text item",
			content:  []mcp.Content{&mcp.TextContent{Text: "hello"}},
			expected: "hello",
		},
		{
			// Differentiation: two different multi-item inputs produce different outputs.
			name: "multiple text items are concatenated without separator",
			content: []mcp.Content{
				&mcp.TextContent{Text: "first"},
				&mcp.TextContent{Text: "second"},
			},
			expected: "firstsecond",
		},
		{
			name: "non-text content is skipped",
			content: []mcp.Content{
				&mcp.TextContent{Text: "text only"},
				&mcp.ImageContent{Data: []byte("img"), MIMEType: "image/png"},
			},
			expected: "text only",
		},
		{
			name:     "all non-text content produces empty string",
			content:  []mcp.Content{&mcp.ImageContent{Data: []byte("img"), MIMEType: "image/jpeg"}},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mcpContentText(tc.content)
			assert.Equal(t, tc.expected, got)
		})
	}
}
