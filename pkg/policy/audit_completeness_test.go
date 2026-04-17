// Contract test: Plan 3 §1 acceptance decision — every policy decision produces
// exactly one audit entry.
//
// BDD: Given a PolicyAuditor with a fake audit sink, When EvaluateTool is called,
//
//	Then the audit sink receives exactly one entry with the correct decision value.
//
// Acceptance decision: Plan 3 §1 "Audit log completeness: every tool call"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/policy/audit_completeness_test.go

package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuditSink records every LogPolicyDecision call for assertion.
type fakeAuditSink struct {
	entries []*AuditEntry
}

func (f *fakeAuditSink) LogPolicyDecision(entry *AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

// TestEveryPolicyDecisionAudited verifies that PolicyAuditor sends exactly one
// audit entry for each EvaluateTool call, regardless of whether the decision
// is allow, ask, or deny.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestEveryPolicyDecisionAudited
func TestEveryPolicyDecisionAudited(t *testing.T) {
	tests := []struct {
		name        string
		toolPolicy  ToolPolicy
		wantAllowed bool
		wantPolicy  string
	}{
		{
			name:        "allow produces one audit entry",
			toolPolicy:  ToolPolicyAllow,
			wantAllowed: true,
			wantPolicy:  "allow",
		},
		{
			name:        "ask produces one audit entry",
			toolPolicy:  ToolPolicyAsk,
			wantAllowed: true,
			wantPolicy:  "ask",
		},
		{
			name:        "deny produces one audit entry",
			toolPolicy:  ToolPolicyDeny,
			wantAllowed: false,
			wantPolicy:  "deny",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &fakeAuditSink{}
			secCfg := &SecurityConfig{
				DefaultPolicy: PolicyAllow,
				ToolPolicies:  map[string]ToolPolicy{"exec": tc.toolPolicy},
			}
			eval := NewEvaluator(secCfg)
			auditor := NewPolicyAuditor(eval, sink, "session-abc")

			// BDD: When EvaluateTool is called once.
			decision := auditor.EvaluateTool("ray", "exec")

			// BDD: Then exactly one audit entry was written.
			require.Len(t, sink.entries, 1,
				"exactly one audit entry must be written per EvaluateTool call")

			entry := sink.entries[0]
			assert.Equal(t, "tool_call", entry.Event,
				"audit entry must have event == tool_call")
			assert.Equal(t, "ray", entry.AgentID,
				"audit entry must capture the agent ID")
			assert.Equal(t, "exec", entry.Tool,
				"audit entry must capture the tool name")

			// The decision value in the audit entry must match the actual decision.
			if tc.wantAllowed {
				assert.Equal(t, DecisionAllow, entry.Decision,
					"allowed decision must produce audit Decision=allow")
			} else {
				assert.Equal(t, DecisionDeny, entry.Decision,
					"denied decision must produce audit Decision=deny")
			}

			// The actual decision must match expectations.
			assert.Equal(t, tc.wantAllowed, decision.Allowed,
				"EvaluateTool result.Allowed must match expected outcome")
			assert.Equal(t, tc.wantPolicy, decision.Policy,
				"EvaluateTool result.Policy must match expected policy string")

			// Differentiation: a second call on a different tool produces a second entry.
			_ = auditor.EvaluateTool("ray", "read_file")
			assert.Len(t, sink.entries, 2,
				"second EvaluateTool call must produce a second audit entry")
			assert.Equal(t, "read_file", sink.entries[1].Tool,
				"second audit entry must reference the second tool (not hardcoded)")
		})
	}
}

// DecisionAllow and DecisionDeny are the expected audit Decision strings.
// Mirrors audit.DecisionAllow / audit.DecisionDeny without importing audit.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)
