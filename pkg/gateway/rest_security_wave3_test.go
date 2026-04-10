//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wave 3 REST endpoint tests — SEC-25 and SEC-28.
//
// These tests exercise the operator-facing surface added in Wave 3:
//   - GET  /api/v1/security/exec-proxy-status
//   - GET  /api/v1/security/prompt-guard
//   - PUT  /api/v1/security/prompt-guard
//
// The tests use newTestRestAPIWithHome so mutations actually land on a
// temp-dir config.json via safeUpdateConfigJSON — mirroring the
// allowlist-endpoint tests in rest_exec_test.go.

// TestHandleExecProxyStatus_Disabled returns enabled=false, running=false
// when cfg.Tools.Exec.EnableProxy is not set. This is the default state
// operators start in.
func TestHandleExecProxyStatus_Disabled(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/security/exec-proxy-status", nil)
	api.HandleExecProxyStatus(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, false, body["enabled"], "enabled must be false by default")
	assert.Equal(t, false, body["running"], "running must be false when no proxy")
	_, hasAddr := body["address"]
	assert.False(t, hasAddr, "address must be omitted when proxy is not running")
}

// TestHandleExecProxyStatus_MethodNotAllowed returns 405 for POST/PUT/DELETE.
// The status endpoint is read-only.
func TestHandleExecProxyStatus_MethodNotAllowed(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/api/v1/security/exec-proxy-status", nil)
			api.HandleExecProxyStatus(w, r)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}

// TestHandlePromptGuard_GET_Default returns medium when nothing is set,
// because NewPromptGuardFromConfig defaults to medium on empty input.
func TestHandlePromptGuard_GET_Default(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/security/prompt-guard", nil)
	api.HandlePromptGuard(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "medium", body["strictness"], "empty config must surface as medium")
	assert.Equal(t, false, body["restart_required"], "GET must not claim a restart is pending")
}

// TestHandlePromptGuard_PUT_HappyPath updates strictness and persists to
// config.json on disk. It also verifies restart_required: true in the
// response (SEC-12 — security-critical config is not hot-reloaded).
func TestHandlePromptGuard_PUT_HappyPath(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	for _, level := range []string{"low", "medium", "high"} {
		t.Run(level, func(t *testing.T) {
			payload := `{"strictness":"` + level + `"}`
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPut, "/api/v1/security/prompt-guard", strings.NewReader(payload))
			r.Header.Set("Content-Type", "application/json")
			api.HandlePromptGuard(w, r)

			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, level, resp["strictness"])
			assert.Equal(t, true, resp["restart_required"])

			// Confirm the file on disk was updated via safeUpdateConfigJSON.
			raw, err := os.ReadFile(api.configPath())
			require.NoError(t, err)
			var onDisk map[string]any
			require.NoError(t, json.Unmarshal(raw, &onDisk))
			sandboxDisk, _ := onDisk["sandbox"].(map[string]any)
			require.NotNil(t, sandboxDisk, "sandbox section must be written")
			assert.Equal(t, level, sandboxDisk["prompt_injection_level"])
		})
	}
}

// TestHandlePromptGuard_PUT_InvalidStrictness rejects unknown values with
// 400. The handler must fail closed on invalid config — silently fixing
// the value would hide operator misconfiguration.
func TestHandlePromptGuard_PUT_InvalidStrictness(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"unknown level", `{"strictness":"paranoid"}`},
		{"empty string", `{"strictness":""}`},
		{"numeric", `{"strictness":"1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := newTestRestAPIWithHome(t)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPut, "/api/v1/security/prompt-guard", strings.NewReader(tc.payload))
			r.Header.Set("Content-Type", "application/json")
			api.HandlePromptGuard(w, r)
			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
		})
	}
}

// TestHandlePromptGuard_PUT_InvalidJSON returns 400 on malformed body.
func TestHandlePromptGuard_PUT_InvalidJSON(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/security/prompt-guard", strings.NewReader(`not-json`))
	r.Header.Set("Content-Type", "application/json")
	api.HandlePromptGuard(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandlePromptGuard_MethodNotAllowed returns 405 for POST/DELETE.
func TestHandlePromptGuard_MethodNotAllowed(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/api/v1/security/prompt-guard", nil)
			api.HandlePromptGuard(w, r)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}
