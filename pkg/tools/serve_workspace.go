//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package tools — serve_workspace tool (.., US-4).
//
// serve_workspace allows an agent to register a subdirectory of its workspace
// for static-asset serving. The gateway mints a 32-byte random token and
// returns a URL of the form:
//
//	https://<host>/serve/<agent>/<token>/<initial-path>
//
// Authentication is required on every request to that URL
// (RequireSessionCookieOrBearer). The registration has a bounded lifetime
// clamped to [MinDurationSeconds, MaxDurationSeconds] from config.
//
// Per-agent cap: calling serve_workspace again on the same agent
// atomically invalidates the previous token and issues a new one.

package tools

import (
	"context"
	"fmt"
	"time"
)

// ServedSubdirsRegistry is the interface the serve_workspace tool uses to
// register a directory. It is satisfied by *agent.ServedSubdirs; the
// interface lives here to avoid an import cycle (tools → agent would cycle
// since agent imports tools).
type ServedSubdirsRegistry interface {
	// Register creates a new registration. Returns (token, deadline, error).
	Register(agentID, absDir string, duration time.Duration) (token string, deadline time.Time, err error)
	// ActiveForAgent returns (token, deadline, true) if agentID already has
	// an active registration.
	ActiveForAgent(agentID string) (token string, deadline time.Time, ok bool)
}

// ServeWorkspaceTool implements the serve_workspace tool (..).
type ServeWorkspaceTool struct {
	// workspace is the agent's absolute workspace directory.
	workspace string
	// registry is the process-wide registration map.
	registry ServedSubdirsRegistry
	// gatewayBaseURL is the scheme+host used to build the returned URL.
	// In the two-port iframe-preview topology this MUST be the preview
	// listener's base URL (e.g. "http://146.190.89.151:5001" or
	// "https://preview.omnipus.example.com") — NOT the main gateway origin.
	// The browser must reach a different origin from the SPA's origin so
	// served JS cannot read the SPA's localStorage (Threat Model T-01).
	// Provided by the agent instance at construction time.
	gatewayBaseURL string
	// agentID is the agent this tool instance belongs to.
	agentID string
	// minDuration is the minimum allowed registration lifetime.
	minDuration time.Duration
	// maxDuration is the maximum allowed registration lifetime.
	maxDuration time.Duration
}

// NewServeWorkspaceTool creates a serve_workspace tool for the given agent.
//
// - workspace: absolute path to the agent's workspace root.
// - agentID: the agent's ID (embedded in the URL).
// - gatewayBaseURL: e.g. "http://localhost:3000" (no trailing slash).
// - registry: the process-wide ServedSubdirs registry.
// - minDurationSec: minimum duration in seconds (default 60 when 0).
// - maxDurationSec: maximum duration in seconds (default 86400 when 0).
func NewServeWorkspaceTool(
	workspace string,
	agentID string,
	gatewayBaseURL string,
	registry ServedSubdirsRegistry,
	minDurationSec int32,
	maxDurationSec int32,
) *ServeWorkspaceTool {
	if minDurationSec <= 0 {
		minDurationSec = 60
	}
	if maxDurationSec <= 0 {
		maxDurationSec = 86400
	}
	return &ServeWorkspaceTool{
		workspace:      workspace,
		registry:       registry,
		gatewayBaseURL: gatewayBaseURL,
		agentID:        agentID,
		minDuration:    time.Duration(minDurationSec) * time.Second,
		maxDuration:    time.Duration(maxDurationSec) * time.Second,
	}
}

func (t *ServeWorkspaceTool) Name() string        { return "serve_workspace" }
func (t *ServeWorkspaceTool) Scope() ToolScope    { return ScopeGeneral }

func (t *ServeWorkspaceTool) Description() string {
	return "Serve a directory from the agent workspace as a static website. " +
		"Returns a URL valid for the requested duration. " +
		"Only one active registration is permitted per agent — calling again issues a new token."
}

func (t *ServeWorkspaceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the directory to serve, relative to the agent workspace.",
			},
			"duration_seconds": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("How long the URL should remain active (clamped to [%d, %d]).", int(t.minDuration.Seconds()), int(t.maxDuration.Seconds())),
			},
		},
		"required": []string{"path"},
	}
}

func (t *ServeWorkspaceTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	rawPath, _ := args["path"].(string)
	if rawPath == "" {
		return ErrorResult("path is required")
	}

	// Canonicalise and validate path against workspace (restrict=true, no allow-list).
	absDir, err := ValidateWorkspacePath(rawPath, t.workspace, true, nil)
	if err != nil {
		return ErrorResult(fmt.Sprintf("path rejected: %v", err))
	}

	// Parse requested duration; clamp to [min, max].
	var duration time.Duration
	switch v := args["duration_seconds"].(type) {
	case float64:
		duration = time.Duration(int64(v)) * time.Second
	case int:
		duration = time.Duration(v) * time.Second
	case int64:
		duration = time.Duration(v) * time.Second
	default:
		// Default to max when not provided.
		duration = t.maxDuration
	}
	if duration < t.minDuration {
		duration = t.minDuration
	}
	if duration > t.maxDuration {
		duration = t.maxDuration
	}

	agentID := t.agentID
	if agentID == "" {
		agentID = ToolAgentID(ctx)
	}

	// Register (atomically replaces any previous registration for this agent).
	token, deadline, err := t.registry.Register(agentID, absDir, duration)
	if err != nil {
		return ErrorResult(fmt.Sprintf("serve_workspace: registration failed: %v", err))
	}

	// Build the path AND absolute URL. The path is what the SPA mounts in
	// the iframe (relative to whatever preview origin it resolves at render
	// time — survives transcript replay against a moved gateway). The
	// absolute URL is preserved for replay safety on legacy clients that
	// only read `url` (FR-008 — neither field is deprecated).
	//
	// gatewayBaseURL MUST be the preview listener's base URL (different
	// origin from the SPA) per FR-008 / T-01.
	path := fmt.Sprintf("/serve/%s/%s/", agentID, token)
	url := t.gatewayBaseURL + path

	return NewToolResult(fmt.Sprintf(
		`{"path":%q,"url":%q,"expires_at":%q}`,
		path,
		url,
		deadline.UTC().Format(time.RFC3339),
	))
}
