//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// WebSocket events for the Central Tool Registry redesign (A3 lane).
//
// Emits two event types:
//
//  1. tool_approval_required (FR-011, FR-082)
//     Sent to all connected WS clients when an ask-policy tool call is paused.
//     Uses expires_in_ms (not expires_at) per OBS-004.
//
//  2. session_state (FR-052, FR-073, FR-081)
//     One-shot per WS connection on every reconnect.
//     Scoped to the authenticated user: admins see all sessions; non-admins see own only.

package gateway

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// toolApprovalRequiredPayload is the wire format for tool_approval_required (FR-082).
// Uses expires_in_ms (relative) not expires_at (absolute) to avoid clock-skew issues (OBS-004).
type toolApprovalRequiredPayload struct {
	Type        string         `json:"type"`
	ApprovalID  string         `json:"approval_id"`
	ToolCallID  string         `json:"tool_call_id"`
	ToolName    string         `json:"tool_name"`
	Args        map[string]any `json:"args"`
	AgentID     string         `json:"agent_id"`
	SessionID   string         `json:"session_id"`
	TurnID      string         `json:"turn_id"`
	ExpiresInMs int64          `json:"expires_in_ms"` // OBS-004: relative, not absolute
}

// sessionStatePendingApproval is one entry in the session_state payload.
type sessionStatePendingApproval struct {
	ApprovalID  string `json:"approval_id"`
	SessionID   string `json:"session_id"`
	ToolName    string `json:"tool_name"`
	AgentID     string `json:"agent_id"`
	ExpiresInMs int64  `json:"expires_in_ms"`
}

// sessionStatePayload is the full session_state frame (FR-081 schema is binding).
type sessionStatePayload struct {
	Type             string                        `json:"type"`
	UserID           string                        `json:"user_id"`
	PendingApprovals []sessionStatePendingApproval `json:"pending_approvals"`
	EmittedAt        string                        `json:"emitted_at"` // RFC3339
}

// broadcastToolApprovalRequired sends a tool_approval_required WS frame to all
// connected WebSocket clients.  Called by the agent loop (via a callback registered
// at startup) when an ask-policy tool call is paused (FR-011, FR-082).
//
// The frame is best-effort: clients that are disconnected or have a full send buffer
// will miss the frame and must rely on the next session_state reset on reconnect.
func (h *WSHandler) broadcastToolApprovalRequired(entry *approvalEntry) {
	if entry == nil {
		return
	}
	payload := toolApprovalRequiredPayload{
		Type:        "tool_approval_required",
		ApprovalID:  entry.ApprovalID,
		ToolCallID:  entry.ToolCallID,
		ToolName:    entry.ToolName,
		Args:        entry.Args,
		AgentID:     entry.AgentID,
		SessionID:   entry.SessionID,
		TurnID:      entry.TurnID,
		ExpiresInMs: entry.expiresInMs(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Error("ws: marshal tool_approval_required", "error", err)
		return
	}

	h.mu.Lock()
	conns := make([]*wsConn, 0, len(h.sessions))
	for _, wc := range h.sessions {
		conns = append(conns, wc)
	}
	h.mu.Unlock()

	for _, wc := range conns {
		select {
		case wc.sendCh <- raw:
		default:
			slog.Warn("ws: tool_approval_required dropped — send buffer full",
				"approval_id", entry.ApprovalID)
			wc.droppedFrames.Add(1)
		}
	}
}

// emitSessionState sends the session_state one-shot frame to a single WS connection
// immediately after authentication (FR-052, FR-073, FR-081).
//
// Scoping rules (FR-073):
//   - Admin role: sees pending approvals for ALL sessions.
//   - Non-admin: sees only approvals for their own sessions (matched by session.AgentID
//     is unreliable without per-session user tracking; until the session-ownership model
//     is wired by A1, non-admins see their own connection's associated session ID, which
//     may be "" on first connect).
//
// Note: When approvalRegV2 is nil (pre-registry harness), the payload has an empty
// pending_approvals array — the SPA receives a valid frame and clears any stale UI.
func (h *WSHandler) emitSessionState(wc *wsConn) {
	if wc == nil {
		return
	}

	var pendingApprovals []sessionStatePendingApproval

	if h.approvalRegV2 != nil {
		allPending := h.approvalRegV2.pendingApprovals()
		isAdmin := wc.role == config.UserRoleAdmin

		for _, e := range allPending {
			// Admin sees all; non-admin sees only approvals matching their own sessions.
			// Until session ownership is tracked at the WS layer, non-admins see nothing
			// (safe default; they will see their own once session ownership is wired).
			if !isAdmin {
				// FR-073: non-admin scoping — placeholder until A1 wires session ownership.
				// For now, non-admin clients receive an empty set rather than leaking
				// other users' approval data.  TODO: wire wc.userID → session ownership.
				continue
			}
			pendingApprovals = append(pendingApprovals, sessionStatePendingApproval{
				ApprovalID:  e.ApprovalID,
				SessionID:   e.SessionID,
				ToolName:    e.ToolName,
				AgentID:     e.AgentID,
				ExpiresInMs: e.expiresInMs(),
			})
		}
	}

	// Never send null — empty array per FR-081.
	if pendingApprovals == nil {
		pendingApprovals = []sessionStatePendingApproval{}
	}

	payload := sessionStatePayload{
		Type:             "session_state",
		UserID:           wc.userID,
		PendingApprovals: pendingApprovals,
		EmittedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Error("ws: marshal session_state", "error", err)
		return
	}

	select {
	case wc.sendCh <- raw:
		slog.Debug("ws: session_state emitted", "user_id", wc.userID, "pending", len(pendingApprovals))
	case <-wc.doneCh:
		// Connection closed before we could send — ignore.
	default:
		slog.Warn("ws: session_state dropped — send buffer full", "user_id", wc.userID)
		wc.droppedFrames.Add(1)
	}
}
