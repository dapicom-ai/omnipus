//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

// Tests for the test-harness scenario endpoint and the HarnessQueue LLM provider.
//
// These tests exercise:
//   - parseScenarioRequest correctness and validation.
//   - HarnessQueue FIFO ordering, Reset, and Len.
//   - HarnessQueue.Chat returns ErrQueueEmpty on empty queue.
//   - HarnessQueue.Chat respects context cancellation.
//   - registerTestHarness no-op (non-test_harness) vs. real registration (test_harness).
//   - HTTP endpoint: POST /api/v1/_test/scenario returns 200 with queued count.
//   - HTTP endpoint: non-POST method returns 405.
//   - HTTP endpoint: dev_mode_bypass=true returns 503 (RequireNotBypass).
//   - HTTP endpoint: invalid JSON body returns 400.
//   - HTTP endpoint: invalid scenario shape (unknown type) returns 400.
//   - HTTP endpoint: empty responses array returns 400.
//   - HTTP endpoint: enqueued scenario dequeues in order.
//
// Each Test* function delegates to a helper defined either in
// test_harness_cases_test.go (test_harness build: real implementation) or
// test_harness_cases_stub_test.go (non-test_harness build: t.Skip stub).
// This split keeps the entry-point file clean and compiling in all builds.

import "testing"

// --- HarnessQueue unit tests ---

// TestHarnessQueue_FIFOOrder verifies responses are returned in insertion order.
func TestHarnessQueue_FIFOOrder(t *testing.T) {
	testHarnessQueueFIFOOrder(t)
}

// TestHarnessQueue_EmptyReturnsErrQueueEmpty verifies that Chat on an empty
// queue returns ErrQueueEmpty.
func TestHarnessQueue_EmptyReturnsErrQueueEmpty(t *testing.T) {
	testHarnessQueueEmptyReturnsErrQueueEmpty(t)
}

// TestHarnessQueue_CtxCancelBeforeChat verifies that a cancelled context causes
// Chat to return ctx.Err() without consuming a step.
func TestHarnessQueue_CtxCancelBeforeChat(t *testing.T) {
	testHarnessQueueCtxCancelBeforeChat(t)
}

// TestHarnessQueue_Reset verifies that Reset drains the queue.
func TestHarnessQueue_Reset(t *testing.T) {
	testHarnessQueueReset(t)
}

// --- parseScenarioRequest unit tests ---

// TestParseScenarioRequest_ValidText verifies a text-only scenario parses correctly.
func TestParseScenarioRequest_ValidText(t *testing.T) {
	testParseScenarioRequestValidText(t)
}

// TestParseScenarioRequest_ValidToolCalls verifies a tool_calls scenario parses correctly.
func TestParseScenarioRequest_ValidToolCalls(t *testing.T) {
	testParseScenarioRequestValidToolCalls(t)
}

// TestParseScenarioRequest_UnknownType verifies that an unknown type returns an error.
func TestParseScenarioRequest_UnknownType(t *testing.T) {
	testParseScenarioRequestUnknownType(t)
}

// TestParseScenarioRequest_EmptyResponses verifies that an empty responses array returns an error.
func TestParseScenarioRequest_EmptyResponses(t *testing.T) {
	testParseScenarioRequestEmptyResponses(t)
}

// TestParseScenarioRequest_ToolCallsMissingID verifies that tool_calls entry
// without an ID returns an error.
func TestParseScenarioRequest_ToolCallsMissingID(t *testing.T) {
	testParseScenarioRequestToolCallsMissingID(t)
}

// TestParseScenarioRequest_ToolCallsMissingName verifies that a tool_calls entry
// without a name returns an error.
func TestParseScenarioRequest_ToolCallsMissingName(t *testing.T) {
	testParseScenarioRequestToolCallsMissingName(t)
}

// TestParseScenarioRequest_ToolCallsEmptyList verifies that type=tool_calls with
// zero tool_calls entries returns an error.
func TestParseScenarioRequest_ToolCallsEmptyList(t *testing.T) {
	testParseScenarioRequestToolCallsEmptyList(t)
}

// --- HTTP endpoint tests ---

// TestHandleTestScenario_NonPOST_Returns405 verifies that non-POST methods return 405.
func TestHandleTestScenario_NonPOST_Returns405(t *testing.T) {
	testHandleTestScenarioNonPOSTReturns405(t)
}

// TestHandleTestScenario_InvalidJSON_Returns400 verifies that malformed JSON returns 400.
func TestHandleTestScenario_InvalidJSON_Returns400(t *testing.T) {
	testHandleTestScenarioInvalidJSONReturns400(t)
}

// TestHandleTestScenario_EmptyResponses_Returns400 verifies that an empty
// responses array returns 400.
func TestHandleTestScenario_EmptyResponses_Returns400(t *testing.T) {
	testHandleTestScenarioEmptyResponsesReturns400(t)
}

// TestHandleTestScenario_UnknownType_Returns400 verifies that an unknown
// response type returns 400.
func TestHandleTestScenario_UnknownType_Returns400(t *testing.T) {
	testHandleTestScenarioUnknownTypeReturns400(t)
}

// TestHandleTestScenario_ValidScenario_Returns200 verifies that a well-formed
// scenario body returns 200 with the correct queued count.
func TestHandleTestScenario_ValidScenario_Returns200(t *testing.T) {
	testHandleTestScenarioValidScenarioReturns200(t)
}

// TestHandleTestScenario_QueueDequeuesInOrder verifies that steps enqueued via
// the HTTP endpoint are consumed in order by HarnessQueue.Chat.
func TestHandleTestScenario_QueueDequeuesInOrder(t *testing.T) {
	testHandleTestScenarioQueueDequeuesInOrder(t)
}

// TestRegisterTestHarness_DevModeBypass_Returns503 verifies that RequireNotBypass
// returns 503 when dev_mode_bypass=true.
func TestRegisterTestHarness_DevModeBypass_Returns503(t *testing.T) {
	testRegisterTestHarnessDevModeBypassReturns503(t)
}

// TestRegisterTestHarness_DevModeBypassOff_PassesThrough verifies that
// RequireNotBypass allows the request when dev_mode_bypass=false.
func TestRegisterTestHarness_DevModeBypassOff_PassesThrough(t *testing.T) {
	testRegisterTestHarnessDevModeBypassOffPassesThrough(t)
}
