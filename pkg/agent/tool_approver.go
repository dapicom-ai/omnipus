// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// ToolApprover is the interface the agent loop calls when an ask-policy tool is
// encountered, implementing the human-in-the-loop approval gate (FR-011, FR-082).
//
// The concrete implementation lives in pkg/gateway (approvalRegistryV2 + WSHandler)
// and is injected at boot via AgentLoop.SetToolApprover to avoid an import cycle.
//
// A nil ToolApprover means approvals are not wired (test mode or CLI). When nil,
// ask-policy tools are treated as allow — the gate is open but no WS event fires.

package agent

import "context"

// PolicyApprovalReq carries the fields needed to create and broadcast an approval.
type PolicyApprovalReq struct {
	ToolCallID    string
	ToolName      string
	Args          map[string]any
	AgentID       string
	SessionID     string
	TurnID        string
	RequiresAdmin bool
}

// PolicyApprover is implemented by the gateway to wire the central approval
// registry and WebSocket broadcast into the agent loop (FR-011, FR-082).
//
// This is distinct from the hooks.ToolApprover interface (which is for hook-based
// interactive approval). PolicyApprover governs the policy-level ask gate from
// FilterToolsByPolicy, using the in-process approvalRegistryV2.
//
// RequestApproval MUST:
//  1. Create a pending approval entry in the registry.
//  2. Emit a tool_approval_required WS frame (scoped to session owner, FR-073).
//  3. Block until the user approves/denies, the timeout fires, the queue is
//     saturated, or the gateway shuts down.
//  4. Return (true, "") on approve; (false, reason) otherwise.
//
// denialReason matches the Reason field from ApprovalOutcome:
// "user", "timeout", "saturated", "restart", "cancel", "batch_short_circuit".
type PolicyApprover interface {
	RequestApproval(ctx context.Context, req PolicyApprovalReq) (approved bool, denialReason string)
}

// nopPolicyApprover is returned by loadToolApprover when no PolicyApprover has
// been set. It approves every ask — safe fallback for unit tests and CLI mode
// where the gateway (and its WS layer) is not present.
type nopPolicyApprover struct{}

func (nopPolicyApprover) RequestApproval(_ context.Context, _ PolicyApprovalReq) (bool, string) {
	return true, ""
}
