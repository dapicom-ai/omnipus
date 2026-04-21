// REVERSAL NOTICE — 2026-04-20 (owner decision)
//
// Plan 3 §1 / temporal-puzzling-melody.md §4 Axis-1 accepted "subagent grandchildren:
// allowed, unlimited depth, budget-only caps". That decision is FORMALLY REVERSED in
// Sprint H (sprint-h-subagent-block, 2026-04-20).
//
// Rationale (owner): "unlimited grandchildren is not an option; one level only for
// general subagents; we will improve that in the future."
//
// The prior test TestSubagentCanSpawnGrandchild asserted the reversed behavior. This
// test (TestSubagentCannotSpawnGrandchild) asserts the NEW contract:
//   - A sub-turn's tool registry is constructed via CloneExcept("spawn","handoff").
//   - "spawn" and "handoff" are absent from the registry.
//   - Any LLM tool call for "spawn" inside a sub-turn receives an unknown-tool error.
//   - No grandchild subagent_start frame is emitted.
//
// The enforcement is at the registry level (FR-H-006), not a depth check in the tool.
// Traces to: sprint-h-subagent-block-spec.md FR-H-006, FR-H-007, US-3, BDD Scenario 9 & 10.

package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubagentCannotSpawnGrandchild verifies the REVERSAL of Plan 3 §1:
// subagents cannot spawn grandchildren because "spawn" is absent from their tool registry.
//
// The child sub-turn's registry is constructed via CloneExcept("spawn","handoff") in
// pkg/agent/subturn.go::spawnSubTurn. With "spawn" absent from the registry, any LLM
// tool call for "spawn" is dispatched to ExecuteWithContext which returns an unknown-tool
// error — no new sub-turn is created, no subagent_start frame is emitted.
//
// This test verifies the registry-level enforcement directly.
func TestSubagentCannotSpawnGrandchild(t *testing.T) {
	// Build a parent registry with spawn + subagent tools registered.
	// Both are delegation tools — the async `spawn` and the sync `subagent`
	// variant — and both must be filtered out of the child's registry.
	parentRegistry := NewToolRegistry()
	spawnTool := &SpawnTool{} // no spawner — only used for registration
	parentRegistry.Register(spawnTool)
	subagentTool := &SubagentTool{} // no manager — only used for registration
	parentRegistry.Register(subagentTool)

	// Verify both delegation tools ARE in the parent registry.
	parent, ok := parentRegistry.Get("spawn")
	require.True(t, ok, "spawn must be present in the parent registry before CloneExcept")
	require.NotNil(t, parent)
	parentSubagent, okSubagent := parentRegistry.Get("subagent")
	require.True(t, okSubagent, "subagent must be present in the parent registry before CloneExcept")
	require.NotNil(t, parentSubagent)

	// Construct the child registry as spawnSubTurn does (FR-H-006).
	// All three delegation tools are excluded: spawn, subagent, handoff.
	childRegistry := parentRegistry.CloneExcept(ExcludedSpawn, ExcludedSubagent, ExcludedHandoff)

	// BDD: Then "spawn" is absent from the child registry.
	childSpawn, childHasSpawn := childRegistry.Get("spawn")
	assert.False(t, childHasSpawn,
		"spawn must NOT be in the child registry — grandchildren are forbidden (Plan 3 §1 reversal)")
	assert.Nil(t, childSpawn,
		"spawn tool must be nil in the child registry")

	// BDD: And "subagent" (sync delegation variant) is absent too.
	childSubagent, childHasSubagent := childRegistry.Get("subagent")
	assert.False(t, childHasSubagent,
		"subagent must NOT be in the child registry — sync delegation is also grandchild-forbidden")
	assert.Nil(t, childSubagent,
		"subagent tool must be nil in the child registry")

	// BDD: And "handoff" is absent from the child registry.
	childHandoff, childHasHandoff := childRegistry.Get("handoff")
	assert.False(t, childHasHandoff,
		"handoff must NOT be in the child registry — one level only (Plan 3 §1 reversal)")
	assert.Nil(t, childHandoff,
		"handoff tool must be nil in the child registry")

	// BDD: When the child registry tries to execute "spawn", it returns an unknown-tool error.
	result := childRegistry.ExecuteWithContext(
		context.Background(),
		"spawn",
		map[string]any{"task": "grandchild task"},
		"", "", nil,
	)
	require.NotNil(t, result, "ExecuteWithContext must return a non-nil result")
	assert.True(t, result.IsError,
		"executing spawn in a child registry must return an error result")
	assert.Contains(t, result.ForLLM, "not found",
		"the error must indicate the tool is not found (unknown-tool error), not a depth error")

	// The error must NOT mention "depth" — depth is not the enforcement mechanism.
	assert.NotContains(t, result.ForLLM, "depth",
		"unknown-tool error must not mention depth — the enforcement is registry-level, not depth-level")
}
