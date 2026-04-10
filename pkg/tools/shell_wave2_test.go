// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package tools

import (
	"context"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/policy"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// mockPolicyAuditor records calls to EvaluateExec and returns a canned decision.
type mockPolicyAuditor struct {
	decision policy.Decision
	calls    int32
	lastCmd  string
	lastAgnt string
}

func (m *mockPolicyAuditor) EvaluateExec(agentID, command string) policy.Decision {
	atomic.AddInt32(&m.calls, 1)
	m.lastCmd = command
	m.lastAgnt = agentID
	return m.decision
}

// mockSandboxBackend records the last cmd it was applied to.
type mockSandboxBackend struct {
	calls      int32
	lastCmdPtr *exec.Cmd
	failWith   error
}

func (m *mockSandboxBackend) ApplyToCmd(cmd *exec.Cmd, _ sandbox.SandboxPolicy) error {
	atomic.AddInt32(&m.calls, 1)
	m.lastCmdPtr = cmd
	return m.failWith
}

// configWithAllowlist returns a config.Config with an exec allowlist that
// permits only the given patterns.
func configWithAllowlist(patterns ...string) *config.Config {
	cfg := &config.Config{}
	cfg.Tools.Exec.EnableDenyPatterns = true
	cfg.Tools.Exec.AllowedBinaries = append([]string(nil), patterns...)
	return cfg
}

// TestExecTool_BinaryAllowlist_Denies verifies SEC-05 enforcement: a command
// that does not match any allowed pattern is rejected with the policy rule
// in the error result.
func TestExecTool_BinaryAllowlist_Denies(t *testing.T) {
	cfg := configWithAllowlist("git *")
	auditor := &mockPolicyAuditor{
		decision: policy.Decision{
			Allowed:    false,
			PolicyRule: `binary "rm" not in exec allowlist`,
		},
	}

	tool, err := NewExecToolWithDeps("", false, cfg, ExecToolDeps{
		PolicyAuditor: auditor,
	})
	require.NoError(t, err)

	// Use a safe command so we isolate the allowlist path — dangerous
	// commands are blocked earlier by guardCommand's deny patterns, which
	// would mask the behaviour we want to verify here.
	ctx := WithToolContext(context.Background(), "cli", "")
	ctx = WithAgentID(ctx, "test-agent")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo hello",
	})
	require.NotNil(t, result)
	assert.True(t, result.IsError, "allowlist miss must produce an error result")
	assert.Contains(t, result.ForLLM, "Command blocked by exec allowlist",
		"error message must mention the allowlist")
	assert.Contains(t, result.ForLLM, "not in exec allowlist",
		"error message must include the policy rule")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&auditor.calls), int32(1),
		"PolicyAuditor.EvaluateExec must be called")
	assert.Equal(t, "test-agent", auditor.lastAgnt,
		"agent ID from context must be passed to the auditor")
}

// TestExecTool_BinaryAllowlist_Allows verifies that a matching pattern lets
// the command proceed past the allowlist check. We still let the command
// actually run to catch any regression where the allow path skips too much.
func TestExecTool_BinaryAllowlist_Allows(t *testing.T) {
	cfg := configWithAllowlist("echo *")
	auditor := &mockPolicyAuditor{
		decision: policy.Decision{
			Allowed:    true,
			PolicyRule: `exec allowed: command matched pattern "echo *"`,
		},
	}

	tool, err := NewExecToolWithDeps("", false, cfg, ExecToolDeps{
		PolicyAuditor: auditor,
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	ctx = WithAgentID(ctx, "test-agent")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo wave2",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError, "allowed command must run: %s", result.ForLLM)
	assert.Contains(t, result.ForLLM, "wave2",
		"stdout must be captured when allowed")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&auditor.calls), int32(1),
		"PolicyAuditor.EvaluateExec must be called for allowed commands too")
}

// TestExecTool_BinaryAllowlist_EmptyList_DelegatesToEvaluator verifies that
// when the allowlist is empty, the auditor IS still consulted — the evaluator
// is responsible for deciding whether to enforce (based on its default_policy).
// This prevents the silent "empty list means disabled" failure mode that
// violated deny-by-default semantics in an earlier version of Wave 2.
func TestExecTool_BinaryAllowlist_EmptyList_DelegatesToEvaluator(t *testing.T) {
	cfg := configWithAllowlist() // no patterns
	// Return allow — an empty list with default_policy=allow should permit
	// the command. The test verifies delegation, not a specific policy outcome.
	auditor := &mockPolicyAuditor{
		decision: policy.Decision{
			Allowed:    true,
			PolicyRule: "default_policy is 'allow', empty allowlist",
		},
	}

	tool, err := NewExecToolWithDeps("", false, cfg, ExecToolDeps{
		PolicyAuditor: auditor,
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	ctx = WithAgentID(ctx, "test-agent")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo empty-allowlist",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError,
		"allowed command should run: %s", result.ForLLM)
	assert.Equal(t, int32(1), atomic.LoadInt32(&auditor.calls),
		"auditor MUST be consulted even with empty allowlist — the evaluator decides whether to enforce")
}

// TestExecTool_BinaryAllowlist_EmptyList_RealEvaluator_Allow is an integration
// test that uses the real policy.Evaluator (not a mock) to verify the
// empty-list case honours default_policy=allow by permitting all commands.
func TestExecTool_BinaryAllowlist_EmptyList_RealEvaluator_Allow(t *testing.T) {
	eval := policy.NewEvaluator(&policy.SecurityConfig{
		DefaultPolicy: policy.PolicyAllow,
		// No AllowedBinaries → empty list.
	})
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	tool, err := NewExecToolWithDeps("", false, nil, ExecToolDeps{
		PolicyAuditor: auditor,
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	ctx = WithAgentID(ctx, "test-agent")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo real-allow",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError,
		"allow default + empty list must permit commands: %s", result.ForLLM)
	assert.Contains(t, result.ForLLM, "real-allow")
}

// TestExecTool_BinaryAllowlist_RealEvaluator_EnforcesPattern is the headline
// end-to-end test: wire a real evaluator with a real allowlist, confirm that
// matching commands pass and non-matching commands are blocked. This test
// would have caught the loop.go/NewEvaluator(nil) wiring bug that prevented
// any pattern in the UI from ever taking effect.
func TestExecTool_BinaryAllowlist_RealEvaluator_EnforcesPattern(t *testing.T) {
	eval := policy.NewEvaluator(&policy.SecurityConfig{
		DefaultPolicy: policy.PolicyDeny,
		Policy: policy.PolicySection{
			Exec: policy.ExecPolicy{
				AllowedBinaries: []string{"echo *"},
			},
		},
	})
	auditor := policy.NewPolicyAuditor(eval, nil, "")

	tool, err := NewExecToolWithDeps("", false, nil, ExecToolDeps{
		PolicyAuditor: auditor,
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	ctx = WithAgentID(ctx, "test-agent")

	// Matching pattern: allowed.
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo pattern-match",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError,
		"'echo pattern-match' must match 'echo *' pattern: %s", result.ForLLM)
	assert.Contains(t, result.ForLLM, "pattern-match")

	// Non-matching pattern: denied with explainable policy rule.
	result = tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "true",
	})
	require.NotNil(t, result)
	assert.True(t, result.IsError,
		"'true' must be denied (not in echo * allowlist)")
	assert.Contains(t, result.ForLLM, "Command blocked by exec allowlist")
	assert.Contains(t, result.ForLLM, "not in exec allowlist",
		"error must include policy rule explanation")
}

// TestExecTool_SandboxBackend_AppliedBeforeStart verifies that the sandbox
// backend's ApplyToCmd is called before cmd.Start(). We use a mock backend
// and confirm it sees a valid *exec.Cmd.
func TestExecTool_SandboxBackend_AppliedBeforeStart(t *testing.T) {
	mock := &mockSandboxBackend{}
	tool, err := NewExecToolWithDeps("", false, nil, ExecToolDeps{
		SandboxBackend: mock,
		SandboxPolicy: sandbox.SandboxPolicy{
			FilesystemRules: []sandbox.PathRule{
				{Path: t.TempDir(), Access: sandbox.AccessRead},
			},
		},
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo sandbox-applied",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError, "command should run: %s", result.ForLLM)
	assert.Contains(t, result.ForLLM, "sandbox-applied")
	assert.Equal(t, int32(1), atomic.LoadInt32(&mock.calls),
		"sandbox.ApplyToCmd must be called exactly once for a sync run")
	assert.NotNil(t, mock.lastCmdPtr, "ApplyToCmd must receive a non-nil cmd")
}

// TestExecTool_SandboxBackend_FailurePreventsStart verifies that if the
// sandbox backend returns an error, the command is NOT started and the
// error is surfaced to the caller.
func TestExecTool_SandboxBackend_FailurePreventsStart(t *testing.T) {
	mock := &mockSandboxBackend{
		failWith: assertErr{"sandbox unavailable"},
	}
	tool, err := NewExecToolWithDeps("", false, nil, ExecToolDeps{
		SandboxBackend: mock,
		SandboxPolicy: sandbox.SandboxPolicy{
			FilesystemRules: []sandbox.PathRule{
				{Path: t.TempDir(), Access: sandbox.AccessRead},
			},
		},
	})
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo should-not-run",
	})
	require.NotNil(t, result)
	assert.True(t, result.IsError, "sandbox failure must produce an error result")
	assert.Contains(t, result.ForLLM, "sandbox setup failed",
		"error must identify sandbox setup as the failure point")
	// Critical assertion: the command output must NOT appear, proving the
	// process was never started.
	assert.NotContains(t, result.ForLLM, "should-not-run",
		"command must not execute when sandbox setup fails")
}

// TestExecTool_NoDeps_BackwardCompatible verifies that the legacy
// NewExecToolWithConfig constructor still works with no Wave 2 wiring.
func TestExecTool_NoDeps_BackwardCompatible(t *testing.T) {
	tool, err := NewExecToolWithConfig("", false, nil)
	require.NoError(t, err)

	ctx := WithToolContext(context.Background(), "cli", "")
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "echo legacy",
	})
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.True(t, strings.Contains(result.ForLLM, "legacy"))
}

// assertErr is a tiny error type for tests.
type assertErr struct{ msg string }

func (e assertErr) Error() string { return e.msg }
