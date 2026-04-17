// Package testutil provides shared test infrastructure for all Plan-3 PRs.
// It is intentionally not a _test.go file so it can be imported from any
// _test.go in the repo without import cycles.
package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// ErrNoMoreResponses signals the scenario has run out of scripted responses.
var ErrNoMoreResponses = errors.New("scenario provider: no more scripted responses")

// scenarioStep holds one scripted Chat turn.
type scenarioStep struct {
	resp *providers.LLMResponse
	err  error
}

// ScenarioProvider returns pre-scripted responses in order. Thread-safe.
// Each builder method appends one step; Chat consumes steps sequentially.
//
// Example:
//
//	p := NewScenario().
//	    WithText("Hello!").
//	    WithToolCall("bash", `{"cmd":"ls"}`).
//	    WithText("Done.")
type ScenarioProvider struct {
	mu        sync.Mutex
	steps     []scenarioStep
	idx       int
	callCount int
	modelName string
}

// NewScenario returns an empty ScenarioProvider. Default model name is "scripted-model".
func NewScenario() *ScenarioProvider {
	return &ScenarioProvider{modelName: "scripted-model"}
}

// WithText appends a plain assistant text response (no tool calls).
func (s *ScenarioProvider) WithText(content string) *ScenarioProvider {
	s.steps = append(s.steps, scenarioStep{
		resp: &providers.LLMResponse{
			Content:   content,
			ToolCalls: []providers.ToolCall{},
		},
	})
	return s
}

// WithToolCall appends a response that invokes a single tool with the given JSON args.
func (s *ScenarioProvider) WithToolCall(name, argsJSON string) *ScenarioProvider {
	fc := providers.FunctionCall{
		Name:      name,
		Arguments: argsJSON,
	}
	return s.WithToolCalls([]providers.ToolCall{
		{
			ID:       name + "-0",
			Function: &fc,
		},
	})
}

// WithToolCalls appends a response with multiple parallel tool calls in a single turn.
func (s *ScenarioProvider) WithToolCalls(calls []providers.ToolCall) *ScenarioProvider {
	s.steps = append(s.steps, scenarioStep{
		resp: &providers.LLMResponse{
			Content:   "",
			ToolCalls: calls,
		},
	})
	return s
}

// WithError appends a step that returns err on the next Chat call.
func (s *ScenarioProvider) WithError(err error) *ScenarioProvider {
	s.steps = append(s.steps, scenarioStep{err: err})
	return s
}

// WithModelName sets the provider's model name (default "scripted-model").
func (s *ScenarioProvider) WithModelName(name string) *ScenarioProvider {
	s.modelName = name
	return s
}

// Chat pops the next scripted step. Returns ErrNoMoreResponses once exhausted.
// Implements providers.LLMProvider.
func (s *ScenarioProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	s.mu.Lock()
	if s.idx >= len(s.steps) {
		s.mu.Unlock()
		return nil, ErrNoMoreResponses
	}
	step := s.steps[s.idx]
	s.idx++
	s.callCount++
	s.mu.Unlock()

	return step.resp, step.err
}

// GetDefaultModel returns the configured model name.
// Implements providers.LLMProvider.
func (s *ScenarioProvider) GetDefaultModel() string {
	return s.modelName
}

// CallCount returns how many times Chat was invoked. Safe to read concurrently.
func (s *ScenarioProvider) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

// Remaining returns how many steps haven't been consumed yet.
func (s *ScenarioProvider) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.steps) - s.idx
}

// Reset clears consumption state; the scripted steps are preserved.
// Useful between sub-tests that share the same ScenarioProvider.
func (s *ScenarioProvider) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx = 0
	s.callCount = 0
}
