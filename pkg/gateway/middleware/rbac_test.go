//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
)

// sentinel handler that writes 200 so we know the next handler was called.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func requestWithRole(role config.UserRole) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if role != "" {
		ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, role)
		r = r.WithContext(ctx)
	}
	return r
}

// TestRequireAdmin_Anonymous verifies that requests with no role in context
// (simulating a request that bypassed the auth middleware) receive 401.
func TestRequireAdmin_Anonymous(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil) // no role in context
	w := httptest.NewRecorder()

	middleware.RequireAdmin(okHandler).ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "unauthorized", body["error"])
}

// TestRequireAdmin_UserRole verifies that requests from a user-role principal
// receive 403 with an "admin required" error body.
func TestRequireAdmin_UserRole(t *testing.T) {
	w := httptest.NewRecorder()

	middleware.RequireAdmin(okHandler).ServeHTTP(w, requestWithRole(config.UserRoleUser))

	assert.Equal(t, http.StatusForbidden, w.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "admin required", body["error"])
}

// TestRequireAdmin_AdminRole verifies that admin-role requests pass through to
// the wrapped handler unchanged.
func TestRequireAdmin_AdminRole(t *testing.T) {
	w := httptest.NewRecorder()

	middleware.RequireAdmin(okHandler).ServeHTTP(w, requestWithRole(config.UserRoleAdmin))

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRequireAdmin_MissingRoleInContext simulates the defense-in-depth case
// where a role key exists in context but holds a zero value (empty string),
// which should be treated identically to no role at all (→ 401).
func TestRequireAdmin_MissingRoleInContext(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// Explicitly store an empty string — not the zero-value absence.
	ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, config.UserRole(""))
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	middleware.RequireAdmin(okHandler).ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "unauthorized", body["error"])
}
