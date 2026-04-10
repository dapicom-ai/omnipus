//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wave 5 REST endpoint tests — SEC-01/02/03 sandbox status.
//
// These tests exercise:
//   - GET /api/v1/security/sandbox-status — returns 200 with valid JSON
//   - GET /api/v1/security/sandbox-status — response includes "backend" field
//   - Non-GET methods return 405

// TestHandleSandboxStatus_Returns200 verifies the endpoint returns 200 with
// a parseable JSON body in the default test configuration.
func TestHandleSandboxStatus_Returns200(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/security/sandbox-status", nil)
	api.HandleSandboxStatus(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "response must be valid JSON")
}

// TestHandleSandboxStatus_HasBackendField verifies the response always contains
// the "backend" field, which is guaranteed to be non-empty (at minimum "none").
func TestHandleSandboxStatus_HasBackendField(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/security/sandbox-status", nil)
	api.HandleSandboxStatus(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	backend, exists := body["backend"]
	assert.True(t, exists, `response must contain "backend" field`)
	backendStr, isString := backend.(string)
	assert.True(t, isString, `"backend" field must be a string`)
	assert.NotEmpty(t, backendStr, `"backend" field must not be empty`)

	// "available" must always be present as a boolean.
	_, exists = body["available"]
	assert.True(t, exists, `response must contain "available" field`)

	// "seccomp_enabled" must always be present.
	_, exists = body["seccomp_enabled"]
	assert.True(t, exists, `response must contain "seccomp_enabled" field`)

	// "kernel_level" must always be present.
	_, exists = body["kernel_level"]
	assert.True(t, exists, `response must contain "kernel_level" field`)
}

// TestHandleSandboxStatus_MethodNotAllowed verifies that POST/PUT/DELETE return 405.
func TestHandleSandboxStatus_MethodNotAllowed(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/api/v1/security/sandbox-status", nil)
			api.HandleSandboxStatus(w, r)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}
