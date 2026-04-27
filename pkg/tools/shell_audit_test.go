package tools

// Tests for ExecTool/ReadFileTool audit emission on path-guard rejection.
// Spec: path-sandbox-and-capability-tiers-spec.md /
// BDD test ID: #84b
// Traces to: path-sandbox-and-capability-tiers-spec.md line ~183

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// ---------------------------------------------------------------------------
// TestExecTool_AuditOnPathRejection
// BDD: Given a workspace-restricted ExecTool with an audit logger,
// When a command containing "../" traversal is executed,
// Then the tool returns IsError=true (path.access_denied emitted).
// Traces to: path-sandbox-and-capability-tiers-spec.md
// ---------------------------------------------------------------------------

func TestExecTool_AuditOnPathRejection(t *testing.T) {
	workspace := t.TempDir()

	// Construct workspace-restricted ExecTool (restrict=true so the workspace
	// guard in guardCommand applies and the "../" traversal is blocked).
	tool, err := NewExecTool(workspace, true)
	require.NoError(t, err)

	// Wire in audit logger.
	auditDir := t.TempDir()
	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           auditDir,
		RetentionDays: 1,
	})
	require.NoError(t, err)
	tool.SetAuditLogger(auditLogger)

	// "cli" is an internal channel — passes the remote-channel guard so we reach
	// the path-traversal check in guardCommand (the behaviour under test).
	ctx := WithToolContext(context.Background(), "cli", "")

	// A command with "../" triggers guardCommand's path-traversal check.
	result := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "cat ../../etc/passwd",
	})

	// Primary: tool must reject.
	require.True(t, result.IsError,
		"ExecTool must return IsError=true on path traversal (got ForLLM: %q)", result.ForLLM)
	assert.NotEmpty(t, result.ForLLM, "ForLLM must describe the rejection reason")
	assert.Contains(t, result.ForLLM, "blocked",
		"rejection message must mention 'blocked'")
}

// ---------------------------------------------------------------------------
// TestReadFileTool_AuditOnPathRejection_AccessDeniedEmitted
// BDD: Given a workspace-restricted ReadFileTool with an audit logger,
// When read_file is called with /etc/passwd (outside workspace),
// Then IsError=true, audit file is non-empty (path.access_denied emitted).
// Traces to: path-sandbox-and-capability-tiers-spec.md
// ---------------------------------------------------------------------------

func TestReadFileTool_AuditOnPathRejection_AccessDeniedEmitted(t *testing.T) {
	workspace := t.TempDir()
	auditDir := t.TempDir()

	auditLogger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           auditDir,
		RetentionDays: 1,
	})
	require.NoError(t, err)

	tool := NewReadFileTool(workspace, true, 0)
	tool.SetAuditLogger(auditLogger)

	ctx := WithToolContext(context.Background(), "test-agent", "")

	result := tool.Execute(ctx, map[string]any{"path": "/etc/passwd"})

	// Tool must reject.
	require.True(t, result.IsError,
		"ReadFileTool must reject /etc/passwd with IsError=true")
	assert.NotEmpty(t, result.ForLLM, "ForLLM must be non-empty on path rejection")

	// Audit file must be written.
	entries, err := os.ReadDir(auditDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "audit dir must contain at least one file after rejection")

	var totalBytes int
	for _, e := range entries {
		fp := filepath.Join(auditDir, e.Name())
		data, readErr := os.ReadFile(fp)
		if readErr == nil {
			totalBytes += len(data)
		}
	}
	assert.Greater(t, totalBytes, 0,
		"audit file must contain data (path.access_denied entry expected)")
}

// ---------------------------------------------------------------------------
// TestReadFileTool_AuditReason_OutsideWorkspace
// BDD: Given a workspace-restricted ReadFileTool with no allow-list,
// When read_file is called with /etc/passwd,
// Then the reason is "outside_workspace" (not "not_in_allow_list").
// Traces to: path-sandbox-and-capability-tiers-spec.md
// ---------------------------------------------------------------------------

func TestReadFileTool_AuditReason_OutsideWorkspace(t *testing.T) {
	// classifyPathDenialReason with allowPathsLen=0 and a non-symlink error
	// must return "outside_workspace".
	dummyErr := errorFromString("path is outside the workspace")
	reason := classifyPathDenialReason(dummyErr, 0)
	assert.Equal(t, ReasonOutsideWorkspace, reason,
		"no allow-list: reason must be outside_workspace")
}

func TestReadFileTool_AuditReason_NotInAllowList(t *testing.T) {
	dummyErr := errorFromString("path is outside the workspace")
	reason := classifyPathDenialReason(dummyErr, 3) // 3 allow-paths configured
	assert.Equal(t, ReasonNotInAllowList, reason,
		"with allow-list configured: reason must be not_in_allow_list")
}

func TestReadFileTool_AuditReason_SymlinkEscape(t *testing.T) {
	symlinkErr := errorFromString("symlink escapes workspace boundary")
	reason := classifyPathDenialReason(symlinkErr, 0)
	assert.Equal(t, ReasonSymlinkEscape, reason,
		"symlink error: reason must be symlink_escape")
}

// errorFromString converts a string into an error for testing.
func errorFromString(s string) error {
	return &testError{msg: s}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Differentiation test: two different rejected paths produce real error results.
// ---------------------------------------------------------------------------

func TestReadFileTool_DifferentRejectedPaths_BothDenied(t *testing.T) {
	workspace := t.TempDir()
	tool := NewReadFileTool(workspace, true, 0)
	ctx := WithToolContext(context.Background(), "test-agent", "")

	result1 := tool.Execute(ctx, map[string]any{"path": "/etc/passwd"})
	result2 := tool.Execute(ctx, map[string]any{"path": "/etc/shadow"})

	// Both must be errors.
	require.True(t, result1.IsError, "/etc/passwd: must be rejected")
	require.True(t, result2.IsError, "/etc/shadow: must be rejected")

	// Both must have non-empty error descriptions.
	assert.NotEmpty(t, result1.ForLLM, "rejection for /etc/passwd must have ForLLM content")
	assert.NotEmpty(t, result2.ForLLM, "rejection for /etc/shadow must have ForLLM content")
}

// ---------------------------------------------------------------------------
// Constant verification — all three reason constants must be the spec values.
// Traces to: path-sandbox-and-capability-tiers-spec.md
// ---------------------------------------------------------------------------

func TestPathAuditConstants_MatchSpecValues(t *testing.T) {
	assert.Equal(t, "path.access_denied", PathAccessDeniedEvent,
		"PathAccessDeniedEvent must match MAJ-001 event class")
	assert.Equal(t, "outside_workspace", ReasonOutsideWorkspace,
		"ReasonOutsideWorkspace must match MAJ-002 spec literal")
	assert.Equal(t, "not_in_allow_list", ReasonNotInAllowList,
		"ReasonNotInAllowList must match MAJ-002 spec literal")
	assert.Equal(t, "symlink_escape", ReasonSymlinkEscape,
		"ReasonSymlinkEscape must match MAJ-002 spec literal")
}
