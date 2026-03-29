// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package security_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/policy"
	"github.com/dapicom-ai/omnipus/pkg/security"
)

// TestE2E_AgentToolDenied is an end-to-end test: agent invokes denied tool →
// receives deny decision with policy_rule → audit entry written with decision: "deny".
// Traces to: wave2-security-layer-spec.md line 813 (TestE2E_AgentToolDenied)
// BDD: Full tool invocation denied (spec line 813)
func TestE2E_AgentToolDenied(t *testing.T) {
	dir := t.TempDir()
	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
		RedactEnabled: true,
	})
	require.NoError(t, err)
	defer auditLogger.Close()

	cfg := &policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		Agents: map[string]policy.AgentPolicy{
			"researcher": {
				Tools: policy.AgentToolsPolicy{
					Allow: []string{"web_search"},
				},
			},
		},
	}
	evaluator := policy.NewEvaluator(cfg)

	// Agent "researcher" invokes "exec" — not in allow list
	decision := evaluator.EvaluateTool("researcher", "exec")
	require.False(t, decision.Allowed, "exec must be denied for researcher agent")
	require.NotEmpty(t, decision.PolicyRule)

	// Log the denial to audit
	entry := audit.Entry{
		Timestamp:  time.Now().UTC(),
		Event:      audit.EventToolCall,
		Decision:   "deny",
		AgentID:    "researcher",
		SessionID:  "sess-e2e-001",
		Tool:       "exec",
		Parameters: map[string]any{},
		PolicyRule: decision.PolicyRule,
	}
	require.NoError(t, auditLogger.Log(&entry))

	// Validate audit log entry
	logPath := filepath.Join(dir, "audit.jsonl")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "audit entry must be valid JSON")

	assert.Equal(t, "deny", parsed["decision"])
	assert.Equal(t, "exec", parsed["tool"])
	assert.Equal(t, "researcher", parsed["agent_id"])

	policyRule, _ := parsed["policy_rule"].(string)
	assert.NotEmpty(t, policyRule,
		"audit entry must include policy_rule explaining the denial")
	assert.Contains(t, policyRule, "exec",
		"policy_rule must reference the denied tool")
}

// TestE2E_RateLimitTriggered is an end-to-end test: agent hits rate limit →
// receives retry_after → audit entry written with decision: "deny".
// Traces to: wave2-security-layer-spec.md line 815 (TestE2E_RateLimitTriggered)
// BDD: Full rate limit rejection (spec line 689)
func TestE2E_RateLimitTriggered(t *testing.T) {
	dir := t.TempDir()
	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
	})
	require.NoError(t, err)
	defer auditLogger.Close()

	sw := security.NewSlidingWindow(3, time.Minute, security.ScopeAgent, "researcher", "llm_calls")

	// Exhaust the limit
	for i := 0; i < 3; i++ {
		sw.Allow()
	}

	// 4th call should be rejected
	limitResult := sw.Allow()
	require.False(t, limitResult.Allowed)
	assert.Greater(t, limitResult.RetryAfterSeconds, 0.0)
	assert.NotEmpty(t, limitResult.PolicyRule)

	// Log the rate limit rejection
	entry := audit.Entry{
		Timestamp:  time.Now().UTC(),
		Event:      audit.EventRateLimit,
		Decision:   "deny",
		AgentID:    "researcher",
		SessionID:  "sess-e2e-003",
		Tool:       "llm_call",
		Parameters: map[string]any{},
		PolicyRule: limitResult.PolicyRule,
	}
	require.NoError(t, auditLogger.Log(&entry))

	// Validate audit entry has all required fields
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "deny", parsed["decision"])
	policyRule, _ := parsed["policy_rule"].(string)
	assert.Contains(t, policyRule, "rate_limit")
}

// TestE2E_SSRFBlocked is an end-to-end test: agent calls web_fetch to private IP →
// SSRF checker blocks it → audit entry written with decision: "deny".
// Traces to: wave2-security-layer-spec.md line 816 (TestE2E_SSRFBlocked)
// BDD: Full SSRF block (spec line 648)
func TestE2E_SSRFBlocked(t *testing.T) {
	dir := t.TempDir()
	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
		RedactEnabled: true,
	})
	require.NoError(t, err)
	defer auditLogger.Close()

	checker := security.NewSSRFChecker(nil)

	targetURL := "http://169.254.169.254/latest/meta-data/"
	ssrfErr := checker.CheckURL(context.Background(), targetURL)
	require.Error(t, ssrfErr, "cloud metadata endpoint must be blocked by SSRF checker")
	assert.Contains(t, ssrfErr.Error(), "SSRF")

	// Build audit entry for the denial
	entry := audit.Entry{
		Timestamp:  time.Now().UTC(),
		Event:      audit.EventSSRF,
		Decision:   "deny",
		AgentID:    "researcher",
		SessionID:  "sess-e2e-004",
		Tool:       "web_fetch",
		Parameters: map[string]any{"url": targetURL},
		PolicyRule: ssrfErr.Error(),
	}
	require.NoError(t, auditLogger.Log(&entry))

	// Validate audit entry
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "deny", parsed["decision"])
	assert.Equal(t, "web_fetch", parsed["tool"])
	policyRule, _ := parsed["policy_rule"].(string)
	assert.Contains(t, policyRule, "SSRF")
}
