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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// TestHandleRateLimits_* covers the PUT rate-limits semantics.
// GET semantics (disabled/enabled/wave4 MethodNotAllowed) remain in
// rest_security_wave4_test.go.

func putRateLimits(t *testing.T, api *restAPI, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/security/rate-limits", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = withAdminRole(r)
	w := httptest.NewRecorder()
	api.HandleRateLimits(w, r)
	return w
}

// TestHandleRateLimits_PersistsAllThreeFields verifies that all three fields are
// written to disk and readable via GET after a successful PUT.
func TestHandleRateLimits_PersistsAllThreeFields(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	body := `{"daily_cost_cap_usd":25.5,"max_agent_llm_calls_per_hour":100,"max_agent_tool_calls_per_minute":30}`
	w := putRateLimits(t, api, body)
	require.Equal(t, http.StatusOK, w.Code, "PUT must succeed: %s", w.Body)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["saved"])
	assert.Equal(t, false, resp["requires_restart"])

	applied, ok := resp["applied"].(map[string]any)
	require.True(t, ok, "applied must be an object")
	assert.Equal(t, float64(25.5), applied["daily_cost_cap_usd"])
	assert.Equal(t, float64(100), applied["max_agent_llm_calls_per_hour"])
	assert.Equal(t, float64(30), applied["max_agent_tool_calls_per_minute"])

	// Read back via GET to confirm persistence.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/security/rate-limits", nil)
	getW := httptest.NewRecorder()
	api.HandleRateLimits(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	var getResp map[string]any
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getResp))
	assert.Equal(t, float64(25.5), getResp["daily_cost_cap"])
}

// TestHandleRateLimits_PartialUpdate verifies that a PUT with only one field
// leaves the other pre-seeded fields unchanged.
func TestHandleRateLimits_PartialUpdate(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	// Pre-seed: set LLM cap to 50.
	preseed := `{"max_agent_llm_calls_per_hour":50}`
	w1 := putRateLimits(t, api, preseed)
	require.Equal(t, http.StatusOK, w1.Code, "preseed PUT must succeed: %s", w1.Body)

	// Partial update: only change daily cost cap.
	w2 := putRateLimits(t, api, `{"daily_cost_cap_usd":10}`)
	require.Equal(t, http.StatusOK, w2.Code, "partial PUT must succeed: %s", w2.Body)

	// Confirm LLM cap is preserved.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/security/rate-limits", nil)
	getW := httptest.NewRecorder()
	api.HandleRateLimits(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	var getResp map[string]any
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getResp))
	assert.Equal(t, float64(10), getResp["daily_cost_cap"], "daily_cost_cap must be updated")
	assert.Equal(t, float64(50), getResp["max_agent_llm_calls_per_hour"], "llm_calls cap must be preserved")
}

// TestHandleRateLimits_EmptyBodyNoOp verifies that {} returns 200 without
// changing any persisted values.
func TestHandleRateLimits_EmptyBodyNoOp(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	// Pre-seed a value.
	preseed := `{"max_agent_tool_calls_per_minute":20}`
	require.Equal(t, http.StatusOK, putRateLimits(t, api, preseed).Code)

	// Empty body no-op.
	w := putRateLimits(t, api, `{}`)
	require.Equal(t, http.StatusOK, w.Code, "empty body must succeed: %s", w.Body)

	// Confirm value unchanged.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/security/rate-limits", nil)
	getW := httptest.NewRecorder()
	api.HandleRateLimits(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)

	var getResp map[string]any
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getResp))
	assert.Equal(t, float64(20), getResp["max_agent_tool_calls_per_minute"], "tool cap must be unchanged after no-op")
}

// TestHandleRateLimits_NegativeRejected verifies that negative values are
// rejected with 400.
func TestHandleRateLimits_NegativeRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"daily_cost_cap_usd":-5}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "negative cost cap must return 400: %s", w.Body)
}

// TestHandleRateLimits_StringInIntFieldRejected verifies that a JSON string
// value in an integer field is rejected with 400.
func TestHandleRateLimits_StringInIntFieldRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"max_agent_llm_calls_per_hour":"50"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, `"50" in int field must return 400: %s`, w.Body)
}

// TestHandleRateLimits_FloatInIntFieldRejected verifies that a fractional float
// value in an integer field is rejected with 400.
func TestHandleRateLimits_FloatInIntFieldRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"max_agent_llm_calls_per_hour":10.5}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "10.5 in int field must return 400: %s", w.Body)
}

// TestHandleRateLimits_NullRejected verifies that JSON null in a numeric field
// is rejected with 400. JSON does not support NaN/Inf literals; null is the
// closest representable invalid numeric value in JSON.
func TestHandleRateLimits_NullRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"daily_cost_cap_usd":null}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "null in float field must return 400: %s", w.Body)
}

// TestHandleRateLimits_MaxInt64Accepted verifies that math.MaxInt64 is accepted.
func TestHandleRateLimits_MaxInt64Accepted(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"max_agent_llm_calls_per_hour":9223372036854775807}`)
	assert.Equal(t, http.StatusOK, w.Code, "MaxInt64 must be accepted: %s", w.Body)
}

// TestHandleRateLimits_OverflowRejected verifies that a value exceeding
// math.MaxInt64 is rejected with 400.
func TestHandleRateLimits_OverflowRejected(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	// 9223372036854775808 = MaxInt64 + 1.
	w := putRateLimits(t, api, `{"max_agent_llm_calls_per_hour":9223372036854775808}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, "overflow must return 400: %s", w.Body)
}

// TestHandleRateLimits_HotReload verifies that the response includes
// requires_restart: false (rate limits are hot-reloaded via the 2s config poll).
func TestHandleRateLimits_HotReload(t *testing.T) {
	api := newTestRestAPIWithHome(t)
	w := putRateLimits(t, api, `{"daily_cost_cap_usd":5}`)
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["requires_restart"], "rate limits must not require restart")
}

// TestHandleRateLimits_NonAdmin403 verifies that an authenticated user with
// user role receives 403 (RequireAdmin returns 401 for unauthenticated, 403 for
// authenticated non-admin).
func TestHandleRateLimits_NonAdmin403(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	r := httptest.NewRequest(http.MethodPut, "/api/v1/security/rate-limits",
		strings.NewReader(`{"daily_cost_cap_usd":5}`))
	r.Header.Set("Content-Type", "application/json")
	// Inject user role (authenticated but not admin) → 403.
	ctx := context.WithValue(r.Context(), RoleContextKey{}, config.UserRoleUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	api.HandleRateLimits(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code, "user-role caller must receive 403")
}

// TestHandleRateLimits_MethodNotAllowed verifies that DELETE returns 405.
func TestHandleRateLimits_MethodNotAllowed(t *testing.T) {
	api := newTestRestAPIWithHome(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/security/rate-limits", nil)
	api.HandleRateLimits(w, r)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
