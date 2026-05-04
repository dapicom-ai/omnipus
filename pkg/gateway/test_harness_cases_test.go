//go:build !cgo && test_harness

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

// test_harness build: real implementations of test helper functions that
// exercise HarnessQueue and parseScenarioRequest directly.
// The non-test_harness build provides skip-stubs in
// test_harness_cases_stub_test.go so the test binary compiles in all modes.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
)

// resetProcessHarnessQueueForTest drains the singleton queue between tests so
// that test ordering does not affect outcomes.
func resetProcessHarnessQueueForTest() {
	processHarnessQueue.Reset()
}

func testHarnessQueueFIFOOrder(t *testing.T) {
	t.Helper()
	q := &HarnessQueue{}

	// Enqueue three text responses directly.
	steps, err := parseScenarioRequest(harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{Type: "text", Content: "first"},
			{Type: "text", Content: "second"},
			{Type: "text", Content: "third"},
		},
	})
	require.NoError(t, err)
	q.Enqueue(steps)
	require.Equal(t, 3, q.Len(), "queue must have 3 steps after enqueue")

	ctx := context.Background()
	for i, want := range []string{"first", "second", "third"} {
		resp, chatErr := q.Chat(ctx, nil, nil, "", nil)
		require.NoError(t, chatErr, "step %d must not error", i+1)
		require.NotNil(t, resp, "step %d response must not be nil", i+1)
		assert.Equal(t, want, resp.Content, "step %d content mismatch", i+1)
	}
	assert.Equal(t, 0, q.Len(), "queue must be empty after consuming all steps")
}

func testHarnessQueueEmptyReturnsErrQueueEmpty(t *testing.T) {
	t.Helper()
	q := &HarnessQueue{}
	_, err := q.Chat(context.Background(), nil, nil, "", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrQueueEmpty), "expected ErrQueueEmpty, got %v", err)
}

func testHarnessQueueCtxCancelBeforeChat(t *testing.T) {
	t.Helper()
	q := &HarnessQueue{}

	// Enqueue one step so it's not an empty-queue error.
	steps, err := parseScenarioRequest(harnessScenarioRequest{
		Responses: []harnessResponseEntry{{Type: "text", Content: "unreachable"}},
	})
	require.NoError(t, err)
	q.Enqueue(steps)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Chat

	_, chatErr := q.Chat(ctx, nil, nil, "", nil)
	require.Error(t, chatErr)
	assert.ErrorIs(t, chatErr, context.Canceled,
		"cancelled context must return context.Canceled, not consume the step")
	assert.Equal(t, 1, q.Len(), "step must not be consumed when context is already cancelled")
}

func testHarnessQueueReset(t *testing.T) {
	t.Helper()
	q := &HarnessQueue{}
	steps, err := parseScenarioRequest(harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{Type: "text", Content: "a"},
			{Type: "text", Content: "b"},
		},
	})
	require.NoError(t, err)
	q.Enqueue(steps)
	require.Equal(t, 2, q.Len())
	q.Reset()
	assert.Equal(t, 0, q.Len(), "Reset must drain the queue")
}

func testParseScenarioRequestValidText(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{Type: "text", Content: "hello"},
		},
	}
	steps, err := parseScenarioRequest(req)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "hello", steps[0].resp.Content)
	assert.Empty(t, steps[0].resp.ToolCalls, "text response must have no tool calls")
}

func testParseScenarioRequestValidToolCalls(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{
				Type: "tool_calls",
				ToolCalls: []harnessToolCallEntry{
					{ID: "tc_1", Name: "fs_read_file", Arguments: map[string]any{"path": "README.md"}},
				},
			},
		},
	}
	steps, err := parseScenarioRequest(req)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Len(t, steps[0].resp.ToolCalls, 1)
	tc := steps[0].resp.ToolCalls[0]
	assert.Equal(t, "tc_1", tc.ID)
	require.NotNil(t, tc.Function)
	assert.Equal(t, "fs_read_file", tc.Function.Name)
	// Arguments must be valid JSON containing the path key.
	var args map[string]any
	require.NoError(t, json.Unmarshal([]byte(tc.Function.Arguments), &args))
	assert.Equal(t, "README.md", args["path"])
}

func testParseScenarioRequestUnknownType(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{Type: "invalid_type"},
		},
	}
	_, err := parseScenarioRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
}

func testParseScenarioRequestEmptyResponses(t *testing.T) {
	t.Helper()
	_, err := parseScenarioRequest(harnessScenarioRequest{Responses: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty")
}

func testParseScenarioRequestToolCallsMissingID(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{
				Type: "tool_calls",
				ToolCalls: []harnessToolCallEntry{
					{ID: "", Name: "some_tool", Arguments: map[string]any{}},
				},
			},
		},
	}
	_, err := parseScenarioRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")
}

func testParseScenarioRequestToolCallsMissingName(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{
				Type: "tool_calls",
				ToolCalls: []harnessToolCallEntry{
					{ID: "tc_1", Name: "", Arguments: map[string]any{}},
				},
			},
		},
	}
	_, err := parseScenarioRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func testParseScenarioRequestToolCallsEmptyList(t *testing.T) {
	t.Helper()
	req := harnessScenarioRequest{
		Responses: []harnessResponseEntry{
			{Type: "tool_calls", ToolCalls: []harnessToolCallEntry{}},
		},
	}
	_, err := parseScenarioRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one")
}

func testHandleTestScenarioNonPOSTReturns405(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/_test/scenario", nil)
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func testHandleTestScenarioInvalidJSONReturns400(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario",
		bytes.NewBufferString("not-json{{{"))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func testHandleTestScenarioEmptyResponsesReturns400(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario",
		bytes.NewBufferString(`{"responses":[]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func testHandleTestScenarioUnknownTypeReturns400(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario",
		bytes.NewBufferString(`{"responses":[{"type":"bogus"}]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func testHandleTestScenarioValidScenarioReturns200(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	resetProcessHarnessQueueForTest()

	body := `{"responses":[
		{"type":"text","content":"step one"},
		{"type":"text","content":"step two"}
	]}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario",
		bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["queued"], "queued count must match number of responses")

	resetProcessHarnessQueueForTest()
}

func testHandleTestScenarioQueueDequeuesInOrder(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	resetProcessHarnessQueueForTest()

	// Enqueue two text responses via the HTTP handler.
	body := `{"responses":[
		{"type":"text","content":"alpha"},
		{"type":"text","content":"beta"}
	]}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario",
		bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.handleTestScenario(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 2, processHarnessQueue.Len())

	// Consume via Chat to verify FIFO ordering.
	ctx := context.Background()
	first, err := processHarnessQueue.Chat(ctx, nil, nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "alpha", first.Content)

	second, err := processHarnessQueue.Chat(ctx, nil, nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, "beta", second.Content)

	// Queue exhausted — next Chat must return ErrQueueEmpty.
	_, err = processHarnessQueue.Chat(ctx, nil, nil, "", nil)
	assert.ErrorIs(t, err, ErrQueueEmpty, "exhausted queue must return ErrQueueEmpty")

	resetProcessHarnessQueueForTest()
}

func testRegisterTestHarnessDevModeBypassReturns503(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)

	// Build the same chain as registerTestHarness:
	//   withAuth → RequireNotBypass → handleTestScenario
	// We test RequireNotBypass in isolation (skipping withAuth) because
	// withAuth calls checkBearerAuth which requires OMNIPUS_BEARER_TOKEN.
	chain := middleware.RequireNotBypass(api.handleTestScenario)

	body := `{"responses":[{"type":"text","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	// Inject bypass=true into context (mimics configSnapshotMiddleware).
	cfg := &config.Config{}
	cfg.Gateway.DevModeBypass = true
	ctx := context.WithValue(r.Context(), ctxkey.ConfigContextKey{}, cfg)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	chain.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"RequireNotBypass must return 503 when DevModeBypass=true")
}

func testRegisterTestHarnessDevModeBypassOffPassesThrough(t *testing.T) {
	t.Helper()
	api := newTestRestAPIWithHome(t)
	resetProcessHarnessQueueForTest()

	chain := middleware.RequireNotBypass(api.handleTestScenario)

	body := `{"responses":[{"type":"text","content":"hello"}]}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/_test/scenario", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	// Inject bypass=false.
	cfg := &config.Config{}
	cfg.Gateway.DevModeBypass = false
	ctx := context.WithValue(r.Context(), ctxkey.ConfigContextKey{}, cfg)
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	chain.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code, "bypass=false must reach the handler")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["queued"], "queued must equal 1")

	resetProcessHarnessQueueForTest()
}
