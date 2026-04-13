package tools

import (
	"context"
	"fmt"
)

// AgentRegistryReader is a minimal interface for looking up agents by ID.
// It is satisfied by *agent.AgentRegistry — using an interface here avoids
// an import cycle (tools → agent → tools).
type AgentRegistryReader interface {
	// GetAgentName returns the display name and a boolean indicating whether
	// the agent exists. Used by HandoffTool to validate agent_id and build
	// user-facing messages.
	GetAgentName(agentID string) (string, bool)
}

// HandoffTool transfers the active session to a specialist agent.
// After a successful handoff, subsequent messages on the same session key
// are routed to the target agent instead of the default one.
type HandoffTool struct {
	getRegistry func() AgentRegistryReader
	setActive   func(sessionKey, agentID string)
}

// NewHandoffTool creates a HandoffTool.
//
//   - getRegistry is called at Execute time (not construction time) so that hot
//     reloads are automatically reflected without rebuilding the tool.
//   - setActive updates the per-session agent override in the agent loop.
func NewHandoffTool(
	getRegistry func() AgentRegistryReader,
	setActive func(sessionKey, agentID string),
) *HandoffTool {
	return &HandoffTool{
		getRegistry: getRegistry,
		setActive:   setActive,
	}
}

func (t *HandoffTool) Name() string        { return "handoff" }
func (t *HandoffTool) Scope() ToolScope    { return ScopeGeneral }

func (t *HandoffTool) Description() string {
	return "Hand off the conversation to a specialist agent. The user's subsequent messages will go to the target agent."
}

func (t *HandoffTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "ID of the target agent (e.g. \"ray\", \"max\", \"ava\", \"jim\")",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Context or instructions to give the target agent about this conversation",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Optional message to show the user (e.g. \"Connecting you with Ray...\")",
			},
		},
		"required": []string{"agent_id", "context"},
	}
}

func (t *HandoffTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	agentID, ok := args["agent_id"].(string)
	if !ok || agentID == "" {
		return ErrorResult("agent_id is required")
	}
	contextMsg, _ := args["context"].(string)
	userMsg, _ := args["message"].(string)

	registry := t.getRegistry()
	agentName, exists := registry.GetAgentName(agentID)
	if !exists {
		return ErrorResult(fmt.Sprintf("agent %q not found — check the agent ID", agentID))
	}

	sessionKey := ToolSessionKey(ctx)
	if sessionKey == "" {
		return ErrorResult("handoff is not available in this context (no session key)")
	}

	t.setActive(sessionKey, agentID)

	forUI := userMsg
	if forUI == "" {
		forUI = fmt.Sprintf("Handing off to %s...", agentName)
	}

	_ = contextMsg // context is available to the LLM in its own message history

	return &ToolResult{
		ForUser:  forUI,
		ForLLM: fmt.Sprintf("Handoff complete. The user is now connected to %s (%s). Continue the conversation from their perspective.", agentName, agentID),
	}
}

// ReturnToDefaultTool clears the session-level agent override, returning
// routing to the normal binding cascade (and ultimately the default agent).
type ReturnToDefaultTool struct {
	setActive func(sessionKey, agentID string)
}

// NewReturnToDefaultTool creates a ReturnToDefaultTool.
func NewReturnToDefaultTool(setActive func(sessionKey, agentID string)) *ReturnToDefaultTool {
	return &ReturnToDefaultTool{setActive: setActive}
}

func (t *ReturnToDefaultTool) Name() string        { return "return_to_default" }
func (t *ReturnToDefaultTool) Scope() ToolScope    { return ScopeGeneral }

func (t *ReturnToDefaultTool) Description() string {
	return "Return the conversation to the default agent. Clears any active handoff override for this session."
}

func (t *ReturnToDefaultTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Optional summary of what was accomplished before returning",
			},
		},
		"required": []string{},
	}
}

func (t *ReturnToDefaultTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	sessionKey := ToolSessionKey(ctx)
	if sessionKey == "" {
		return ErrorResult("return_to_default is not available in this context (no session key)")
	}

	// Clear the override by setting agentID to "".
	t.setActive(sessionKey, "")

	summary, _ := args["summary"].(string)
	forLLM := "Returned to default agent."
	if summary != "" {
		forLLM = fmt.Sprintf("Returned to default agent. Summary: %s", summary)
	}

	return &ToolResult{
		ForUser:  "Returning to default agent.",
		ForLLM: forLLM,
	}
}
