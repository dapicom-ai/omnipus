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
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- matchBlockedPath unit tests ---

func TestMatchBlockedPath_NestedGatewayUsers(t *testing.T) {
	body := map[string]any{
		"gateway": map[string]any{
			"users": []any{map[string]any{"username": "evil", "role": "admin"}},
		},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.True(t, blocked, "nested gateway.users must be blocked")
	assert.Equal(t, "gateway.users", path)
}

func TestMatchBlockedPath_DotPathLiteral(t *testing.T) {
	body := map[string]any{
		"gateway.users": []any{map[string]any{"username": "evil"}},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.True(t, blocked, "dot-path literal key gateway.users must be blocked")
	assert.Equal(t, "gateway.users", path)
}

func TestMatchBlockedPath_MixedBenignAndBlocked(t *testing.T) {
	body := map[string]any{
		"gateway": map[string]any{
			"port":  float64(5000),
			"users": []any{map[string]any{"username": "evil"}},
		},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.True(t, blocked, "benign sibling must not shield the blocked path")
	assert.Equal(t, "gateway.users", path)
}

func TestMatchBlockedPath_TopLevelSandbox(t *testing.T) {
	body := map[string]any{
		"sandbox": map[string]any{},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.True(t, blocked, "top-level sandbox must be blocked")
	assert.Equal(t, "sandbox", path)
}

func TestMatchBlockedPath_UnblockedKey(t *testing.T) {
	body := map[string]any{
		"gateway": map[string]any{"port": float64(5000)},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.False(t, blocked, "gateway.port alone must not be blocked")
	assert.Equal(t, "", path)
}

func TestMatchBlockedPath_DevModeBypass(t *testing.T) {
	body := map[string]any{
		"gateway": map[string]any{"dev_mode_bypass": true},
	}
	path, blocked := matchBlockedPath(body, blockedPaths)
	assert.True(t, blocked, "nested gateway.dev_mode_bypass must be blocked")
	assert.Equal(t, "gateway.dev_mode_bypass", path)
}

// --- Integration tests: PUT /api/v1/config via updateConfig handler ---
//
// These tests call updateConfig directly to bypass the admin middleware.
// The walker is the unit under test — the middleware is orthogonal.

// readConfigOnDisk returns the raw config.json contents for the given API so
// tests can assert atomic-reject semantics (nothing persisted on 403).
func readConfigOnDisk(t *testing.T, api *restAPI) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(api.homePath, "config.json"))
	require.NoError(t, err, "config.json must be readable")
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m), "config.json must be valid JSON")
	return m
}

func TestConfigPUT_CannotSetGatewayUsers_Nested(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	before := readConfigOnDisk(t, api)

	body := `{"gateway":{"users":[{"username":"evil","role":"admin"}]}}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.updateConfig(w, r)

	require.Equal(t, http.StatusForbidden, w.Code, "nested gateway.users must be 403")
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "gateway.users",
		"error message must name the blocked path so the operator knows which endpoint to use")

	after := readConfigOnDisk(t, api)
	assert.Equal(t, before, after, "rejected PUT must not mutate config.json")
}

func TestConfigPUT_CannotSetGatewayUsers_DotPathLiteral(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	before := readConfigOnDisk(t, api)

	body := `{"gateway.users":[{"username":"evil","role":"admin"}]}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.updateConfig(w, r)

	require.Equal(t, http.StatusForbidden, w.Code, "dot-path literal gateway.users must be 403")
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "gateway.users")

	after := readConfigOnDisk(t, api)
	assert.Equal(t, before, after, "rejected PUT must not mutate config.json")
}

func TestConfigPUT_CannotSetGatewayUsers_Mixed(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	before := readConfigOnDisk(t, api)

	body := `{"gateway":{"port":5000,"users":[{"username":"evil","role":"admin"}]}}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.updateConfig(w, r)

	require.Equal(t, http.StatusForbidden, w.Code,
		"mixed body with benign sibling must still be rejected")
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "gateway.users")

	after := readConfigOnDisk(t, api)
	assert.Equal(t, before, after,
		"atomic reject: benign sibling (gateway.port) must NOT be persisted when the body contains a blocked path")
}

func TestConfigPUT_CannotSetDevModeBypass(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	before := readConfigOnDisk(t, api)

	body := `{"gateway":{"dev_mode_bypass":true}}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.updateConfig(w, r)

	require.Equal(t, http.StatusForbidden, w.Code, "gateway.dev_mode_bypass must be 403")
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "gateway.dev_mode_bypass")

	after := readConfigOnDisk(t, api)
	assert.Equal(t, before, after, "rejected PUT must not mutate config.json")
}

func TestConfigPUT_UnblockedKeySucceeds(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	body := `{"gateway":{"port":5001}}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.updateConfig(w, r)

	require.Equal(t, http.StatusOK, w.Code,
		"unblocked key must pass through to safeUpdateConfigJSON: body=%s", w.Body.String())

	after := readConfigOnDisk(t, api)
	gw, ok := after["gateway"].(map[string]any)
	require.True(t, ok, "gateway section must be an object after write")
	assert.Equal(t, float64(5001), gw["port"], "gateway.port must reflect the PUT body")
}
