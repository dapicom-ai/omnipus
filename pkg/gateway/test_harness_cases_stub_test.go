//go:build !cgo && !test_harness

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

// Non-test_harness build: skip-stubs for all test helper functions that
// require the HarnessQueue type (only present in test_harness builds).
//
// These stubs allow the test binary to compile and run without -tags test_harness.
// Every function simply calls t.Skip() so the test results remain green in all
// build configurations.

import "testing"

// resetProcessHarnessQueueForTest is a no-op in non-test_harness builds.
// The processHarnessQueue singleton does not exist without the tag.
func resetProcessHarnessQueueForTest() {}

func testHarnessQueueFIFOOrder(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — HarnessQueue not compiled in this build")
}

func testHarnessQueueEmptyReturnsErrQueueEmpty(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — HarnessQueue not compiled in this build")
}

func testHarnessQueueCtxCancelBeforeChat(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — HarnessQueue not compiled in this build")
}

func testHarnessQueueReset(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — HarnessQueue not compiled in this build")
}

func testParseScenarioRequestValidText(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestValidToolCalls(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestUnknownType(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestEmptyResponses(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestToolCallsMissingID(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestToolCallsMissingName(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testParseScenarioRequestToolCallsEmptyList(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — parseScenarioRequest not compiled in this build")
}

func testHandleTestScenarioNonPOSTReturns405(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testHandleTestScenarioInvalidJSONReturns400(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testHandleTestScenarioEmptyResponsesReturns400(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testHandleTestScenarioUnknownTypeReturns400(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testHandleTestScenarioValidScenarioReturns200(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testHandleTestScenarioQueueDequeuesInOrder(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — HarnessQueue not compiled in this build")
}

func testRegisterTestHarnessDevModeBypassReturns503(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}

func testRegisterTestHarnessDevModeBypassOffPassesThrough(t *testing.T) {
	t.Helper()
	t.Skip("test_harness build tag required — handleTestScenario not compiled in this build")
}
