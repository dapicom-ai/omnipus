package security_test

// File purpose: Sprint J issue #79 — tool-name glob policy routing integration tests.
//
// These tests prove that the real policy.Evaluator (pkg/policy) correctly routes
// allow/deny decisions based on glob patterns in agent tools.allow and tools.deny,
// with the invariant that explicit deny beats glob allow.
//
// All test cases are table-driven against the specification matrix in the sprint-j
// prompt §3, plus boundary and edge cases (? wildcard, prefix-only, global glob).
//
// Traces to: sprint-j-security-hardening prompt §3 (policy glob table).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/policy"
)

// policyGlobCase describes one row in the policy glob test matrix.
// Maps directly to the specification table in sprint-j prompt §3.
type policyGlobCase struct {
	name string
	// globalPolicies mirrors security.tool_policies in config.
	globalPolicies map[string]policy.ToolPolicy
	// agentAllow is agent.tools.allow list.
	agentAllow []string
	// agentDeny is agent.tools.deny list.
	agentDeny []string
	// tool is the tool name being evaluated.
	tool string
	// wantDecision is the expected allow/deny decision.
	wantDecision bool
	// wantPolicy is the expected policy string ("allow", "deny", "ask").
	wantPolicy string
	// defaultPolicy overrides the SecurityConfig default policy for this case.
	// When empty, PolicyDeny is used (the project's secure default).
	defaultPolicy policy.DefaultPolicy
}

// TestPolicyGlob_ToolNameGlobRouting exercises every row from the sprint-j spec §3
// table against the real policy.Evaluator. This is the canonical regression test
// for tool-name glob matching and explicit-deny-beats-glob semantics.
//
// Traces to: sprint-j prompt §3 (policy glob table, 12 rows).
func TestPolicyGlob_ToolNameGlobRouting(t *testing.T) {
	// Spec table from sprint-j prompt §3.
	// Rows are numbered to match the spec; each has a name that mirrors the table.
	cases := []policyGlobCase{
		// Row 1: exact allow
		{
			name:         "exact_allow_web_search",
			agentAllow:   []string{"web_search"},
			agentDeny:    nil,
			tool:         "web_search",
			wantDecision: true,
			wantPolicy:   "allow",
		},
		// Row 2: exact deny beats exact allow
		{
			name:         "exact_deny_beats_exact_allow",
			agentAllow:   []string{"web_search"},
			agentDeny:    []string{"web_search"},
			tool:         "web_search",
			wantDecision: false,
			wantPolicy:   "deny",
		},
		// Row 3: glob allow — fs.read matches fs.*
		{
			name:         "glob_allow_fs_read",
			agentAllow:   []string{"fs.*"},
			agentDeny:    nil,
			tool:         "fs.read",
			wantDecision: true,
			wantPolicy:   "allow",
		},
		// Row 4: glob allow — fs.delete matches fs.*
		{
			name:         "glob_allow_fs_delete",
			agentAllow:   []string{"fs.*"},
			agentDeny:    nil,
			tool:         "fs.delete",
			wantDecision: true,
			wantPolicy:   "allow",
		},
		// Row 5: glob allow + explicit deny — fs.delete in deny beats fs.* allow
		{
			name:         "glob_allow_explicit_deny_fs_delete",
			agentAllow:   []string{"fs.*"},
			agentDeny:    []string{"fs.delete"},
			tool:         "fs.delete",
			wantDecision: false,
			wantPolicy:   "deny",
		},
		// Row 6: glob allow + explicit deny — fs.write still allowed (only fs.delete denied)
		{
			name:         "glob_allow_explicit_deny_fs_write_allowed",
			agentAllow:   []string{"fs.*"},
			agentDeny:    []string{"fs.delete"},
			tool:         "fs.write",
			wantDecision: true,
			wantPolicy:   "allow",
		},
		// Row 7: glob no-match — shell not in fs.* → deny by default
		{
			name:         "glob_no_match_shell_denied",
			agentAllow:   []string{"fs.*"},
			agentDeny:    nil,
			tool:         "shell",
			wantDecision: false,
			wantPolicy:   "deny",
		},
		// Row 8: global glob ask — fs.read matches global {fs.*: ask}.
		// The global "ask" floor elevates an otherwise "allow" decision to "ask".
		// Uses default_policy=allow so evaluateDefault returns allow, then the
		// global floor raises it to ask. With default_policy=deny the deny wins
		// (the global ask floor only applies when the agent's own evaluation
		// says "allow"). Matches pkg/policy/evaluator.go lines 87-95.
		{
			name: "global_glob_ask_fs_read",
			globalPolicies: map[string]policy.ToolPolicy{
				"fs.*": policy.ToolPolicyAsk,
			},
			agentAllow:    nil,
			agentDeny:     nil,
			tool:          "fs.read",
			defaultPolicy: policy.PolicyAllow, // ask floor requires allow as default
			wantDecision:  true,               // ask = allowed but requires confirmation
			wantPolicy:    "ask",
		},
		// Row 9: global glob ask + agent explicit allow — agent allow elevates above ask → allow
		{
			name: "global_glob_ask_agent_allow_elevates",
			globalPolicies: map[string]policy.ToolPolicy{
				"fs.*": policy.ToolPolicyAsk,
			},
			agentAllow:   []string{"fs.read"},
			agentDeny:    nil,
			tool:         "fs.read",
			wantDecision: true,
			wantPolicy:   "allow", // agent explicit allow wins over global ask floor
		},
		// Row 10: prefix-only (no wildcard) — "fs" does not match "fs.read"
		{
			name:         "prefix_only_no_wildcard_does_not_match",
			agentAllow:   []string{"fs"},
			agentDeny:    nil,
			tool:         "fs.read",
			wantDecision: false, // "fs" is not a glob for "fs.read"
			wantPolicy:   "deny",
		},
		// Row 11: ? wildcard — "fs.rea?" matches "fs.read"
		{
			name:         "question_mark_wildcard_matches",
			agentAllow:   []string{"fs.rea?"},
			agentDeny:    nil,
			tool:         "fs.read",
			wantDecision: true,
			wantPolicy:   "allow",
		},
		// Row 12: ? wildcard — "fs.rea?" does not match "fs.reads" (extra char)
		{
			name:         "question_mark_wildcard_no_match_reads",
			agentAllow:   []string{"fs.rea?"},
			agentDeny:    nil,
			tool:         "fs.reads",
			wantDecision: false,
			wantPolicy:   "deny",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// BDD: Given a SecurityConfig built from the test case
			defaultPol := policy.PolicyDeny // deny-by-default per project security posture
			if tc.defaultPolicy != "" {
				defaultPol = tc.defaultPolicy
			}
			cfg := &policy.SecurityConfig{
				DefaultPolicy: defaultPol,
				ToolPolicies:  tc.globalPolicies,
			}
			evaluator := policy.NewEvaluator(cfg)

			// Build the explicit AgentPolicy from test case fields.
			var agentPolicy *policy.AgentPolicy
			if tc.agentAllow != nil || tc.agentDeny != nil {
				agentPolicy = &policy.AgentPolicy{
					Tools: policy.AgentToolsPolicy{
						Allow: tc.agentAllow,
						Deny:  tc.agentDeny,
					},
				}
			}

			// BDD: When EvaluateTool is called with the tool name and agent policy
			var decision policy.Decision
			const agentID = "test-agent-policy-glob"
			if agentPolicy != nil {
				decision = evaluator.EvaluateTool(agentID, tc.tool, agentPolicy)
			} else {
				decision = evaluator.EvaluateTool(agentID, tc.tool)
			}

			// BDD: Then the decision must match the expected allow/deny
			assert.Equal(t, tc.wantDecision, decision.Allowed,
				"case %q: tool=%q: want allowed=%v got=%v (PolicyRule: %q)",
				tc.name, tc.tool, tc.wantDecision, decision.Allowed, decision.PolicyRule)

			// Content assertion: policy string must match
			assert.Equal(t, tc.wantPolicy, decision.Policy,
				"case %q: tool=%q: want policy=%q got=%q",
				tc.name, tc.tool, tc.wantPolicy, decision.Policy)

			// Content assertion: PolicyRule must not be empty (SEC-17 explainability)
			assert.NotEmpty(t, decision.PolicyRule,
				"case %q: PolicyRule must be non-empty for all decisions (SEC-17)", tc.name)

			// Content assertion: PolicyRule must mention the tool name
			assert.Contains(t, decision.PolicyRule, tc.tool,
				"case %q: PolicyRule must mention the tool name %q", tc.name, tc.tool)
		})
	}
}

// TestPolicyGlob_DenyBeatsAllowInvariant — differentiation test
//
// BDD: Given two evaluators — one that allows fs.delete via glob, one that also
//
//	denies fs.delete explicitly — when both evaluate the same tool, they must
//	produce DIFFERENT decisions. This proves the evaluator is not hardcoded.
//
// Traces to: sprint-j prompt §3 rows 4 vs 5.
func TestPolicyGlob_DenyBeatsAllowInvariant(t *testing.T) {
	const agentID = "diff-test-agent"
	const tool = "fs.delete"

	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyDeny}
	evaluator := policy.NewEvaluator(cfg)

	// Policy A: glob allow only
	policyAllowOnly := &policy.AgentPolicy{
		Tools: policy.AgentToolsPolicy{
			Allow: []string{"fs.*"},
			Deny:  nil,
		},
	}
	// Policy B: glob allow + explicit deny
	policyWithDeny := &policy.AgentPolicy{
		Tools: policy.AgentToolsPolicy{
			Allow: []string{"fs.*"},
			Deny:  []string{"fs.delete"},
		},
	}

	decisionAllow := evaluator.EvaluateTool(agentID, tool, policyAllowOnly)
	decisionDeny := evaluator.EvaluateTool(agentID, tool, policyWithDeny)

	// BDD: Then the two policies produce different outcomes
	assert.True(t, decisionAllow.Allowed,
		"glob allow without deny must permit fs.delete")
	assert.False(t, decisionDeny.Allowed,
		"explicit deny must override glob allow for fs.delete")
	// Differentiation: different inputs → different outputs (not hardcoded)
	assert.NotEqual(t, decisionAllow.Allowed, decisionDeny.Allowed,
		"allow-only vs allow+deny must produce different decisions (differentiation test)")
}

// TestPolicyGlob_GlobMatcherDirectly tests pkg/policy.MatchGlob directly to
// validate the ? and * wildcard semantics on which the evaluator depends.
//
// Traces to: sprint-j prompt §3 rows 10-12 (?-wildcard, prefix-only).
func TestPolicyGlob_GlobMatcherDirectly(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		want    bool
	}{
		// Star wildcard
		{"fs.*", "fs.read", true},
		{"fs.*", "fs.delete", true},
		{"fs.*", "shell", false},
		{"*", "anything", true},
		{"*", "", true},
		// Question mark
		{"fs.rea?", "fs.read", true},
		{"fs.rea?", "fs.reads", false}, // one ? can only match one char
		{"fs.rea?", "fs.rea", false},   // ? needs exactly one char
		{"fs.?ead", "fs.read", true},
		// Prefix only (no wildcard)
		{"fs", "fs.read", false},
		{"fs", "fs", true},
		// Mixed
		{"fs.r*", "fs.read", true},
		{"fs.r*", "fs.write", false},
		{"web_?earch", "web_search", true},
		{"web_?earch", "web_bearch", true}, // ? matches any single char
		{"web_?earch", "web_search_extra", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"/"+tc.s, func(t *testing.T) {
			got := policy.MatchGlob(tc.pattern, tc.s)
			assert.Equal(t, tc.want, got,
				"MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		})
	}
}

// TestPolicyGlob_GlobalDenyBlocksEvenWithAgentAllow — global deny overrides agent allow
//
// BDD: Given a global tool policy with {fs.*: deny},
//
//	When an agent tries to use fs.read with an explicit allow in its policy,
//	Then the request is still denied (global deny beats agent allow per SEC-04).
//
// Traces to: sprint-j prompt §3 row 5 semantic (global deny > agent allow).
func TestPolicyGlob_GlobalDenyBlocksEvenWithAgentAllow(t *testing.T) {
	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		ToolPolicies: map[string]policy.ToolPolicy{
			"fs.*": policy.ToolPolicyDeny, // global deny
		},
	}
	evaluator := policy.NewEvaluator(cfg)

	agentPolicy := &policy.AgentPolicy{
		Tools: policy.AgentToolsPolicy{
			Allow: []string{"fs.read"}, // agent tries to allow
		},
	}

	decision := evaluator.EvaluateTool("agent-with-allow", "fs.read", agentPolicy)

	// BDD: Then global deny wins
	require.False(t, decision.Allowed,
		"global deny on fs.* must block fs.read even when agent has fs.read in allow list")
	assert.Equal(t, "deny", decision.Policy,
		"policy must be 'deny' when global tool policy denies")
}

// TestPolicyGlob_EmptyAllowListDeniesByDefault — SEC-07 deny-by-default
//
// BDD: Given an agent with an explicit empty tools.allow list (not nil),
//
//	When EvaluateTool is called for any tool,
//	Then the decision must be "deny" (no tools permitted).
//
// This is the "empty array = no tools" semantic from the evaluator spec.
// Traces to: sprint-j prompt §3 (implicit row — empty allow list).
func TestPolicyGlob_EmptyAllowListDeniesByDefault(t *testing.T) {
	cfg := &policy.SecurityConfig{DefaultPolicy: policy.PolicyDeny}
	evaluator := policy.NewEvaluator(cfg)

	agentPolicy := &policy.AgentPolicy{
		Tools: policy.AgentToolsPolicy{
			Allow: []string{}, // explicit empty, not nil
		},
	}

	tools := []string{"web_search", "fs.read", "shell", "browser.*"}
	for _, tool := range tools {
		t.Run("denies_"+tool, func(t *testing.T) {
			decision := evaluator.EvaluateTool("agent-empty-allow", tool, agentPolicy)
			assert.False(t, decision.Allowed,
				"explicit empty tools.allow must deny all tools; %q was allowed", tool)
			assert.Equal(t, "deny", decision.Policy,
				"policy must be 'deny' for empty allow list")
		})
	}
}

// TestPolicyGlob_EvaluatorReturnsExplainableDecision — SEC-17 explainability
//
// BDD: Given any EvaluateTool call, the returned Decision must always contain a
//
//	non-empty PolicyRule that explains which rule matched.
//
// Traces to: sprint-j prompt §3 (SEC-17 explainability is a cross-cutting requirement).
func TestPolicyGlob_EvaluatorReturnsExplainableDecision(t *testing.T) {
	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
	}
	evaluator := policy.NewEvaluator(cfg)

	// Evaluate a variety of tool names — every decision must be explainable.
	scenarios := []struct {
		name    string
		agentID string
		tool    string
		policy  *policy.AgentPolicy
	}{
		{
			name:    "no_policy_deny_default",
			agentID: "agent-a",
			tool:    "web_search",
			policy:  nil,
		},
		{
			name:    "explicit_allow",
			agentID: "agent-b",
			tool:    "web_search",
			policy: &policy.AgentPolicy{
				Tools: policy.AgentToolsPolicy{Allow: []string{"web_search"}},
			},
		},
		{
			name:    "glob_allow",
			agentID: "agent-c",
			tool:    "fs.read",
			policy: &policy.AgentPolicy{
				Tools: policy.AgentToolsPolicy{Allow: []string{"fs.*"}},
			},
		},
		{
			name:    "explicit_deny",
			agentID: "agent-d",
			tool:    "fs.delete",
			policy: &policy.AgentPolicy{
				Tools: policy.AgentToolsPolicy{
					Allow: []string{"fs.*"},
					Deny:  []string{"fs.delete"},
				},
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			var decision policy.Decision
			if sc.policy != nil {
				decision = evaluator.EvaluateTool(sc.agentID, sc.tool, sc.policy)
			} else {
				decision = evaluator.EvaluateTool(sc.agentID, sc.tool)
			}

			// SEC-17: every decision must have a non-empty PolicyRule
			require.NotEmpty(t, decision.PolicyRule,
				"EvaluateTool must always return a non-empty PolicyRule for SEC-17 compliance")
			// The PolicyRule must contain either the tool name or the agent ID
			mentioned := false
			if sc.tool != "" {
				mentioned = mentioned || contains(decision.PolicyRule, sc.tool)
			}
			if sc.agentID != "" {
				mentioned = mentioned || contains(decision.PolicyRule, sc.agentID)
			}
			assert.True(t, mentioned,
				"PolicyRule %q must mention either the tool %q or agent %q",
				decision.PolicyRule, sc.tool, sc.agentID)
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
