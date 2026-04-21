// Sprint H schema tests — FR-H-001
// Traces to: sprint-h-subagent-block-spec.md TDD rows 1 & 9.

package session

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolCall_JSONRoundtrip_WithParentToolCallID verifies FR-H-001:
// ToolCall.ParentToolCallID is preserved through JSON marshal/unmarshal,
// and the omitempty tag ensures it is absent from the wire when empty.
// Traces to: sprint-h-subagent-block-spec.md TDD row 1.
func TestToolCall_JSONRoundtrip_WithParentToolCallID(t *testing.T) {
	t.Run("parent_tool_call_id is preserved when set", func(t *testing.T) {
		original := ToolCall{
			ID:               "t1",
			Tool:             "fs.list",
			Status:           "success",
			DurationMS:       120,
			ParentToolCallID: "c1",
		}
		data, err := json.Marshal(original)
		require.NoError(t, err, "ToolCall must marshal without error")

		var roundTripped ToolCall
		require.NoError(t, json.Unmarshal(data, &roundTripped), "ToolCall must unmarshal without error")

		assert.Equal(t, original.ID, roundTripped.ID)
		assert.Equal(t, original.Tool, roundTripped.Tool)
		assert.Equal(t, original.Status, roundTripped.Status)
		assert.Equal(t, original.DurationMS, roundTripped.DurationMS)
		// FR-H-001: ParentToolCallID must survive the round-trip.
		assert.Equal(t, ToolCallID("c1"), roundTripped.ParentToolCallID,
			"ParentToolCallID must be preserved through JSON round-trip")
	})

	t.Run("parent_tool_call_id absent from JSON when empty (omitempty)", func(t *testing.T) {
		tc := ToolCall{
			ID:     "t2",
			Tool:   "shell",
			Status: "error",
		}
		data, err := json.Marshal(tc)
		require.NoError(t, err)

		// The field must be absent when empty (omitempty).
		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		_, hasField := raw["parent_tool_call_id"]
		assert.False(t, hasField,
			"parent_tool_call_id must be absent from JSON when empty (omitempty)")
	})

	t.Run("JSON key is parent_tool_call_id (snake_case)", func(t *testing.T) {
		tc := ToolCall{
			ID:               "t3",
			Tool:             "git.log",
			Status:           "success",
			ParentToolCallID: "spawn-abc",
		}
		data, err := json.Marshal(tc)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		val, ok := raw["parent_tool_call_id"]
		require.True(t, ok, "JSON key must be parent_tool_call_id")
		assert.Equal(t, "spawn-abc", val)
	})
}
