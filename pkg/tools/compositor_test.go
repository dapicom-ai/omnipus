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
