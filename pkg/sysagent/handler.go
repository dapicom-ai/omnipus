// Omnipus — System Agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sysagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// ConfirmationFunc is called by the handler before executing a destructive tool.
// It must return (true, nil) only when the user has clicked the UI confirmation button.
// Returns (false, nil) when the user canceled, or (false, err) on error.
type ConfirmationFunc func(ctx context.Context, toolName string, args map[string]any) (confirmed bool, err error)

// SystemToolHandler wraps the system tool registry with RBAC, rate limiting,
// and audit logging. It is the single entry point for all system tool invocations.
type SystemToolHandler struct {
	registry    *tools.ToolRegistry
	rateLimiter *SystemRateLimiter
	audit       *audit.Logger
	confirm     ConfirmationFunc
}

// HandlerConfig groups the dependencies for a SystemToolHandler.
type HandlerConfig struct {
	// Registry holds the 35 registered system tools.
	Registry *tools.ToolRegistry
	// Audit is the audit logger (SEC-15).
	Audit *audit.Logger
	// Confirm is called for destructive operations to obtain UI-level confirmation.
	// If nil, destructive ops are denied in non-single-user mode.
	Confirm ConfirmationFunc
}

// NewSystemToolHandler creates a handler with the given config.
func NewSystemToolHandler(cfg HandlerConfig) *SystemToolHandler {
	return &SystemToolHandler{
		registry:    cfg.Registry,
		rateLimiter: NewSystemRateLimiter(),
		audit:       cfg.Audit,
		confirm:     cfg.Confirm,
	}
}

// Handle executes a system tool call after RBAC, rate limit, confirmation, and
// audit checks. It is the authoritative execution path for all system.* tools.
//
// callerRole is the RBAC role of the calling device/session.
// deviceID identifies the calling device for audit purposes.
func (h *SystemToolHandler) Handle(
	ctx context.Context,
	callerRole PrincipalRole,
	deviceID string,
	toolName string,
	args map[string]any,
) *tools.ToolResult {
	start := time.Now()

	// 1. RBAC check (SEC-19).
	if err := CheckRBAC(callerRole, toolName); err != nil {
		slog.Warn("System tool RBAC denied",
			"tool", toolName,
			"caller_role", callerRole,
			"device_id", deviceID,
		)
		h.logAudit(toolName, deviceID, string(callerRole), args, "denied", start)
		if denied, ok := err.(*PermissionDeniedError); ok {
			return tools.ErrorResult(FriendlyDenialMessage(denied))
		}
		return tools.ErrorResult(err.Error())
	}

	// 2. Rate limit check.
	if err := h.rateLimiter.Check(toolName); err != nil {
		slog.Warn("System tool rate-limited", "tool", toolName)
		h.logAudit(toolName, deviceID, string(callerRole), args, "rate_limited", start)
		if rlErr, ok := err.(*RateLimitedError); ok {
			return tools.ErrorResult(fmt.Sprintf(
				"RATE_LIMITED: too many %s operations — please wait %.0f seconds before trying again.",
				rlErr.Category, rlErr.RetryAfterSeconds,
			))
		}
		return tools.ErrorResult(err.Error())
	}

	// 3. UI confirmation for destructive operations.
	if RequiresConfirmation(toolName) == ConfirmationUI {
		if h.confirm == nil {
			h.logAudit(toolName, deviceID, string(callerRole), args, "no_confirm_handler", start)
			return tools.ErrorResult(
				"CONFIRMATION_REQUIRED: this operation requires explicit confirmation but no " +
					"confirmation handler is configured (headless mode). " +
					"Operation denied.",
			)
		}
		confirmed, err := h.confirm(ctx, toolName, args)
		if err != nil {
			h.logAudit(toolName, deviceID, string(callerRole), args, "confirm_error", start)
			return tools.ErrorResult(fmt.Sprintf(
				"CONFIRMATION_ERROR: failed to obtain confirmation: %v", err))
		}
		if !confirmed {
			h.logAudit(toolName, deviceID, string(callerRole), args, "canceled", start)
			return tools.NewToolResult(
				`{"success":false,"status":"CONFIRMATION_REQUIRED","message":"Operation canceled by user."}`,
			)
		}
	}

	// 4. Execute via registry.
	result := h.registry.ExecuteWithContext(ctx, toolName, args, "", "", nil)

	// 5. Audit log after execution (SEC-15).
	decision := "allowed"
	if result != nil && result.IsError {
		decision = "error"
	}
	h.logAudit(toolName, deviceID, string(callerRole), args, decision, start)

	return result
}

// logAudit writes a system tool invocation to the audit trail (SEC-15).
func (h *SystemToolHandler) logAudit(
	toolName, deviceID, callerRole string,
	args map[string]any,
	decision string,
	start time.Time,
) {
	if h.audit == nil {
		return
	}
	entry := &audit.Entry{
		Timestamp:  start,
		Event:      audit.EventToolCall,
		AgentID:    SystemAgentID,
		Tool:       toolName,
		Decision:   decision,
		Parameters: args,
		Details: map[string]any{
			"device_id":   deviceID,
			"caller_role": callerRole,
		},
	}
	if err := h.audit.Log(entry); err != nil {
		slog.Error("System tool audit log failed",
			"tool", toolName,
			"device_id", deviceID,
			"error", err,
		)
	}
}
