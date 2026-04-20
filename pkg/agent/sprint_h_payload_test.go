// Sprint H agent payload tests — FR-H-002, FR-H-003
// Traces to: sprint-h-subagent-block-spec.md TDD row 4.

package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestToolExecStartPayload_CarriesParentSpawnCallID verifies FR-H-002:
// ToolExecStartPayload and ToolExecEndPayload carry ParentSpawnCallID.
// Traces to: sprint-h-subagent-block-spec.md TDD row 4.
func TestToolExecStartPayload_CarriesParentSpawnCallID(t *testing.T) {
	t.Run("ToolExecStartPayload has ParentSpawnCallID field", func(t *testing.T) {
		p := ToolExecStartPayload{
			ToolCallID:        "t1",
			ChatID:            "chat-abc",
			Tool:              "fs.list",
			Arguments:         map[string]any{"path": "/tmp"},
			ParentSpawnCallID: "c1",
		}
		assert.Equal(t, "c1", p.ParentSpawnCallID,
			"ToolExecStartPayload.ParentSpawnCallID must be settable and readable")
		assert.Empty(t, ToolExecStartPayload{}.ParentSpawnCallID,
			"ParentSpawnCallID must default to empty string for top-level tool calls")
	})

	t.Run("ToolExecEndPayload has ParentSpawnCallID field", func(t *testing.T) {
		p := ToolExecEndPayload{
			ToolCallID:        "t1",
			ChatID:            "chat-abc",
			Tool:              "fs.list",
			IsError:           false,
			ParentSpawnCallID: "c1",
		}
		assert.Equal(t, "c1", p.ParentSpawnCallID,
			"ToolExecEndPayload.ParentSpawnCallID must be settable and readable")
		assert.Empty(t, ToolExecEndPayload{}.ParentSpawnCallID,
			"ParentSpawnCallID must default to empty string for top-level tool calls")
	})
}

// TestSpawnToolCallIDContext verifies the context helpers that thread the spawn tool
// call ID down to spawnSubTurn (FR-H-003).
func TestSpawnToolCallIDContext(t *testing.T) {
	t.Run("withSpawnToolCallID injects and spawnToolCallIDFromContext retrieves", func(t *testing.T) {
		ctx := context.Background()
		assert.Empty(t, spawnToolCallIDFromContext(ctx),
			"empty context must return empty string")

		ctx2 := withSpawnToolCallID(ctx, "spawn-call-123")
		assert.Equal(t, "spawn-call-123", spawnToolCallIDFromContext(ctx2),
			"injected ID must be retrievable from context")

		// Original context must be unaffected.
		assert.Empty(t, spawnToolCallIDFromContext(ctx),
			"original context must not be mutated by withSpawnToolCallID")
	})

	t.Run("nested override — innermost wins", func(t *testing.T) {
		ctx := withSpawnToolCallID(context.Background(), "outer")
		ctx2 := withSpawnToolCallID(ctx, "inner")
		assert.Equal(t, "inner", spawnToolCallIDFromContext(ctx2))
		assert.Equal(t, "outer", spawnToolCallIDFromContext(ctx))
	})
}

// TestSubTurnSpawnPayload_HasSpanFields verifies FR-H-004:
// SubTurnSpawnPayload and SubTurnEndPayload carry the required span fields.
func TestSubTurnSpawnPayload_HasSpanFields(t *testing.T) {
	t.Run("SubTurnSpawnPayload span fields", func(t *testing.T) {
		p := SubTurnSpawnPayload{
			AgentID:           "max",
			Label:             "subturn-1",
			ParentTurnID:      "turn-abc",
			SpanID:            "span_c1",
			ParentSpawnCallID: "c1",
			TaskLabel:         "audit go files",
			ChatID:            "chat-xyz",
		}
		assert.Equal(t, "span_c1", p.SpanID)
		assert.Equal(t, "c1", p.ParentSpawnCallID)
		assert.Equal(t, "audit go files", p.TaskLabel)
		assert.Equal(t, "chat-xyz", p.ChatID)
	})

	t.Run("SubTurnEndPayload span fields", func(t *testing.T) {
		p := SubTurnEndPayload{
			AgentID:           "max",
			Status:            "completed",
			SpanID:            "span_c1",
			ParentSpawnCallID: "c1",
			DurationMS:        4210,
			ChatID:            "chat-xyz",
		}
		assert.Equal(t, "span_c1", p.SpanID)
		assert.Equal(t, "c1", p.ParentSpawnCallID)
		assert.Equal(t, int64(4210), p.DurationMS)
		assert.Equal(t, "chat-xyz", p.ChatID)
	})
}
