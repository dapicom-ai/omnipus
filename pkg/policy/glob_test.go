// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dapicom-ai/omnipus/pkg/policy"
)

// TestGlobMatcher_ExecAllowlist validates glob pattern matching for allowed exec commands.
// Uses EvaluateExec which wraps the internal glob matcher.
// Traces to: wave2-security-layer-spec.md line 784 (TestGlobMatcher_ExecAllowlist)
// BDD: Scenario: Allowed binary pattern matches (spec line 499)
func TestGlobMatcher_ExecAllowlist(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 844 (Dataset: Exec Allowlist Glob Matching rows 1–10)
	tests := []struct {
		name        string
		command     string
		patterns    []string
		wantAllowed bool
		wantReason  string
	}{
		// Dataset row 1 — simple glob match
		{
			name:        "git status matches git *",
			command:     "git status",
			patterns:    []string{"git *"},
			wantAllowed: true,
		},
		// Dataset row 2 — multi-word after glob
		{
			name:        "git push origin main matches git *",
			command:     "git push origin main",
			patterns:    []string{"git *"},
			wantAllowed: true,
		},
		// Dataset row 3 — no matching pattern
		{
			name:        "curl not in git-only allowlist",
			command:     "curl http://x.com",
			patterns:    []string{"git *"},
			wantAllowed: false,
			wantReason:  "not in exec allowlist",
		},
		// Dataset row 4 — two-word prefix glob
		{
			name:        "npm run build matches npm run *",
			command:     "npm run build",
			patterns:    []string{"npm run *"},
			wantAllowed: true,
		},
		// Dataset row 5 — partial prefix mismatch
		{
			name:        "npm install does not match npm run *",
			command:     "npm install lodash",
			patterns:    []string{"npm run *"},
			wantAllowed: false,
			wantReason:  "not in exec allowlist",
		},
		// Dataset row 6 — suffix glob
		{
			name:        "python3 script.py matches python3 *.py",
			command:     "python3 script.py",
			patterns:    []string{"python3 *.py"},
			wantAllowed: true,
		},
		// Dataset row 7 — wrong suffix
		{
			name:        "python3 script.sh does not match python3 *.py",
			command:     "python3 script.sh",
			patterns:    []string{"python3 *.py"},
			wantAllowed: false,
			wantReason:  "not in exec allowlist",
		},
		// Dataset row 8 — empty allowlist with deny-by-default blocks everything
		{
			name:        "git status with empty allowlist is denied",
			command:     "git status",
			patterns:    []string{},
			wantAllowed: false,
		},
		// Dataset row 9 — common command
		{
			name:        "ls -la matches ls *",
			command:     "ls -la",
			patterns:    []string{"ls *"},
			wantAllowed: true,
		},
		// Dataset row 10 — glob matches dangerous commands too (safety not glob's concern)
		{
			name:        "rm -rf / matches rm * (glob has no safety semantics)",
			command:     "rm -rf /",
			patterns:    []string{"rm *"},
			wantAllowed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Traces to: wave2-security-layer-spec.md line 499 (Scenario: Allowed binary pattern matches)
			cfg := &policy.SecurityConfig{
				DefaultPolicy: policy.PolicyDeny,
				Policy: policy.PolicySection{
					Exec: policy.ExecPolicy{
						AllowedBinaries: tc.patterns,
					},
				},
			}
			evaluator := policy.NewEvaluator(cfg)
			result := evaluator.EvaluateExec("test-agent", tc.command)

			assert.Equal(t, tc.wantAllowed, result.Allowed)
			if !tc.wantAllowed && tc.wantReason != "" {
				assert.Contains(t, result.PolicyRule, tc.wantReason,
					"policy_rule should explain the denial")
			}
		})
	}
}

// TestGlobMatcher_NoMatch validates commands that should be denied due to no matching pattern.
// Traces to: wave2-security-layer-spec.md line 785 (TestGlobMatcher_NoMatch)
// BDD: Scenario: Disallowed binary is blocked (spec line 509)
func TestGlobMatcher_NoMatch(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 509 (Scenario: Disallowed binary is blocked)
	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		Policy: policy.PolicySection{
			Exec: policy.ExecPolicy{
				AllowedBinaries: []string{"git *", "npm run *"},
			},
		},
	}
	evaluator := policy.NewEvaluator(cfg)

	t.Run("curl blocked — policy_rule identifies binary", func(t *testing.T) {
		result := evaluator.EvaluateExec("test-agent", "curl http://example.com")
		assert.False(t, result.Allowed)
		assert.Contains(t, result.PolicyRule, "curl")
		assert.Contains(t, result.PolicyRule, "not in exec allowlist")
	})

	t.Run("wget blocked", func(t *testing.T) {
		result := evaluator.EvaluateExec("test-agent", "wget http://example.com")
		assert.False(t, result.Allowed)
		assert.Contains(t, result.PolicyRule, "wget")
	})

	t.Run("arbitrary script blocked", func(t *testing.T) {
		result := evaluator.EvaluateExec("test-agent", "/tmp/evil.sh")
		assert.False(t, result.Allowed)
	})
}

// TestGlobMatcher_EmptyList validates that an empty allowlist denies all exec commands.
// Traces to: wave2-security-layer-spec.md line 786 (TestGlobMatcher_EmptyList)
// BDD: Scenario: Empty allowlist with deny-by-default blocks all exec (spec line 517)
func TestGlobMatcher_EmptyList(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 517 (Scenario: Empty allowlist)
	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		Policy: policy.PolicySection{
			Exec: policy.ExecPolicy{
				AllowedBinaries: []string{}, // explicit empty
			},
		},
	}
	evaluator := policy.NewEvaluator(cfg)

	commands := []string{
		"git status",
		"npm install lodash",
		"python3 script.py",
		"ls -la",
	}
	for _, cmd := range commands {
		t.Run("denies: "+cmd, func(t *testing.T) {
			result := evaluator.EvaluateExec("test-agent", cmd)
			assert.False(t, result.Allowed,
				"command %q should be denied when allowlist is empty", cmd)
		})
	}

	t.Run("nil allowlist with deny-by-default also denies", func(t *testing.T) {
		cfgNil := &policy.SecurityConfig{
			DefaultPolicy: policy.PolicyDeny,
			Policy: policy.PolicySection{
				Exec: policy.ExecPolicy{
					AllowedBinaries: nil, // not configured at all
				},
			},
		}
		ev := policy.NewEvaluator(cfgNil)
		result := ev.EvaluateExec("test-agent", "git status")
		assert.False(t, result.Allowed,
			"nil allowlist with deny-by-default should block exec")
	})
}
