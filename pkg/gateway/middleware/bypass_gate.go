//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package middleware

import (
	"net/http"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
)

// RequireNotBypass gates admin-only user-management and security-setting
// endpoints (FR-019 / MAJ-006). When gateway.dev_mode_bypass is true every
// request is auth'd as admin, so these endpoints would otherwise be
// anonymously reachable — a catastrophic elevation. Returning 503 disables
// the surface entirely in that mode.
//
// The config is read from the request context (written by
// configSnapshotMiddleware). If the snapshot is missing we fail closed with
// 503 rather than silently passing through: a handler chain that skipped the
// snapshot middleware cannot be trusted to have applied auth either.
func RequireNotBypass(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, _ := r.Context().Value(ctxkey.ConfigContextKey{}).(*config.Config)
		if cfg == nil || cfg.Gateway.DevModeBypass {
			writeJSONErr(w, http.StatusServiceUnavailable, "user management disabled in dev-mode-bypass")
			return
		}
		next(w, r)
	}
}
