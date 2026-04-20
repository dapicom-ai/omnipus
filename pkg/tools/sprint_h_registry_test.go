// Sprint H registry tests — FR-H-006
// Traces to: sprint-h-subagent-block-spec.md TDD rows 2 & 3, BDD Scenario 9.

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolRegistry_CloneExcept_OmitsNamed verifies FR-H-006:
// CloneExcept(ExcludedSpawn, ExcludedHandoff) produces a registry without those tools
// but with all other tools intact.
// Traces to: sprint-h-subagent-block-spec.md TDD row 2, BDD Scenario 9.
func TestToolRegistry_CloneExcept_OmitsNamed(t *testing.T) {
	r := NewToolRegistry()

	// Register three tools: spawn, handoff, and a neutral one (read_file).
	// We use concrete types available in the tools package.
	spawnTool := &SpawnTool{}
	handoffTool := &HandoffTool{}
	otherTool := &ReadFileTool{} // a non-excluded tool

	r.Register(spawnTool)
	r.Register(handoffTool)
	r.Register(otherTool)

	// Verify all three are in the parent before cloning.
	_, hasSpawn := r.Get("spawn")
	_, hasHandoff := r.Get("handoff")
	_, hasReadFile := r.Get("read_file")
	require.True(t, hasSpawn, "spawn must be in the parent registry")
	require.True(t, hasHandoff, "handoff must be in the parent registry")
	require.True(t, hasReadFile, "read_file must be in the parent registry")

	// Construct the child registry as spawnSubTurn does.
	child := r.CloneExcept(ExcludedSpawn, ExcludedHandoff)

	// FR-H-006: "spawn" must be absent.
	childSpawn, childHasSpawn := child.Get("spawn")
	assert.False(t, childHasSpawn, "spawn must not be in the child registry after CloneExcept")
	assert.Nil(t, childSpawn)

	// FR-H-006: "handoff" must be absent.
	childHandoff, childHasHandoff := child.Get("handoff")
	assert.False(t, childHasHandoff, "handoff must not be in the child registry after CloneExcept")
	assert.Nil(t, childHandoff)

	// Non-excluded tools must be present.
	childReadFile, childHasReadFile := child.Get("read_file")
	assert.True(t, childHasReadFile, "read_file must remain in the child registry (not excluded)")
	assert.NotNil(t, childReadFile)

	// Verify clone is independent: registering a new tool on child does not affect parent.
	child.Register(&MessageTool{})
	_, parentHasMessage := r.Get("message")
	assert.False(t, parentHasMessage,
		"registering on child must not pollute parent registry (independent copy)")
}

// TestToolRegistry_CloneExcept_EmptyNames verifies that CloneExcept() with no names
// behaves like Clone() — all tools are present.
func TestToolRegistry_CloneExcept_EmptyNames(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&SpawnTool{})

	clone := r.CloneExcept() // no exclusions

	_, hasSpawn := clone.Get("spawn")
	assert.True(t, hasSpawn, "CloneExcept with no names must keep all tools (same as Clone)")
}

// TestSubTurn_ChildRegistry_OmitsSpawnAndHandoff verifies the registry used in
// sub-turns does not contain "spawn" or "handoff" — the enforcement point is
// CloneExcept in spawnSubTurn (FR-H-006).
// This is a structural test complementing the functional grandchild test.
// Traces to: sprint-h-subagent-block-spec.md TDD row 3, BDD Scenario 9.
func TestSubTurn_ChildRegistry_OmitsSpawnAndHandoff(t *testing.T) {
	// Build a registry that contains both excluded tools plus extras.
	r := NewToolRegistry()
	r.Register(&SpawnTool{})
	r.Register(&HandoffTool{})
	r.Register(&ReadFileTool{})

	child := r.CloneExcept(ExcludedSpawn, ExcludedHandoff)

	// The child registry must have exactly read_file, no spawn, no handoff.
	childNames := child.List()

	hasSpawnInList := false
	hasHandoffInList := false
	hasReadFileInList := false
	for _, name := range childNames {
		switch name {
		case "spawn":
			hasSpawnInList = true
		case "handoff":
			hasHandoffInList = true
		case "read_file":
			hasReadFileInList = true
		}
	}

	assert.False(t, hasSpawnInList,
		"spawn must not appear in child.List() — grandchildren are forbidden")
	assert.False(t, hasHandoffInList,
		"handoff must not appear in child.List()")
	assert.True(t, hasReadFileInList,
		"read_file must appear in child.List() — non-excluded tools are kept")

	assert.Equal(t, r.Count()-2, child.Count(),
		"child registry must have exactly 2 fewer tools than parent (spawn and handoff excluded)")
}
