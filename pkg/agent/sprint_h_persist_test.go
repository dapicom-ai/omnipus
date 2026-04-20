// Sprint H persistence test — FR-H-001, FR-H-003
// Traces to: sprint-h-subagent-block-spec.md TDD row 9.

package agent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/session"
)

// TestSpawn_PersistsParentToolCallIDOnChildren verifies FR-H-001 + FR-H-003:
// When a tool call executes inside a sub-turn (parentSpawnCallID set on childTS),
// the resulting ToolCall.ParentToolCallID equals the parent spawn's ToolCall.ID.
//
// This test sets up a real transcript store, constructs a child turnState with
// parentSpawnCallID set, and calls appendToolCallTranscript — then reads back the
// persisted transcript to confirm ParentToolCallID was recorded correctly.
// Traces to: sprint-h-subagent-block-spec.md TDD row 9.
func TestSpawn_PersistsParentToolCallIDOnChildren(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sprint-h-persist-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Set up a real session store so appendToolCallTranscript writes to disk.
	store, err := session.NewUnifiedStore(tmpDir)
	require.NoError(t, err)

	sessionID, err := session.NewSessionID()
	require.NoError(t, err)

	// Build a turnState with parentSpawnCallID set (simulating a child sub-turn).
	ts := &turnState{
		agentID:             "max",
		parentSpawnCallID:   "c1",
		transcriptStore:     store,
		transcriptSessionID: sessionID,
	}

	// Construct the ToolCall exactly as loop.go does (FR-H-001):
	// ParentToolCallID = ts.parentSpawnCallID
	tc := session.ToolCall{
		ID:               "t1",
		Tool:             "fs.list",
		Status:           "success",
		DurationMS:       100,
		ParentToolCallID: ts.parentSpawnCallID,
	}
	ts.appendToolCallTranscript(tc)

	// Read back the transcript from disk.
	entries, err := store.ReadTranscript(sessionID)
	require.NoError(t, err, "ReadTranscript must succeed after appendToolCallTranscript")
	require.NotEmpty(t, entries, "at least one transcript entry must be present")

	// Find the tool call entry.
	var found bool
	for _, entry := range entries {
		for _, call := range entry.ToolCalls {
			if call.ID == "t1" {
				found = true
				// FR-H-001: ParentToolCallID must equal the parent spawn's ToolCall.ID.
				assert.Equal(t, "c1", call.ParentToolCallID,
					"ToolCall.ParentToolCallID must be persisted correctly (FR-H-001, FR-H-003)")
				assert.Equal(t, "fs.list", call.Tool)
			}
		}
	}
	assert.True(t, found, "tool call t1 must be found in the persisted transcript")
}

// TestSpawn_TopLevel_NoParentToolCallID verifies that top-level tool calls
// (parentSpawnCallID == "") result in an empty ParentToolCallID in the transcript.
func TestSpawn_TopLevel_NoParentToolCallID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sprint-h-persist-top-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	store, err := session.NewUnifiedStore(tmpDir)
	require.NoError(t, err)

	sessionID, err := session.NewSessionID()
	require.NoError(t, err)

	ts := &turnState{
		agentID:             "max",
		parentSpawnCallID:   "", // empty — top-level turn
		transcriptStore:     store,
		transcriptSessionID: sessionID,
	}

	tc := session.ToolCall{
		ID:               "t2",
		Tool:             "shell",
		Status:           "success",
		ParentToolCallID: ts.parentSpawnCallID, // empty
	}
	ts.appendToolCallTranscript(tc)

	entries, err := store.ReadTranscript(sessionID)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		for _, call := range entry.ToolCalls {
			if call.ID == "t2" {
				assert.Empty(t, call.ParentToolCallID,
					"top-level tool calls must have empty ParentToolCallID in transcript")
			}
		}
	}
}
