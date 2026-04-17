// Contract test: Plan 3 §1 acceptance decision — three identical consecutive tool
// calls in a row must inject a "loop detected" tool-result so the LLM can adapt.
//
// BDD: Given the same tool call is made 3× in a row with identical args,
//
//	When the agent loop processes the third identical call,
//	Then a "loop detected" synthetic tool-result is injected before the LLM call.
//
// Acceptance decision: Plan 3 §1 "Identical tool call 3× in a row: inject loop-detected tool-result"
// Traces to: temporal-puzzling-melody.md §4 Axis-1, pkg/tools/loop_detection_test.go

package tools_test

import (
	"testing"
)

// TestThreeIdenticalToolCallsInjectLoopDetected verifies that the agent loop
// loop-detection mechanism fires on the third consecutive identical tool call.
//
// This contract is implemented in the agent loop (runTurn) not in the tools package.
// The test is placed here as a discoverable contract marker; the implementation
// assertion is done via an integration test once the feature lands.
//
// Traces to: temporal-puzzling-melody.md §4 Axis-1 — TestThreeIdenticalToolCallsInjectLoopDetected
func TestThreeIdenticalToolCallsInjectLoopDetected(t *testing.T) {
	t.Skip("contract pending implementation — tracked in Plan 3 §1; " +
		"loop detection (3× identical tool call → inject loop-detected result) " +
		"is not yet implemented in pkg/agent/loop.go runTurn. " +
		"When implemented, remove this skip and assert via ScenarioProvider that " +
		"the third duplicate produces a 'loop_detected' tool result before the next LLM call.")

	// When implemented, the test should:
	// 1. Boot an AgentLoop with a ScenarioProvider that returns the same tool call 3×.
	// 2. Drive the loop through 3 identical consecutive tool calls (same name, same args).
	// 3. Assert that the third iteration sees an injected tool-result with
	//    Content containing "loop" or "repeated" before the next LLM request.
	// 4. Assert that the LLM's 4th call sees the synthetic result in its message history.
	// 5. Differentiation: 2 identical calls must NOT trigger injection; only 3+ does.
}
