package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// ErrNoActiveTurn is returned by CancelActiveTurn when InterruptSession reports
// that no turn is currently running for the given session. Callers should reply
// with an informational "Nothing to cancel" message rather than treating it as
// a failure.
var ErrNoActiveTurn = errors.New("no active turn")

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
// attributed in the audit log.
//
// Return values:
//   - nil           — interrupt was successfully fired.
//   - ErrNoActiveTurn — the agent loop reported no running turn (informational).
//   - other error   — a real failure (e.g., fsync, lock failure) that the caller
//     must surface to the user as a failure response.
func (rt *Runtime) CancelActiveTurn(ctx context.Context, sessionID string, canceller Canceller) error {
	if rt == nil || rt.agentLoop == nil {
		// No agent loop wired — treat as "nothing to cancel" so the
		// nil-runtime path still acknowledges the request.
		return ErrNoActiveTurn
	}
	hint := fmt.Sprintf("cancelled by %s via %s", canceller.UserID, canceller.Channel)
	_, err := rt.agentLoop.InterruptSession(sessionID, hint)
	if err == nil {
		return nil
	}
	// Distinguish "no active turn" from real errors. The agent loop may not
	// yet expose a typed sentinel (depends on whether the loop package has been
	// migrated). Until it does, fall back to string inspection.
	// Switch to errors.Is once the loop exports a typed ErrNoActiveTurn.
	if strings.Contains(err.Error(), "no active turn") {
		return ErrNoActiveTurn
	}
	return fmt.Errorf("cancel: %w", err)
}

// WithAgentLoop returns a shallow copy of rt with agentLoop set. Used by the
// agent loop's buildCommandsRuntime to inject the loop reference without
// exporting the field directly.
func (rt *Runtime) WithAgentLoop(al AgentLoopInterface) *Runtime {
	copy := *rt
	copy.agentLoop = al
	return &copy
}
