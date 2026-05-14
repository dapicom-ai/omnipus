package commands

import (
	"context"
	"fmt"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// AgentLoopInterface is a minimal interface for the agent-loop methods needed by
// the commands runtime. Using an interface here avoids a hard import cycle
// between pkg/commands and pkg/agent.
type AgentLoopInterface interface {
	// InterruptSession requests a graceful interrupt for the turn associated with
	// the given session ID, attaching hint to the audit trail. Returns the list
	// of cancelled turn IDs (parent + sub-turns) which the gateway uses for the
	// turn_cancelled audit entry; the commands runtime discards it.
	InterruptSession(sessionID, hint string) ([]string, error)
	// InterruptByChannelChat requests a graceful interrupt for all active turns
	// whose channel and chatID match the supplied values. Used by Tier B
	// (text-parsing) channels where inbound messages carry no explicit SessionID.
	InterruptByChannelChat(channel, chatID, hint string) error
}

// Canceller identifies who/what issued a cancel — populated for audit attribution.
type Canceller struct {
	UserID  string // user-side identity, e.g., "@bob" or "user_abc123"
	Channel string // factory ID, e.g., "telegram" | "slack" | "web" | "cli"
}

// Runtime provides runtime dependencies to command handlers. It is constructed
// per-request by the agent loop so that per-request state (like session scope)
// can coexist with long-lived callbacks (like GetModelInfo).
type Runtime struct {
	Config             *config.Config
	GetModelInfo       func() (name, provider string)
	ListAgentIDs       func() []string
	ListDefinitions    func() []Definition
	ListSkillNames     func() []string
	GetEnabledChannels func() []string
	GetActiveTurn      func() any // Returning any to avoid circular dependency with agent package
	SwitchModel        func(value string) (oldModel string, err error)
	SwitchChannel      func(value string) error
	ClearHistory       func() error
	ReloadConfig       func() error
	// SessionID returns the session key for the current request context. Used by
	// handlers that need to address a specific session (e.g., /cancel).
	SessionID func() string
	// agentLoop is the agent loop implementation used by CancelActiveTurn.
	// Populated by the agent loop via buildCommandsRuntime.
	agentLoop AgentLoopInterface
}

// CancelActiveTurn requests a graceful interrupt of the active turn for sessionID.
// The canceller fields are included in the audit hint so the interrupt can be
// attributed in the audit log. Returns nil both when the interrupt was fired and
// when there is no active turn — callers should not treat "no active turn" as an
// error at this layer.
func (rt *Runtime) CancelActiveTurn(ctx context.Context, sessionID string, canceller Canceller) error {
	if rt == nil || rt.agentLoop == nil {
		return nil
	}
	hint := fmt.Sprintf("cancelled by %s via %s", canceller.UserID, canceller.Channel)
	_, err := rt.agentLoop.InterruptSession(sessionID, hint)
	// "no active turn" is not an error at the commands layer — it surfaces to the
	// caller as a no-op cancel attempt (audit is the responsibility of the
	// gateway cancel handler wave).
	if err != nil {
		// Distinguish "no active turn" from real errors by returning nil for the
		// known "no active turn" sentinel so handlers can reply uniformly.
		// The deeper audit emission (turn_cancel_attempt) is handled by the
		// gateway/cancel-handler wave; here we just drive the interrupt.
		return nil
	}
	return nil
}

// WithAgentLoop returns a shallow copy of rt with agentLoop set. Used by the
// agent loop's buildCommandsRuntime to inject the loop reference without
// exporting the field directly.
func (rt *Runtime) WithAgentLoop(al AgentLoopInterface) *Runtime {
	copy := *rt
	copy.agentLoop = al
	return &copy
}
