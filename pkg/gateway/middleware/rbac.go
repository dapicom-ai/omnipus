//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package middleware provides HTTP middleware for the Omnipus gateway.
package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
)

// RequireAdmin returns a middleware that enforces admin-only access.
//
// Response matrix:
//   - No role in context (auth middleware skipped or token unrecognized) → 401
//   - Authenticated user with role != admin → 403 + {"error":"admin required"}
//   - Authenticated admin → delegates to next handler unchanged
//
// This middleware must sit after withAuth, which is the layer that verifies the
// bearer token and writes the role into the request context via RoleContextKey.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := r.Context().Value(ctxkey.RoleContextKey{}).(config.UserRole)
		if !ok || role == "" {
			// No role in context means the auth middleware did not run or did
			// not recognize the token — treat as unauthenticated.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"}); err != nil {
				// Nothing useful to do after headers are sent; error is already
				// logged by the http.Server at the transport level.
				_ = err
			}
			return
		}
		if role != config.UserRoleAdmin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "admin required"}); err != nil {
				_ = err
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}
