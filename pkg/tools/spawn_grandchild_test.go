// Contract test: Plan 3 §1 acceptance decision — subagents are allowed to spawn
// grandchildren (unlimited depth; budget-only caps apply).
//
// BDD: Given a subagent that invokes the spawn tool, When the spawn call proceeds,
//
//	Then it is NOT blocked by any depth-limit check; budget limits apply instead.
//
// Acceptance decision: Plan 3 §1 "Subagent grandchildren: allowed (unlimited depth, budget-only caps)"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/tools/spawn_grandchild_test.go

package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubagentCanSpawnGrandchild verifies that the SpawnTool itself applies no
// hardcoded depth limit. The only guard is the budget-level mechanism (cost cap,
// max_tool_iterations) — not a fixed recursion depth counter in the tool.
//
// This test verifies the absence of a hardcoded depth guard in the spawn tool's
// Execute path by inspecting the tool's parameter schema (no "depth" or "max_depth"
// field) and by verifying the tool does not reject calls based on a depth argument.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestSubagentCanSpawnGrandchild
func TestSubagentCanSpawnGrandchild(t *testing.T) {
	// Build a minimal SpawnTool. Without a SubagentManager, Execute will return an
	// error about missing spawner — but the error must NOT mention "depth limit".
	tool := &SpawnTool{}

	// BDD: When Execute is called with a task (simulating a grandchild spawn attempt).
	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"task":  "grandchild task from subagent",
		"label": "grandchild",
	})

	// BDD: Then the result must NOT contain a "depth limit" error message.
	require.NotNil(t, result, "Execute must return a result (not nil)")
	if result.IsError {
		// An error is expected (no spawner configured), but it must NOT be a depth error.
		assert.NotContains(t, result.ForLLM, "depth",
			"spawn error must not mention depth — there is no depth limit in the tool")
		assert.NotContains(t, result.ForLLM, "max_depth",
			"spawn error must not mention max_depth — depth limits are budget-based, not tool-based")
		assert.NotContains(t, result.ForLLM, "recursion",
			"spawn error must not mention recursion limit — not a tool-level concern")
	}

	// Verify the tool parameters schema has no depth-related field.
	params := tool.Parameters()
	require.NotNil(t, params)
	props, _ := params["properties"].(map[string]any)
	require.NotNil(t, props, "SpawnTool must have a properties schema")

	_, hasDepth := props["depth"]
	_, hasMaxDepth := props["max_depth"]
	assert.False(t, hasDepth,
		"SpawnTool schema must not have a 'depth' parameter — depth is not a tool-level concept")
	assert.False(t, hasMaxDepth,
		"SpawnTool schema must not have a 'max_depth' parameter — depth limits are budget-only")

	// Differentiation: the "task" parameter IS present (normal invocation field).
	_, hasTask := props["task"]
	assert.True(t, hasTask,
		"SpawnTool schema must have a 'task' parameter — this is the primary input")
}
