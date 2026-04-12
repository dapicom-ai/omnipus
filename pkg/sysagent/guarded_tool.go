// Omnipus — System Agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package sysagent

import (
	"context"

	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// GuardedTool wraps a system tool with the BRD-required guards that
// SystemToolHandler.Handle() enforces: RBAC, rate limiting, confirmation,
// and audit logging. The underlying tool is never executed without passing
// through Handle(). This is the authoritative execution path when system.*
// tools are registered on the main agent's ToolRegistry and invoked via the
// agent loop's ToolRegistry.ExecuteWithContext().
//
// GuardedTool satisfies the tools.Tool interface so it can be registered and
// called identically to raw tools — callers see no difference.
type GuardedTool struct {
	inner      tools.Tool
	handler    *SystemToolHandler
	callerRole PrincipalRole
	deviceID   string
}

// NewGuardedTool wraps inner with the handler's guard sequence (RBAC, rate
// limit, confirmation, audit). callerRole and deviceID are baked in at
// construction time; in open-source single-user mode use RoleSingleUser and an
// empty deviceID.
func NewGuardedTool(
	inner tools.Tool,
	handler *SystemToolHandler,
	callerRole PrincipalRole,
	deviceID string,
) *GuardedTool {
	return &GuardedTool{
		inner:      inner,
		handler:    handler,
		callerRole: callerRole,
		deviceID:   deviceID,
	}
}

// Name delegates to the inner tool — must return the same value so the agent
// loop can resolve the tool by name.
func (g *GuardedTool) Name() string { return g.inner.Name() }

// Description delegates to the inner tool.
func (g *GuardedTool) Description() string { return g.inner.Description() }

// Parameters delegates to the inner tool.
func (g *GuardedTool) Parameters() map[string]any { return g.inner.Parameters() }

// Scope delegates to the inner tool — the guard wrapper preserves the inner
// tool's scope so the ToolCompositor's scope gate fires correctly.
func (g *GuardedTool) Scope() tools.ToolScope { return g.inner.Scope() }

// Execute routes through SystemToolHandler.Handle which enforces:
//  1. RBAC check (CheckRBAC) — SEC-19
//  2. Rate limit check (SystemRateLimiter.Check)
//  3. Confirmation requirement (RequiresConfirmation) — UI button gate
//  4. Inner tool execution (registry.ExecuteWithContext)
//  5. Audit log entry (audit.Logger.Log) — SEC-15
func (g *GuardedTool) Execute(ctx context.Context, args map[string]any) *tools.ToolResult {
	return g.handler.Handle(ctx, g.callerRole, g.deviceID, g.inner.Name(), args)
}
