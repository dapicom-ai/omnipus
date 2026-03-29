// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dapicom-ai/omnipus/pkg/policy"
)

// makeEvaluator builds an Evaluator from a default policy string and optional agent map.
func makeEvaluator(defaultPolicy policy.DefaultPolicy, agents map[string]policy.AgentPolicy) *policy.Evaluator {
	return policy.NewEvaluator(&policy.SecurityConfig{
		DefaultPolicy: defaultPolicy,
		Agents:        agents,
	})
}

// TestPolicyEvaluator_DenyByDefault validates that deny-by-default blocks all tool calls
// when an agent has no tools.allow list configured.
// Traces to: wave2-security-layer-spec.md line 788 (TestPolicyEvaluator_DenyByDefault)
// BDD: Scenario: No tools available without explicit allow (spec line 529)
func TestPolicyEvaluator_DenyByDefault(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 529 (Scenario: No tools without explicit allow)
	evaluator := makeEvaluator(policy.PolicyDeny, nil) // no agent-specific policies

	tests := []struct {
		name    string
		agentID string
		tool    string
	}{
		{name: "web_search denied without allow list", agentID: "researcher", tool: "web_search"},
		{name: "exec denied without allow list", agentID: "assistant", tool: "exec"},
		{name: "file.write denied without allow list", agentID: "researcher", tool: "file.write"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := evaluator.EvaluateTool(tc.agentID, tc.tool)
			assert.False(t, decision.Allowed,
				"tool %q should be denied with deny-by-default and no allow list", tc.tool)
			assert.Contains(t, decision.PolicyRule, "default_policy is 'deny'")
			assert.Contains(t, decision.PolicyRule, tc.tool)
		})
	}
}

// TestPolicyEvaluator_AllowByDefault validates explicit allow-by-default behavior.
// Traces to: wave2-security-layer-spec.md line 789 (TestPolicyEvaluator_AllowByDefault)
// BDD: Scenario: Backward-compatible allow-by-default (spec line 539)
func TestPolicyEvaluator_AllowByDefault(t *testing.T) {
	t.Run("explicit allow default", func(t *testing.T) {
		evaluator := makeEvaluator(policy.PolicyAllow, nil)
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.True(t, decision.Allowed,
			"tool should be allowed with explicit default_policy=allow")
		assert.Contains(t, decision.PolicyRule, "allow",
			"allow decision should explain why it was allowed")
	})

	t.Run("empty string defaults to deny", func(t *testing.T) {
		// Deny-by-default per CLAUDE.md hard constraint #6
		evaluator := makeEvaluator("", nil)
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.False(t, decision.Allowed,
			"tool should be denied with empty default_policy (deny-by-default)")
		assert.Contains(t, decision.PolicyRule, "deny",
			"deny decision should explain why it was denied")
	})
}

// TestPolicyEvaluator_ToolAllowList validates that tools not in the allow list are denied.
// Traces to: wave2-security-layer-spec.md line 790 (TestPolicyEvaluator_ToolAllowList)
// BDD: Scenario: Tool not in allow list is denied (spec line 465)
func TestPolicyEvaluator_ToolAllowList(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 465 (Scenario: Tool not in allow list)
	agents := map[string]policy.AgentPolicy{
		"researcher": {
			Tools: policy.AgentToolsPolicy{
				Allow: []string{"web_search", "web_fetch"},
			},
		},
	}
	evaluator := makeEvaluator(policy.PolicyAllow, agents)

	t.Run("web_search in allow list passes", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.True(t, decision.Allowed)
		assert.Contains(t, decision.PolicyRule, "web_search")
		assert.Contains(t, decision.PolicyRule, "researcher")
	})

	t.Run("web_fetch in allow list passes", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "web_fetch")
		assert.True(t, decision.Allowed)
	})

	t.Run("exec not in allow list is denied", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 465 (BDD: exec not in tools.allow)
		decision := evaluator.EvaluateTool("researcher", "exec")
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.PolicyRule, "exec")
		assert.Contains(t, decision.PolicyRule, "researcher")
	})

	t.Run("file.write not in allow list is denied", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "file.write")
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.PolicyRule, "not in tools.allow")
	})

	t.Run("empty tools.allow denies all tools", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 304 (empty tools.allow = allow nothing)
		agentsEmpty := map[string]policy.AgentPolicy{
			"researcher": {
				Tools: policy.AgentToolsPolicy{
					Allow: []string{}, // explicit empty = no tools
				},
			},
		}
		ev := makeEvaluator(policy.PolicyAllow, agentsEmpty)
		decision := ev.EvaluateTool("researcher", "web_search")
		assert.False(t, decision.Allowed,
			"explicit empty tools.allow should deny all tools even if default is allow")
	})
}

// TestPolicyEvaluator_ToolDenyList validates that tools in the deny list are blocked
// even when default_policy is "allow".
// Traces to: wave2-security-layer-spec.md line 791 (TestPolicyEvaluator_ToolDenyList)
// BDD: Scenario: Tool in deny list is blocked even if default is allow (spec line 476)
func TestPolicyEvaluator_ToolDenyList(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 476 (Scenario: Tool in deny list blocked)
	agents := map[string]policy.AgentPolicy{
		"researcher": {
			Tools: policy.AgentToolsPolicy{
				Deny: []string{"exec", "file.write"},
			},
		},
	}
	evaluator := makeEvaluator(policy.PolicyAllow, agents)

	t.Run("exec in deny list is blocked", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "exec")
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.PolicyRule, "exec")
		assert.Contains(t, decision.PolicyRule, "tools.deny")
	})

	t.Run("file.write in deny list is blocked", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "file.write")
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.PolicyRule, "tools.deny")
	})

	t.Run("web_search not in deny list is allowed", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.True(t, decision.Allowed)
	})
}

// TestPolicyEvaluator_DenyPrecedence validates deny takes precedence when a tool
// appears in both tools.allow and tools.deny.
// Traces to: wave2-security-layer-spec.md line 792 (TestPolicyEvaluator_DenyPrecedence)
// BDD: Scenario: Deny takes precedence over allow (spec line 485)
func TestPolicyEvaluator_DenyPrecedence(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 485 (Scenario: Deny takes precedence)
	agents := map[string]policy.AgentPolicy{
		"researcher": {
			Tools: policy.AgentToolsPolicy{
				Allow: []string{"exec", "web_search"},
				Deny:  []string{"exec"}, // exec in both → deny wins
			},
		},
	}
	evaluator := makeEvaluator(policy.PolicyAllow, agents)

	t.Run("exec in both allow and deny is blocked (deny wins)", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "exec")
		assert.False(t, decision.Allowed,
			"deny must take precedence over allow when both lists contain the tool")
		assert.Contains(t, decision.PolicyRule, "deny takes precedence")
	})

	t.Run("web_search only in allow is still permitted", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.True(t, decision.Allowed)
	})
}

// TestPolicyEvaluator_ExplainableDecision validates every policy decision includes
// a human-readable policy_rule explaining the match.
// Traces to: wave2-security-layer-spec.md line 793 (TestPolicyEvaluator_ExplainableDecision)
// BDD: Scenario: Denial includes matching rule + Allow includes matching rule (spec line 623–640)
func TestPolicyEvaluator_ExplainableDecision(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 623 (Scenario: Denial includes matching rule)
	agents := map[string]policy.AgentPolicy{
		"researcher": {
			Tools: policy.AgentToolsPolicy{
				Allow: []string{"web_search"},
			},
		},
	}
	evaluator := makeEvaluator(policy.PolicyDeny, agents)

	t.Run("deny decision includes full policy_rule string", func(t *testing.T) {
		decision := evaluator.EvaluateTool("researcher", "exec")
		assert.False(t, decision.Allowed)
		assert.NotEmpty(t, decision.PolicyRule,
			"denied decision must include a non-empty policy_rule")
		assert.Contains(t, decision.PolicyRule, "exec",
			"policy_rule must reference the denied tool")
		assert.Contains(t, decision.PolicyRule, "researcher",
			"policy_rule must reference the agent")
	})

	t.Run("allow decision includes policy_rule string", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 633 (Scenario: Allow includes matching rule)
		decision := evaluator.EvaluateTool("researcher", "web_search")
		assert.True(t, decision.Allowed)
		assert.NotEmpty(t, decision.PolicyRule,
			"allowed decision must also include a policy_rule")
		assert.Contains(t, decision.PolicyRule, "web_search")
	})

	t.Run("deny by default includes policy_rule", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 186 (multiple rules: first match wins)
		ev := makeEvaluator(policy.PolicyDeny, nil)
		decision := ev.EvaluateTool("researcher", "exec")
		assert.False(t, decision.Allowed)
		assert.NotEmpty(t, decision.PolicyRule)
	})
}
