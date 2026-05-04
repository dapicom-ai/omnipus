//go:build !cgo && test_harness

// Package gateway — HTTP scenario-injection harness (test_harness build only).
//
// # Purpose
//
// This file provides a build-tag-gated HTTP endpoint that injects deterministic
// LLM response sequences into a live gateway process. It enables integration
// and E2E tests to drive "agent decides to call a tool" assertions without
// depending on real LLM stochasticity.
//
// # Injection mechanism
//
// The harness installs a process-level HarnessQueue as the boot-time
// testProviderOverride (via the same atomic-pointer hook already used by
// pkg/agent/testutil/gateway_harness.go). The HarnessQueue implements
// providers.LLMProvider. Its Chat() method dequeues the next scripted response
// from an in-memory FIFO; when the FIFO is empty it returns ErrQueueEmpty,
// which surfaces to the agent loop as a provider error (test assertions should
// never leave the queue empty mid-scenario).
//
// The POST /api/v1/_test/scenario HTTP endpoint appends a full scenario
// (a slice of scripted responses) to the process-level queue. The next Chat()
// calls in the agent loop consume the responses in order.
//
// This design was chosen over a BeforeLLMCall hook because it requires zero
// changes to pkg/agent/loop.go (smallest surgical footprint) and reuses the
// existing boot-override mechanism.
//
// # Security
//
// The endpoint is ONLY compiled when the binary is built with -tags test_harness.
// In release builds, test_harness_disabled.go provides a no-op registerTestHarness
// so the mux-registration call site compiles without conditional compilation.
//
// Even in test_harness builds the endpoint is defended with RequireNotBypass:
// a gateway running with dev_mode_bypass=true (the common test shortcut) CANNOT
// reach this endpoint. Tests that exercise it must run with real auth or flip
// bypass off for the duration.
//
// # Scenario JSON schema
//
//	{
//	  "responses": [
//	    {"type": "text", "content": "Sure, let me read that file."},
//	    {
//	      "type": "tool_calls",
//	      "tool_calls": [
//	        {
//	          "id":        "tc_1",
//	          "name":      "fs_read_file",
//	          "arguments": {"path": "README.md"}
//	        }
//	      ]
//	    },
//	    {"type": "text", "content": "The file says 'hello'."}
//	  ]
//	}
//
// Field descriptions:
//
//   - responses      — ordered list of scripted LLM turns. Required.
//   - type           — "text" | "tool_calls". Required per entry.
//   - content        — assistant text (for type "text"). Omit or empty for tool_calls entries.
//   - tool_calls     — list of tool-call descriptors (for type "tool_calls"). Required when
//     type is "tool_calls". Each entry:
//   - id             — tool-call ID (string). Required.
//   - name           — tool name (string). Required.
//   - arguments      — arbitrary JSON object passed as tool arguments. Required (may be {}).
//
// # Wire-up
//
// registerTestHarness(cm) is called from registerAdditionalEndpoints in rest.go.
// In the test_harness build, it installs the HarnessQueue as the boot provider
// and registers POST /api/v1/_test/scenario on cm.
// In non-test_harness builds, registerTestHarness is a no-op stub in
// test_harness_disabled.go — the call site in rest.go compiles in all builds.

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
	"github.com/dapicom-ai/omnipus/pkg/providers"
	protocoltypes "github.com/dapicom-ai/omnipus/pkg/providers/protocoltypes"
)

// ErrQueueEmpty is returned by HarnessQueue.Chat when no scripted responses
// remain. Tests must not leave the queue empty when an LLM call is expected.
var ErrQueueEmpty = errors.New("test harness: scenario queue is empty")

// scenarioStep is one scripted LLM response stored in the queue.
type scenarioStep struct {
	resp *providers.LLMResponse
}

// HarnessQueue is a process-level FIFO LLM provider that returns scripted
// responses in the order they were enqueued. Thread-safe.
type HarnessQueue struct {
	mu    sync.Mutex
	steps []scenarioStep
}

// processHarnessQueue is the singleton installed at gateway boot.
// Accessed only via the atomic pointer in testhook.go.
var processHarnessQueue = &HarnessQueue{}

// Chat dequeues and returns the next scripted response.
// Returns ErrQueueEmpty when no responses remain.
// Implements providers.LLMProvider.
func (q *HarnessQueue) Chat(
	ctx context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	q.mu.Lock()
	if len(q.steps) == 0 {
		q.mu.Unlock()
		return nil, ErrQueueEmpty
	}
	step := q.steps[0]
	q.steps = q.steps[1:]
	q.mu.Unlock()

	return step.resp, nil
}

// GetDefaultModel returns a fixed name identifying the harness.
// Implements providers.LLMProvider.
func (q *HarnessQueue) GetDefaultModel() string {
	return "test-harness"
}

// Enqueue appends a slice of scripted responses to the FIFO in order.
func (q *HarnessQueue) Enqueue(steps []scenarioStep) {
	q.mu.Lock()
	q.steps = append(q.steps, steps...)
	q.mu.Unlock()
}

// Len returns the number of responses remaining in the queue.
func (q *HarnessQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steps)
}

// Reset drains the queue. Useful between tests in the same process.
func (q *HarnessQueue) Reset() {
	q.mu.Lock()
	q.steps = q.steps[:0]
	q.mu.Unlock()
}

// --- HTTP endpoint types ---

// harnessScenarioRequest is the JSON body accepted by POST /api/v1/_test/scenario.
type harnessScenarioRequest struct {
	Responses []harnessResponseEntry `json:"responses"`
}

// harnessResponseEntry is one scripted LLM turn in the scenario body.
type harnessResponseEntry struct {
	// Type is "text" or "tool_calls". Required.
	Type string `json:"type"`
	// Content is the assistant text for type="text". May be empty.
	Content string `json:"content,omitempty"`
	// ToolCalls is the list of tool-call descriptors for type="tool_calls".
	ToolCalls []harnessToolCallEntry `json:"tool_calls,omitempty"`
}

// harnessToolCallEntry describes one tool call within a scripted LLM turn.
type harnessToolCallEntry struct {
	// ID is the tool-call ID string.
	ID string `json:"id"`
	// Name is the tool name.
	Name string `json:"name"`
	// Arguments is the tool arguments as a JSON object.
	Arguments map[string]any `json:"arguments"`
}

// parseScenarioRequest converts the JSON body into a slice of scenarioSteps.
func parseScenarioRequest(body harnessScenarioRequest) ([]scenarioStep, error) {
	if len(body.Responses) == 0 {
		return nil, fmt.Errorf("responses must be a non-empty array")
	}
	steps := make([]scenarioStep, 0, len(body.Responses))
	for i, entry := range body.Responses {
		switch entry.Type {
		case "text":
			steps = append(steps, scenarioStep{
				resp: &providers.LLMResponse{
					Content:   entry.Content,
					ToolCalls: []protocoltypes.ToolCall{},
				},
			})
		case "tool_calls":
			if len(entry.ToolCalls) == 0 {
				return nil, fmt.Errorf("responses[%d]: type=tool_calls requires at least one entry in tool_calls", i)
			}
			calls := make([]protocoltypes.ToolCall, 0, len(entry.ToolCalls))
			for j, tc := range entry.ToolCalls {
				if tc.ID == "" {
					return nil, fmt.Errorf("responses[%d].tool_calls[%d]: id is required", i, j)
				}
				if tc.Name == "" {
					return nil, fmt.Errorf("responses[%d].tool_calls[%d]: name is required", i, j)
				}
				argsJSON, err := json.Marshal(tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("responses[%d].tool_calls[%d]: marshal arguments: %w", i, j, err)
				}
				calls = append(calls, protocoltypes.ToolCall{
					ID: tc.ID,
					Function: &protocoltypes.FunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			steps = append(steps, scenarioStep{
				resp: &providers.LLMResponse{
					Content:   entry.Content,
					ToolCalls: calls,
				},
			})
		default:
			return nil, fmt.Errorf("responses[%d]: unknown type %q — must be \"text\" or \"tool_calls\"", i, entry.Type)
		}
	}
	return steps, nil
}

// handleTestScenario handles POST /api/v1/_test/scenario.
// Accepts a scenario body and enqueues the scripted responses so that
// subsequent agent-loop LLM calls consume them in order.
//
// Chain: withAuth → RequireNotBypass → handleTestScenario
// (RequireNotBypass blocks callers when dev_mode_bypass=true so that a
// deployment running without a real auth token cannot reach this endpoint
// even though it is a test-only path.)
func (a *restAPI) handleTestScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed — use POST")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	var body harnessScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	steps, err := parseScenarioRequest(body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	processHarnessQueue.Enqueue(steps)
	slog.Info("test_harness: scenario enqueued", "count", len(steps), "queue_len", processHarnessQueue.Len())

	jsonOK(w, map[string]any{"queued": len(steps)})
}

// registerTestHarness installs the HarnessQueue as the boot-time provider
// override and registers POST /api/v1/_test/scenario on cm.
// This is the test_harness build variant; the complementary no-op stub lives
// in test_harness_disabled.go.
func (a *restAPI) registerTestHarness(cm httpHandlerRegistrar) {
	// Install the singleton HarnessQueue as the provider override so all
	// subsequent agent-loop Chat() calls route through the queue.
	// This supersedes any previously installed override (e.g. from StartTestGateway).
	SetTestProviderOverride(func() providers.LLMProvider {
		return processHarnessQueue
	})

	// Register the endpoint with the RequireNotBypass gate.
	// withAuth verifies the bearer token; RequireNotBypass blocks dev_mode_bypass.
	cm.RegisterHTTPHandler(
		"/api/v1/_test/scenario",
		a.withAuth(
			middleware.RequireNotBypass(a.handleTestScenario),
		),
	)
	slog.Warn("test_harness: POST /api/v1/_test/scenario registered — DO NOT use in production")
}
