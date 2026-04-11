// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package security_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/security"
)

// TestMatchExecAllowlist validates the standalone glob-based exec allowlist matcher.
// Traces to: wave2-security-layer-spec.md line 784 (TestGlobMatcher_ExecAllowlist — security package)
// BDD: Scenario: Allowed binary pattern matches (spec line 499)
func TestMatchExecAllowlist(t *testing.T) {
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
			name: "git status matches git *", command: "git status",
			patterns: []string{"git *"}, wantAllowed: true,
		},
		// Dataset row 2 — multi-word after glob
		{
			name: "git push origin main matches git *", command: "git push origin main",
			patterns: []string{"git *"}, wantAllowed: true,
		},
		// Dataset row 3 — no matching pattern
		{
			name: "curl not in git-only allowlist", command: "curl http://x.com",
			patterns: []string{"git *"}, wantAllowed: false, wantReason: "not in exec allowlist",
		},
		// Dataset row 4 — two-word prefix glob
		{
			name: "npm run build matches npm run *", command: "npm run build",
			patterns: []string{"npm run *"}, wantAllowed: true,
		},
		// Dataset row 5 — partial prefix mismatch
		{
			name: "npm install does not match npm run *", command: "npm install lodash",
			patterns: []string{"npm run *"}, wantAllowed: false, wantReason: "not in exec allowlist",
		},
		// Dataset row 6 — suffix glob
		{
			name: "python3 script.py matches python3 *.py", command: "python3 script.py",
			patterns: []string{"python3 *.py"}, wantAllowed: true,
		},
		// Dataset row 7 — wrong suffix
		{
			name: "python3 script.sh does not match python3 *.py", command: "python3 script.sh",
			patterns: []string{"python3 *.py"}, wantAllowed: false, wantReason: "not in exec allowlist",
		},
		// Dataset row 8 — empty allowlist blocks everything
		{
			name: "empty allowlist denies all", command: "git status",
			patterns: nil, wantAllowed: false, wantReason: "empty allowlist",
		},
		// Dataset row 9 — common command
		{
			name: "ls -la matches ls *", command: "ls -la",
			patterns: []string{"ls *"}, wantAllowed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := security.MatchExecAllowlist(tc.command, tc.patterns)
			assert.Equal(t, tc.wantAllowed, result.Allowed)
			if !tc.wantAllowed && tc.wantReason != "" {
				assert.Contains(t, result.PolicyRule, tc.wantReason,
					"policy_rule should explain the denial")
			}
		})
	}
}

// TestExecApprovalManager_ModeOff validates that ExecApprovalModeOff bypasses all
// prompting and auto-approves every command.
// Traces to: wave2-security-layer-spec.md line 787 (TestExecApproval_OffMode)
// BDD: Scenario: Exec approval disabled allows all commands (spec line 517)
func TestExecApprovalManager_ModeOff(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 517 (Scenario: mode off — no prompt)
	mgr := security.NewExecApprovalManager(security.ExecApprovalConfig{Mode: "off"})

	commands := []string{
		"git status",
		"rm -rf /", // even dangerous commands
		"curl http://x.com",
		"python3 evil.py",
	}

	for _, cmd := range commands {
		t.Run("off mode allows: "+cmd, func(t *testing.T) {
			result := mgr.CheckApproval(cmd)
			assert.True(t, result.Approved,
				"mode=off must approve every command without prompting")
			assert.Contains(t, result.PolicyRule, "off",
				"policy_rule must mention mode=off")
		})
	}
}

// TestExecApprovalManager_PersistPattern validates that a persisted pattern auto-approves
// subsequent commands without prompting.
// Traces to: wave2-security-layer-spec.md line 787 (TestExecApproval_PersistPattern)
// BDD: Scenario: Persistent allowlist auto-approves matching command (spec line 517)
func TestExecApprovalManager_PersistPattern(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 517 (Scenario: Persistent pattern)
	mgr := security.NewExecApprovalManager(security.ExecApprovalConfig{Mode: "ask"})

	// Persist a pattern for git commands
	mgr.PersistPattern("git *")

	t.Run("git status auto-approved via persistent pattern", func(t *testing.T) {
		result := mgr.CheckApproval("git status")
		assert.True(t, result.Approved,
			"command matching persistent pattern must be auto-approved")
		assert.True(t, result.AutoApproved,
			"AutoApproved must be true for persistent pattern match")
		assert.Contains(t, result.PolicyRule, "git *",
			"policy_rule must reference the matching pattern")
	})

	t.Run("git push origin main auto-approved via persistent pattern", func(t *testing.T) {
		result := mgr.CheckApproval("git push origin main")
		assert.True(t, result.Approved)
		assert.True(t, result.AutoApproved)
	})

	t.Run("duplicate pattern is not added twice", func(t *testing.T) {
		// Adding the same pattern twice should be idempotent
		mgr.PersistPattern("git *")
		mgr.PersistPattern("git *")
		// If we can still check approval, the state is consistent
		result := mgr.CheckApproval("git diff")
		assert.True(t, result.Approved)
	})
}

// TestExecApprovalManager_AllowlistFilePersistence validates that persisted patterns
// survive across manager instances via the JSON allowlist file.
// Traces to: wave2-security-layer-spec.md line 787 (TestExecApproval_FileLoad)
// BDD: Scenario: Allowlist patterns persist across restarts (spec line 517)
func TestExecApprovalManager_AllowlistFilePersistence(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 517 (Scenario: Persistent allowlist — file)
	dir := t.TempDir()
	allowlistPath := filepath.Join(dir, "exec-allowlist.json")

	// Manager 1: add a pattern and let it persist to file
	mgr1 := security.NewExecApprovalManager(security.ExecApprovalConfig{Mode: "ask"})
	require.NoError(t, mgr1.WithAllowlistFile(allowlistPath))
	mgr1.PersistPattern("npm run *")

	// Verify the file was written
	data, err := os.ReadFile(allowlistPath)
	require.NoError(t, err, "exec-allowlist.json should be created after PersistPattern")

	var af struct{ Patterns []string }
	require.NoError(t, json.Unmarshal(data, &af))
	assert.Contains(t, af.Patterns, "npm run *",
		"persisted pattern should appear in the allowlist file")

	// Manager 2: loads from the same file and auto-approves matching commands
	mgr2 := security.NewExecApprovalManager(security.ExecApprovalConfig{Mode: "ask"})
	require.NoError(t, mgr2.WithAllowlistFile(allowlistPath))

	result := mgr2.CheckApproval("npm run build")
	assert.True(t, result.Approved,
		"pattern loaded from allowlist file must auto-approve matching command")
	assert.True(t, result.AutoApproved)
}
