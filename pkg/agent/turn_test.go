package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetActiveAgentIDs_Empty verifies that GetActiveAgentIDs returns an empty (or nil)
// slice when no turns are active.
// BDD: Given an AgentLoop with no active turns,
// When GetActiveAgentIDs() is called,
// Then it returns an empty (zero-length) slice.
// Traces to: vivid-roaming-planet.md line 162
func TestGetActiveAgentIDs_Empty(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	ids := al.GetActiveAgentIDs()
	assert.Empty(t, ids, "no active turns must yield an empty slice")
}

// TestGetActiveAgentIDs_SingleTurn verifies that GetActiveAgentIDs returns the agent ID
// of the one active turn.
// BDD: Given an AgentLoop with one active turn for agentID "test-agent",
// When GetActiveAgentIDs() is called,
// Then it returns a slice containing "test-agent".
// Traces to: vivid-roaming-planet.md line 163
func TestGetActiveAgentIDs_SingleTurn(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	ts := &turnState{agentID: "test-agent"}
	al.activeTurnStates.Store("session-key-1", ts)
	defer al.activeTurnStates.Delete("session-key-1")

	ids := al.GetActiveAgentIDs()
	assert.Contains(t, ids, "test-agent", "active turn agent must be in the returned slice")
	assert.Len(t, ids, 1)
}

// TestGetActiveAgentIDs_MultipleTurnsSameAgent verifies that two turns with the same
// agentID are deduplicated in the result.
// BDD: Given an AgentLoop with two active turns both having agentID "shared-agent",
// When GetActiveAgentIDs() is called,
// Then it returns a slice with exactly one entry "shared-agent".
// Traces to: vivid-roaming-planet.md line 164
func TestGetActiveAgentIDs_MultipleTurnsSameAgent(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	ts1 := &turnState{agentID: "shared-agent"}
	ts2 := &turnState{agentID: "shared-agent"}
	al.activeTurnStates.Store("session-key-a", ts1)
	al.activeTurnStates.Store("session-key-b", ts2)
	defer al.activeTurnStates.Delete("session-key-a")
	defer al.activeTurnStates.Delete("session-key-b")

	ids := al.GetActiveAgentIDs()
	assert.Contains(t, ids, "shared-agent")
	// Must deduplicate — exactly one entry for the shared agent ID.
	count := 0
	for _, id := range ids {
		if id == "shared-agent" {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate agentID across turns must be deduplicated")
}

// TestGetActiveAgentIDs_MultipleDifferentAgents verifies that all unique agent IDs from
// different active turns are returned.
// BDD: Given an AgentLoop with active turns for "agent-a" and "agent-b",
// When GetActiveAgentIDs() is called,
// Then it returns a slice containing both "agent-a" and "agent-b".
// Traces to: vivid-roaming-planet.md line 165
func TestGetActiveAgentIDs_MultipleDifferentAgents(t *testing.T) {
	al, _, _, _, cleanup := newTestAgentLoop(t)
	defer cleanup()

	tsA := &turnState{agentID: "agent-a"}
	tsB := &turnState{agentID: "agent-b"}
	al.activeTurnStates.Store("session-key-x", tsA)
	al.activeTurnStates.Store("session-key-y", tsB)
	defer al.activeTurnStates.Delete("session-key-x")
	defer al.activeTurnStates.Delete("session-key-y")

	ids := al.GetActiveAgentIDs()
	assert.Contains(t, ids, "agent-a", "agent-a must be in the result")
	assert.Contains(t, ids, "agent-b", "agent-b must be in the result")
	assert.Len(t, ids, 2, "must return exactly two distinct agent IDs")
}
