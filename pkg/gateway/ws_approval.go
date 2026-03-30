// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dapicom-ai/omnipus/pkg/agent"
)

// wsApprovalRegistry tracks in-flight tool-approval requests awaiting browser decisions.
// Each entry maps a request UUID to a channel on which the approval decision will be sent.
// Entries are inserted by wsApprovalHook.ApproveTool and resolved (or expired) by the
// WSHandler readLoop when it processes "exec_approval_response" frames.
type wsApprovalRegistry struct {
	mu      sync.Mutex
	pending map[string]chan agent.ApprovalDecision
}

func newWSApprovalRegistry() *wsApprovalRegistry {
	return &wsApprovalRegistry{
		pending: make(map[string]chan agent.ApprovalDecision),
	}
}

// register creates a buffered decision channel for the given request ID and returns it.
// If a channel for this ID already exists (UUID collision or duplicate call), it logs a
// warning and returns the existing channel — the caller must still call unregister.
// The caller must call unregister when done (whether the decision arrived or not).
func (r *wsApprovalRegistry) register(id string) chan agent.ApprovalDecision {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.pending[id]; ok {
		slog.Warn("ws: approval registry: duplicate request ID", "id", id)
		return existing
	}
	ch := make(chan agent.ApprovalDecision, 1)
	r.pending[id] = ch
	return ch
}

// unregister removes the pending entry for the given request ID.
func (r *wsApprovalRegistry) unregister(id string) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

// resolve delivers a decision to the waiting ApproveTool call.
// Returns false if no pending request with that ID exists (e.g. it already timed out).
func (r *wsApprovalRegistry) resolve(id string, decision agent.ApprovalDecision) bool {
	r.mu.Lock()
	ch, ok := r.pending[id]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- decision:
		return true
	default:
		// Channel already has a value — possible duplicate response from the browser.
		slog.Warn("ws: approval registry: channel already has value — possible duplicate response", "id", id)
		return false
	}
}

// wsApprovalHook implements agent.ToolApprover for a single WebSocket connection.
// When ApproveTool is called by the agent loop, it:
//  1. Generates a unique request ID.
//  2. Registers a pending channel in the registry.
//  3. Sends an "exec_approval_request" frame to the browser via the connection.
//  4. Blocks until the context is cancelled, the registry resolves the decision, or
//     the approval timeout fires.
//
// The WSHandler readLoop calls registry.resolve when it receives an
// "exec_approval_response" frame from the browser.
type wsApprovalHook struct {
	conn     *wsConn
	registry *wsApprovalRegistry
	// timeout is the per-request approval deadline. Defaults to wsApprovalTimeout.
	timeout time.Duration
}

const wsApprovalTimeout = 90 * time.Second

// ApproveTool sends the tool name and arguments to the connected browser and waits
// for a human decision. It denies execution on timeout or context cancellation.
func (h *wsApprovalHook) ApproveTool(ctx context.Context, req *agent.ToolApprovalRequest) (agent.ApprovalDecision, error) {
	if h == nil || h.conn == nil || h.registry == nil {
		// No active WebSocket — deny by default rather than approving blindly.
		return agent.Deny("no active WebSocket connection for interactive approval"), nil
	}

	id := uuid.New().String()
	ch := h.registry.register(id)
	defer h.registry.unregister(id)

	// Build the params map for the frame. ToolApprovalRequest.Arguments is map[string]any.
	frameParams := make(map[string]any, len(req.Arguments))
	for k, v := range req.Arguments {
		frameParams[k] = v
	}

	// Send the approval-request frame to the browser.
	sendConnFrame(h.conn, wsServerFrame{
		Type:    "exec_approval_request",
		ID:      id,
		Tool:    req.Tool,
		Params:  frameParams,
		Message: fmt.Sprintf("Agent wants to call tool %q. Allow?", req.Tool),
	})

	slog.Info("ws: sent exec_approval_request to browser", "id", id, "tool", req.Tool)

	timeout := h.timeout
	if timeout <= 0 {
		timeout = wsApprovalTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case decision := <-ch:
		slog.Info("ws: exec_approval_response received", "id", id, "verdict", decision.Verdict)
		return decision, nil
	case <-timer.C:
		slog.Warn("ws: exec_approval_request timed out — denying tool execution", "id", id, "tool", req.Tool)
		// Inform the browser the request expired.
		sendConnFrame(h.conn, wsServerFrame{
			Type:    "exec_approval_expired",
			ID:      id,
			Message: fmt.Sprintf("Approval request for %q timed out after %s — tool execution denied.", req.Tool, timeout),
		})
		return agent.Deny("approval timed out"), nil
	case <-h.conn.doneCh:
		slog.Warn("ws: connection closed while waiting for approval — denying tool execution", "id", id, "tool", req.Tool)
		return agent.Deny("WebSocket connection closed during approval"), nil
	case <-ctx.Done():
		slog.Warn("ws: context cancelled while waiting for approval — denying tool execution", "id", id, "tool", req.Tool)
		return agent.Deny("context cancelled"), ctx.Err()
	}
}
