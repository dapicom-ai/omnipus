//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/gateway/ctxkey"
)

// withAdminCtx returns a request with the admin role injected into context.
// Separate from the withAdminRole helper in rest_tool_policies_test.go to
// avoid a duplicate declaration (both live in package gateway).
func withAdminCtx(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, config.UserRoleAdmin)
	return r.WithContext(ctx)
}

// withUserCtx injects a non-admin role so tests can verify 403 responses.
func withUserCtx(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxkey.RoleContextKey{}, config.UserRoleUser)
	return r.WithContext(ctx)
}

// newPendingRestartAPI builds a restAPI wired for pending-restart tests.
// applied is the boot-time snapshot; persisted is written to the temp config.json.
func newPendingRestartAPI(t *testing.T, applied, persisted map[string]any) *restAPI {
	t.Helper()
	tmpDir := t.TempDir()

	raw, err := json.Marshal(persisted)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", raw, 0o600))

	var appliedCfg config.Config
	if applied != nil {
		appliedRaw, marshalErr := json.Marshal(applied)
		require.NoError(t, marshalErr)
		require.NoError(t, json.Unmarshal(appliedRaw, &appliedCfg))
	}

	api := newTestRestAPIWithHome(t)
	api.homePath = tmpDir
	api.appliedConfig = &appliedCfg
	return api
}

// decodeDiffs unmarshals the response body into a slice of pendingRestartEntry.
func decodeDiffs(t *testing.T, body []byte) []pendingRestartEntry {
	t.Helper()
	var diffs []pendingRestartEntry
	require.NoError(t, json.Unmarshal(body, &diffs))
	return diffs
}

// TestHandlePendingRestart_ListsQueuedChanges verifies that a mismatch on a
// restart-gated key surfaces as a diff entry. The applied config is built from
// a struct so all zero-value fields are present in both maps; sandbox.mode is
// the only gated key that differs.
func TestHandlePendingRestart_ListsQueuedChanges(t *testing.T) {
	// Build applied config with sandbox.mode="off".
	appliedCfg := &config.Config{}
	appliedCfg.Sandbox.Mode = "off"

	// Build a persisted map that matches the applied struct exactly, then
	// override sandbox.mode to "enforce" so exactly one key differs.
	appliedRaw, err := json.Marshal(appliedCfg)
	require.NoError(t, err)
	var persistedMap map[string]any
	require.NoError(t, json.Unmarshal(appliedRaw, &persistedMap))
	sandboxMap, _ := persistedMap["sandbox"].(map[string]any)
	if sandboxMap == nil {
		sandboxMap = map[string]any{}
		persistedMap["sandbox"] = sandboxMap
	}
	sandboxMap["mode"] = "enforce"

	tmpDir := t.TempDir()
	persistedRaw, err := json.Marshal(persistedMap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", persistedRaw, 0o600))

	api := newTestRestAPIWithHome(t)
	api.homePath = tmpDir
	api.appliedConfig = appliedCfg

	w := httptest.NewRecorder()
	r := withAdminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil))
	api.HandlePendingRestart(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	diffs := decodeDiffs(t, w.Body.Bytes())
	require.Len(t, diffs, 1, "exactly one gated key changed")
	assert.Equal(t, "sandbox.mode", diffs[0].Key)
	assert.Equal(t, "enforce", diffs[0].PersistedValue)
	assert.Equal(t, "off", diffs[0].AppliedValue)
}

// TestHandlePendingRestart_EmptyAfterApply verifies that when persisted and
// applied configs are equal for all gated keys, the response is an empty array
// (not null).
func TestHandlePendingRestart_EmptyAfterApply(t *testing.T) {
	same := map[string]any{
		"sandbox": map[string]any{"mode": "enforce", "enabled": true},
		"gateway": map[string]any{"port": float64(8080)},
	}
	api := newPendingRestartAPI(t, same, same)

	w := httptest.NewRecorder()
	r := withAdminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil))
	api.HandlePendingRestart(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	// Must be "[]" (empty array), not "null".
	body := w.Body.String()
	assert.Contains(t, body, "[", "body must be a JSON array, not null")
	diffs := decodeDiffs(t, w.Body.Bytes())
	assert.Empty(t, diffs, "no diff when configs are equal")
}

// TestHandlePendingRestart_SetThenRevertClearsDiff verifies the diff-based
// semantics: if a key was changed to Y and then changed back to X (the applied
// value) before restart, the diff returns [] and no banner shows.
func TestHandlePendingRestart_SetThenRevertClearsDiff(t *testing.T) {
	// Applied had mode="off"; persisted was changed to "enforce" then reverted
	// to "off" — so persisted and applied are now equal. Derive both sides from
	// the same struct to guarantee all gated keys are structurally identical.
	appliedCfg := &config.Config{}
	appliedCfg.Sandbox.Mode = "off"

	appliedRaw, err := json.Marshal(appliedCfg)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", appliedRaw, 0o600))

	api := newTestRestAPIWithHome(t)
	api.homePath = tmpDir
	api.appliedConfig = appliedCfg

	w := httptest.NewRecorder()
	r := withAdminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil))
	api.HandlePendingRestart(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	diffs := decodeDiffs(t, w.Body.Bytes())
	assert.Empty(t, diffs, "reverted change must not appear in diff")
}

// TestHandlePendingRestart_NonAdmin403 verifies that a non-admin caller
// receives 403.
func TestHandlePendingRestart_NonAdmin403(t *testing.T) {
	api := newPendingRestartAPI(t, nil, map[string]any{})

	w := httptest.NewRecorder()
	r := withUserCtx(httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil))
	api.HandlePendingRestart(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestHandlePendingRestart_HotReloadKeyNotInDiff verifies that hot-reload keys
// (e.g. sandbox.prompt_injection_level) are excluded from the diff even when
// their values differ between applied and persisted.
func TestHandlePendingRestart_HotReloadKeyNotInDiff(t *testing.T) {
	// Build an applied config where all restart-gated keys match the persisted
	// file. Only prompt_injection_level (a hot-reload key) differs — it must
	// not appear in the diff output.
	appliedCfg := &config.Config{}
	appliedCfg.Sandbox.Mode = "enforce"
	appliedCfg.Sandbox.PromptInjectionLevel = "medium"

	// Derive persisted from appliedCfg (guarantees structural parity for all
	// gated keys), then flip only the hot-reload key.
	appliedRaw, err := json.Marshal(appliedCfg)
	require.NoError(t, err)
	var persistedMap map[string]any
	require.NoError(t, json.Unmarshal(appliedRaw, &persistedMap))
	sandboxMap, _ := persistedMap["sandbox"].(map[string]any)
	if sandboxMap == nil {
		sandboxMap = map[string]any{}
		persistedMap["sandbox"] = sandboxMap
	}
	sandboxMap["prompt_injection_level"] = "high"

	tmpDir := t.TempDir()
	persistedRaw, err := json.Marshal(persistedMap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", persistedRaw, 0o600))

	api := newTestRestAPIWithHome(t)
	api.homePath = tmpDir
	api.appliedConfig = appliedCfg

	w := httptest.NewRecorder()
	r := withAdminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/config/pending-restart", nil))
	api.HandlePendingRestart(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	diffs := decodeDiffs(t, w.Body.Bytes())
	for _, d := range diffs {
		assert.NotEqual(t, "sandbox.prompt_injection_level", d.Key,
			"hot-reload key must never appear in pending-restart diff")
	}
	assert.Empty(t, diffs, "only the hot-reload key changed; diff must be empty")
}

// TestHandlePendingRestart_MethodNotAllowed verifies that POST and PUT return 405.
func TestHandlePendingRestart_MethodNotAllowed(t *testing.T) {
	api := newPendingRestartAPI(t, nil, map[string]any{})

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := withAdminCtx(httptest.NewRequest(method, "/api/v1/config/pending-restart", nil))
			api.HandlePendingRestart(w, r)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}

// TestGetAtPath_DottedPath verifies that getAtPath returns the correct value
// for a two-segment dotted path.
func TestGetAtPath_DottedPath(t *testing.T) {
	m := map[string]any{
		"gateway": map[string]any{
			"port": float64(5000),
		},
	}
	val := getAtPath(m, "gateway.port")
	assert.Equal(t, float64(5000), val)
}

// TestGetAtPath_MissingSegment verifies that getAtPath returns nil without
// panicking when a path segment is absent.
func TestGetAtPath_MissingSegment(t *testing.T) {
	m := map[string]any{
		"gateway": map[string]any{},
	}
	val := getAtPath(m, "gateway.port")
	assert.Nil(t, val, "missing leaf must return nil")

	val2 := getAtPath(m, "sandbox.mode")
	assert.Nil(t, val2, "missing root segment must return nil")
}
