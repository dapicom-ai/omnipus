// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package audit_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/audit"
)

// TestAuditLogger_WriteAndRotate is an integration test covering JSONL append,
// 50MB rotation, retention cleanup, and crash-recovery validation of last line.
// Traces to: wave2-security-layer-spec.md line 807 (TestAuditLogger_WriteAndRotate)
// BDD: Scenario: Tool call produces audit entry + Audit log rotation at 50MB (spec line 577, 587)
func TestAuditLogger_WriteAndRotate(t *testing.T) {
	t.Run("tool call appends valid JSON line to audit.jsonl", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 577 (Scenario: Tool call produces audit entry)
		dir := t.TempDir()
		logger, err := audit.NewLogger(audit.LoggerConfig{
			Dir:           dir,
			RetentionDays: 90,
		})
		require.NoError(t, err)
		defer logger.Close()

		entry := audit.Entry{
			Timestamp:  time.Now().UTC(),
			Event:      audit.EventToolCall,
			Decision:   "allow",
			AgentID:    "general-assistant",
			SessionID:  "sess-abc123",
			Tool:       "web_search",
			Parameters: map[string]any{"query": "AWS pricing"},
			PolicyRule: "tools.allow matched 'web_search' for agent 'general-assistant'",
		}
		require.NoError(t, logger.Log(&entry))

		// Read back and validate
		logPath := filepath.Join(dir, "audit.jsonl")
		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		require.NotEmpty(t, data)

		var parsed map[string]any
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err, "audit entry must be valid JSON: %s", string(data))

		assert.Equal(t, "tool_call", parsed["event"])
		assert.Equal(t, "allow", parsed["decision"])
		assert.Equal(t, "general-assistant", parsed["agent_id"])
		assert.Equal(t, "web_search", parsed["tool"])
		assert.NotEmpty(t, parsed["timestamp"], "timestamp field must be present")
		assert.NotEmpty(t, parsed["policy_rule"], "policy_rule field must be present")
		assert.NotEmpty(t, parsed["session_id"], "session_id field must be present")
	})

	t.Run("rotation triggered when file exceeds 50MB", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 587 (Scenario: Audit log rotation at 50MB)
		dir := t.TempDir()
		const rotationSize = 50 * 1024 * 1024 // 50MB

		logger, err := audit.NewLogger(audit.LoggerConfig{
			Dir:           dir,
			RetentionDays: 90,
		})
		require.NoError(t, err)
		defer logger.Close()

		logPath := filepath.Join(dir, "audit.jsonl")

		// Pre-fill the log file to just under 50MB
		bigPad := strings.Repeat("x", 1024)
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		for i := 0; i < (rotationSize/1024 - 1); i++ {
			fmt.Fprintln(f, bigPad)
		}
		f.Close()

		// Reopen the logger (it reads current file size on open)
		logger.Close()
		logger, err = audit.NewLogger(audit.LoggerConfig{
			Dir:           dir,
			RetentionDays: 90,
		})
		require.NoError(t, err)
		defer logger.Close()

		// Write an entry — should trigger rotation
		entry := audit.Entry{
			Timestamp: time.Now().UTC(),
			Event:     audit.EventToolCall,
			Decision:  "allow",
			AgentID:   "test-agent",
			Tool:      "web_search",
		}
		require.NoError(t, logger.Log(&entry))

		// Rotated file should exist with date suffix
		files, err := filepath.Glob(filepath.Join(dir, "audit-*.jsonl"))
		require.NoError(t, err)
		assert.NotEmpty(t, files, "rotated audit file with date suffix should exist")

		// New audit.jsonl should be smaller than the rotation threshold
		info, err := os.Stat(logPath)
		require.NoError(t, err)
		assert.Less(t, info.Size(), int64(rotationSize),
			"new audit.jsonl after rotation should be smaller than 50MB")
	})

	t.Run("retention cleanup removes files older than retention_days", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 154 (User Story 9, Acceptance Scenario 4)
		dir := t.TempDir()

		// Create a "stale" rotated file with a date 8 days ago (past 7-day retention)
		staleDate := time.Now().AddDate(0, 0, -8).Format("2006-01-02")
		stalePath := filepath.Join(dir, "audit-"+staleDate+".jsonl")
		err := os.WriteFile(stalePath, []byte(`{"event":"old"}`+"\n"), 0o644)
		require.NoError(t, err)

		// Set file modification time to 8 days ago so cleanup picks it up
		oldTime := time.Now().AddDate(0, 0, -8)
		require.NoError(t, os.Chtimes(stalePath, oldTime, oldTime))

		// Opening a logger with 7-day retention triggers cleanup
		logger, err := audit.NewLogger(audit.LoggerConfig{
			Dir:           dir,
			RetentionDays: 7,
		})
		require.NoError(t, err)
		defer logger.Close()

		_, statErr := os.Stat(stalePath)
		assert.True(t, os.IsNotExist(statErr),
			"stale audit file (8 days old with 7-day retention) should be deleted")
	})

	t.Run("crash recovery: malformed last line is truncated on startup", func(t *testing.T) {
		// Traces to: wave2-security-layer-spec.md line 301 (Edge: audit log corruption)
		// FR-032: validate last line of audit.jsonl on startup, truncate if malformed
		dir := t.TempDir()
		logPath := filepath.Join(dir, "audit.jsonl")

		// Write a valid line then a partial/corrupt line (simulates crash mid-write)
		content := `{"event":"tool_call","decision":"allow"}` + "\n" +
			`{"event":"partial` // truncated JSON — simulates crash
		err := os.WriteFile(logPath, []byte(content), 0o644)
		require.NoError(t, err)

		// Opening logger should recover by truncating the bad last line
		logger, err := audit.NewLogger(audit.LoggerConfig{
			Dir:           dir,
			RetentionDays: 90,
		})
		require.NoError(t, err)
		defer logger.Close()

		// File should now contain only the valid first line
		data, err := os.ReadFile(logPath)
		require.NoError(t, err)

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		var lines []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				lines = append(lines, line)
			}
		}
		require.Equal(t, 1, len(lines), "only the valid line should remain after crash recovery")

		var parsed map[string]any
		err = json.Unmarshal([]byte(lines[0]), &parsed)
		assert.NoError(t, err, "remaining line should be valid JSON")
	})
}

// TestAuditLogger_RedactionPipeline is an integration test validating redaction is
// applied to all entries before they are written to disk.
// Traces to: wave2-security-layer-spec.md line 808 (TestAuditLogger_RedactionPipeline)
// BDD: Scenario: API key pattern is redacted (spec line 601) + audit writing
func TestAuditLogger_RedactionPipeline(t *testing.T) {
	// Traces to: wave2-security-layer-spec.md line 808 (TestAuditLogger_RedactionPipeline)
	dir := t.TempDir()
	logger, err := audit.NewLogger(audit.LoggerConfig{
		Dir:           dir,
		RetentionDays: 90,
		RedactEnabled: true,
	})
	require.NoError(t, err)
	defer logger.Close()

	entry := audit.Entry{
		Timestamp: time.Now().UTC(),
		Event:     audit.EventToolCall,
		Decision:  "allow",
		AgentID:   "researcher",
		Tool:      "web_search",
		Parameters: map[string]any{
			"api_key": "sk-ant-abc123def456ghi789jkl012mno345",
			"query":   "safe search query",
		},
		PolicyRule: "tools.allow matched 'web_search'",
	}
	require.NoError(t, logger.Log(&entry))

	// Read back and verify API key was redacted
	logPath := filepath.Join(dir, "audit.jsonl")
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	logContent := string(data)
	assert.NotContains(t, logContent, "sk-ant-abc123def456ghi789jkl012mno345",
		"API key must be redacted before writing to disk")
	assert.Contains(t, logContent, "[REDACTED]",
		"redacted placeholder must appear in log output")
	assert.Contains(t, logContent, "safe search query",
		"non-sensitive parameters should be preserved")
	assert.Contains(t, logContent, "web_search",
		"tool name should be preserved")
}
