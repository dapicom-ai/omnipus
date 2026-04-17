// Contract test: Plan 3 §1 acceptance decision — orphaned session directories
// (sessions whose agent has been deleted) still participate in retention policy.
//
// BDD: Given sessions exist for a deleted agent, When retention sweep runs,
//
//	Then orphaned sessions are swept just like active-agent sessions.
//
// Acceptance decision: Plan 3 §1 "Orphan session directories: kept under retention policy; shown with 'removed' badge"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/session/orphan_cleanup_test.go

package session_test

import (
	"testing"
)

// TestOrphanSessionsParticipateInRetention verifies that session JSONL files from
// a no-longer-present agent are still subject to the standard retention sweep.
//
// The UnifiedStore does not yet expose a public RetentionSweep method. Once that
// method lands, this test should:
//  1. Instantiate the real UnifiedStore in a temp dir.
//  2. Create a session for agent "orphan-agent".
//  3. Remove "orphan-agent" from the config (simulating agent deletion).
//  4. Backdate the session JSONL file so it falls outside the retention window.
//  5. Call store.RetentionSweep(retentionDays).
//  6. Call store.ListSessions() and assert the old orphan session is gone.
//  7. Assert a recent session (same orphan agent, today's date) survives.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestOrphanSessionsParticipateInRetention
func TestOrphanSessionsParticipateInRetention(t *testing.T) {
	t.Skip(
		"UnifiedStore does not yet expose a public RetentionSweep method — " +
			"tracked in Plan 3 §1: add RetentionSweep(days int) to UnifiedStore, " +
			"then implement this test against real production code",
	)
}
