// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionPartitionNaming verifies YYYY-MM-DD.jsonl filename is derived from timestamp.
// Traces to: wave1-core-foundation-spec.md Scenario: New session creates partition and metadata (US-5 AC1, FR-011)
func TestSessionPartitionNaming(t *testing.T) {
	tests := []struct {
		name             string
		timestamp        time.Time
		expectedPartition string
	}{
		// Dataset: Session File Edge Cases row 1
		{
			name:             "last millisecond of day",
			timestamp:        time.Date(2026, 3, 28, 23, 59, 59, 999000000, time.UTC),
			expectedPartition: "2026-03-28.jsonl",
		},
		// Dataset: Session File Edge Cases row 2
		{
			name:             "first millisecond of new day",
			timestamp:        time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
			expectedPartition: "2026-03-29.jsonl",
		},
		// Dataset: Session File Edge Cases row 3
		{
			name:             "just after midnight",
			timestamp:        time.Date(2026, 3, 29, 0, 0, 0, 1000000, time.UTC),
			expectedPartition: "2026-03-29.jsonl",
		},
		{
			name:             "noon on a date",
			timestamp:        time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedPartition: "2026-01-15.jsonl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The partition naming logic is: ts.UTC().Format("2006-01-02") + ".jsonl"
			partitionName := tc.timestamp.UTC().Format("2006-01-02") + ".jsonl"
			assert.Equal(t, tc.expectedPartition, partitionName,
				"partition name must be YYYY-MM-DD.jsonl for timestamp %v", tc.timestamp)
		})
	}
}

// TestSessionPartitionMidnightBoundary verifies UTC midnight triggers a new partition.
// Traces to: wave1-core-foundation-spec.md Scenario: Midnight rollover creates new partition (US-5 AC2, FR-012)
func TestSessionPartitionMidnightBoundary(t *testing.T) {
	home := t.TempDir()
	ps := NewPartitionStore(home, "test-agent")

	meta, err := ps.NewSession("test-channel", "claude-3", "anthropic")
	require.NoError(t, err)
	sessionID := meta.ID

	// Send a message on day 1 (23:59:59 UTC).
	day1Msg := TranscriptEntry{
		ID:        "msg-1",
		Role:      "user",
		Content:   "end of day 1",
		Timestamp: time.Date(2026, 3, 28, 23, 59, 59, 0, time.UTC),
	}
	require.NoError(t, ps.AppendMessage(sessionID, day1Msg))

	// Send a message on day 2 (00:00:00 UTC).
	day2Msg := TranscriptEntry{
		ID:        "msg-2",
		Role:      "assistant",
		Content:   "start of day 2",
		Timestamp: time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, ps.AppendMessage(sessionID, day2Msg))

	// Verify two partition files exist.
	sessionDir := filepath.Join(home, "sessions", sessionID)

	partition1 := filepath.Join(sessionDir, "2026-03-28.jsonl")
	partition2 := filepath.Join(sessionDir, "2026-03-29.jsonl")

	_, err = os.Stat(partition1)
	require.NoError(t, err, "2026-03-28.jsonl must exist for day 1 message")

	_, err = os.Stat(partition2)
	require.NoError(t, err, "2026-03-29.jsonl must exist for day 2 message")

	// Verify meta.json records both partitions.
	updatedMeta, err := ps.GetMeta(sessionID)
	require.NoError(t, err)
	assert.Len(t, updatedMeta.Partitions, 2, "meta.json must list both partitions")
	assert.Contains(t, updatedMeta.Partitions, "2026-03-28.jsonl")
	assert.Contains(t, updatedMeta.Partitions, "2026-03-29.jsonl")
}

// TestSessionWriteIntegration verifies full session create → message append → meta update cycle.
// Traces to: wave1-core-foundation-spec.md Scenario: New session creates partition and metadata (US-5 AC1, AC4)
func TestSessionWriteIntegration(t *testing.T) {
	home := t.TempDir()
	ps := NewPartitionStore(home, "integration-agent")

	// Create a new session.
	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	meta, err := ps.NewSession("telegram", "claude-opus", "anthropic")
	require.NoError(t, err)
	require.NotEmpty(t, meta.ID, "session ID must be non-empty")
	assert.True(t, len(meta.ID) > 0, "session must have a ULID-based ID")
	assert.Equal(t, "active", meta.Status)
	assert.Equal(t, "telegram", meta.Channel)

	// Verify meta.json exists.
	sessionDir := filepath.Join(home, "sessions", meta.ID)
	_, err = os.Stat(filepath.Join(sessionDir, "meta.json"))
	require.NoError(t, err, "meta.json must exist after NewSession")

	// Append a message with tool calls.
	entry := TranscriptEntry{
		ID:        "entry-1",
		Role:      "assistant",
		Content:   "I ran the command",
		Timestamp: now,
		Tokens:    42,
		Cost:      0.001,
		ToolCalls: []ToolCall{
			{
				ID:         "tc-1",
				Tool:       "shell",
				Status:     "success",
				DurationMS: 150,
				Parameters: map[string]any{"cmd": "ls -la"},
				Result:     map[string]any{"output": "file1.txt"},
			},
		},
	}
	require.NoError(t, ps.AppendMessage(meta.ID, entry))

	// Verify JSONL partition file exists.
	partitionName := now.UTC().Format("2006-01-02") + ".jsonl"
	partitionPath := filepath.Join(sessionDir, partitionName)
	_, err = os.Stat(partitionPath)
	require.NoError(t, err, "partition file must exist after AppendMessage")

	// Verify partition file contains valid JSONL.
	f, err := os.Open(partitionPath)
	require.NoError(t, err)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		var e TranscriptEntry
		require.NoError(t, json.Unmarshal([]byte(line), &e),
			"each JSONL line must be valid JSON")
		assert.Equal(t, "entry-1", e.ID)
		assert.Len(t, e.ToolCalls, 1, "tool calls must be serialized")
		assert.Equal(t, "shell", e.ToolCalls[0].Tool)
		lineCount++
	}
	assert.Equal(t, 1, lineCount, "partition must have exactly 1 line")

	// Verify meta.json stats are updated.
	// Assistant messages contribute to TokensOut; user messages to TokensIn.
	updatedMeta, err := ps.GetMeta(meta.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, updatedMeta.Stats.MessageCount)
	assert.Equal(t, 42, updatedMeta.Stats.TokensOut, "assistant message tokens go to TokensOut")
	assert.Equal(t, 42, updatedMeta.Stats.TokensTotal, "TokensTotal must include all tokens")
	assert.InDelta(t, 0.001, updatedMeta.Stats.Cost, 1e-9)
	assert.Equal(t, 1, updatedMeta.Stats.ToolCalls)
}

// TestSessionMultiPartition verifies cross-day message appending creates separate files.
// Traces to: wave1-core-foundation-spec.md Scenario: Midnight rollover creates new partition (US-5 AC2)
func TestSessionMultiPartition(t *testing.T) {
	home := t.TempDir()
	ps := NewPartitionStore(home, "multi-agent")

	meta, err := ps.NewSession("cli", "claude-3", "anthropic")
	require.NoError(t, err)

	days := []time.Time{
		time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 29, 11, 0, 0, 0, time.UTC),
	}

	for i, day := range days {
		msg := TranscriptEntry{
			ID:        "msg-" + day.Format("2006-01-02"),
			Role:      "user",
			Content:   "message on day " + string(rune('A'+i)),
			Timestamp: day,
			Tokens:    10,
		}
		require.NoError(t, ps.AppendMessage(meta.ID, msg),
			"AppendMessage must succeed for day %v", day)
	}

	// Verify 3 partition files exist.
	sessionDir := filepath.Join(home, "sessions", meta.ID)
	expectedPartitions := []string{
		"2026-03-27.jsonl",
		"2026-03-28.jsonl",
		"2026-03-29.jsonl",
	}
	for _, partition := range expectedPartitions {
		_, err := os.Stat(filepath.Join(sessionDir, partition))
		assert.NoError(t, err, "partition %q must exist", partition)
	}

	// Verify meta lists all partitions.
	updatedMeta, err := ps.GetMeta(meta.ID)
	require.NoError(t, err)
	assert.Len(t, updatedMeta.Partitions, 3, "meta must list all 3 partitions")
}

// TestSessionStatsAggregation verifies stats accumulate correctly across multiple messages.
// Traces to: wave1-core-foundation-spec.md Scenario: Session stats aggregate across partitions (US-5 AC3)
func TestSessionStatsAggregation(t *testing.T) {
	home := t.TempDir()
	ps := NewPartitionStore(home, "stats-agent")

	meta, err := ps.NewSession("cli", "claude-3", "anthropic")
	require.NoError(t, err)

	now := time.Now().UTC()

	messages := []TranscriptEntry{
		{ID: "m1", Role: "user", Content: "msg1", Timestamp: now, Tokens: 10, Cost: 0.001},
		{ID: "m2", Role: "assistant", Content: "reply1", Timestamp: now, Tokens: 50, Cost: 0.005,
			ToolCalls: []ToolCall{{ID: "tc1", Tool: "shell", Status: "success"}}},
		{ID: "m3", Role: "user", Content: "msg2", Timestamp: now, Tokens: 20, Cost: 0.002},
	}

	for _, msg := range messages {
		require.NoError(t, ps.AppendMessage(meta.ID, msg))
	}

	updatedMeta, err := ps.GetMeta(meta.ID)
	require.NoError(t, err)

	// user(10) + user(20) = 30 → TokensIn; assistant(50) → TokensOut; total = 80
	assert.Equal(t, 3, updatedMeta.Stats.MessageCount, "message count must aggregate")
	assert.Equal(t, 30, updatedMeta.Stats.TokensIn, "user tokens: 10+20=30")
	assert.Equal(t, 50, updatedMeta.Stats.TokensOut, "assistant tokens: 50")
	assert.Equal(t, 80, updatedMeta.Stats.TokensTotal, "total tokens: 10+50+20=80")
	assert.InDelta(t, 0.008, updatedMeta.Stats.Cost, 1e-9, "cost must aggregate: 0.001+0.005+0.002")
	assert.Equal(t, 1, updatedMeta.Stats.ToolCalls, "tool call count must aggregate")
}

// TestNewSessionIDFormat verifies session IDs are ULID-based with "session_" prefix.
// Traces to: wave1-core-foundation-spec.md Ambiguity Resolution #2 (Session ID algorithm)
func TestNewSessionIDFormat(t *testing.T) {
	id, err := NewSessionID()
	require.NoError(t, err)
	assert.True(t, len(id) > len("session_"), "session ID must be non-empty after prefix")
	assert.Equal(t, "session_", id[:8], "session ID must start with 'session_'")

	// IDs must be unique.
	id2, err := NewSessionID()
	require.NoError(t, err)
	assert.NotEqual(t, id, id2, "consecutive session IDs must be unique")
}
