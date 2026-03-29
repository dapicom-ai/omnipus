//go:build !cgo

// This test file uses //go:build !cgo so it compiles when CGO is disabled.
// When CGO is enabled, pkg/gateway imports pkg/channels/matrix which requires
// the libolm system library (olm/olm.h). If that library is installed,
// remove this build constraint and run tests normally.

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

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// restMockProvider satisfies providers.LLMProvider with no-op responses.
type restMockProvider struct{}

func (m *restMockProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{}, nil
}

func (m *restMockProvider) GetDefaultModel() string { return "test-model" }

// newTestRestAPI creates a restAPI with a minimal AgentLoop for unit testing.
// OMNIPUS_BEARER_TOKEN is unset so auth is disabled (development mode).
func newTestRestAPI(t *testing.T) (*restAPI, func()) {
	t.Helper()
	t.Setenv("OMNIPUS_BEARER_TOKEN", "") // disable auth in tests

	tmpDir := t.TempDir()
	cfg := &config.Config{
		Gateway: config.GatewayConfig{Host: "127.0.0.1", Port: 8080},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: tmpDir,
				ModelName: "test-model",
				MaxTokens: 4096,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(cfg, msgBus, &restMockProvider{})

	api := &restAPI{
		cfg:           cfg,
		agentLoop:     al,
		partitions:    nil,
		allowedOrigin: "http://localhost:3000",
	}
	return api, func() {}
}

// --- HandleAgents tests ---

// TestHandleAgentsListAlwaysIncludesSystemAgent verifies that GET /api/v1/agents
// always includes the omnipus-system agent regardless of config.
// BDD: Given no agents are configured,
// When GET /api/v1/agents is called,
// Then the response includes the system agent with id "omnipus-system".
// Traces to: wave5a-wire-ui-spec.md — Scenario: Agent list always includes system agent (US-6 AC1)
func TestHandleAgentsListAlwaysIncludesSystemAgent(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	api.HandleAgents(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var agents []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &agents))

	found := false
	for _, ag := range agents {
		if ag.ID == "omnipus-system" && ag.Type == "system" {
			found = true
			break
		}
	}
	assert.True(t, found, "system agent must always be present in the agents list")
}

// TestHandleAgentsListIncludesConfiguredAgents verifies custom agents from config appear in the list.
// BDD: Given one custom agent is configured,
// When GET /api/v1/agents is called,
// Then the response includes the system agent plus the custom agent.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Agent list includes configured agents (US-6 AC2)
func TestHandleAgentsListIncludesConfiguredAgents(t *testing.T) {
	t.Setenv("OMNIPUS_BEARER_TOKEN", "")

	tmpDir := t.TempDir()
	cfg := &config.Config{
		Gateway: config.GatewayConfig{Host: "127.0.0.1", Port: 8080},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: tmpDir,
				ModelName: "test-model",
				MaxTokens: 4096,
			},
			List: []config.AgentConfig{
				{ID: "my-agent", Name: "My Agent"},
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al := agent.NewAgentLoop(cfg, msgBus, &restMockProvider{})
	api := &restAPI{cfg: cfg, agentLoop: al}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	api.HandleAgents(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &agents))

	assert.GreaterOrEqual(t, len(agents), 2, "must include system agent + custom agent")

	ids := make(map[string]bool)
	for _, ag := range agents {
		ids[ag.ID] = true
	}
	assert.True(t, ids["omnipus-system"], "system agent must be present")
	assert.True(t, ids["my-agent"], "custom agent must be present")
}

// TestHandleAgentsGetByIDSystemAgent verifies GET /api/v1/agents/omnipus-system returns the system agent.
// BDD: Given agent id "omnipus-system",
// When GET /api/v1/agents/omnipus-system is called,
// Then the response has id "omnipus-system" and type "system".
// Traces to: wave5a-wire-ui-spec.md — Scenario: Get agent by ID (US-7 AC1)
func TestHandleAgentsGetByIDSystemAgent(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents/omnipus-system", nil)
	api.HandleAgents(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "omnipus-system", resp.ID)
	assert.Equal(t, "system", resp.Type)
}

// TestHandleAgentsGetByIDNotFound verifies GET /api/v1/agents/{unknown} returns 404.
// BDD: Given agent id "does-not-exist",
// When GET /api/v1/agents/does-not-exist is called,
// Then the response has status 404.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Get agent by ID not found (US-7 AC2)
func TestHandleAgentsGetByIDNotFound(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/agents/does-not-exist", nil)
	api.HandleAgents(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestHandleAgentsCreateValidation verifies POST /api/v1/agents with empty name returns 422.
// BDD: Given a create-agent request with name="",
// When POST /api/v1/agents is called,
// Then the response has status 422 and an error about name being required.
// Traces to: wave5a-wire-ui-spec.md — Dataset: Agent Configuration row 2 (empty name → validation error)
func TestHandleAgentsCreateValidation(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	body := `{"name": ""}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	api.HandleAgents(w, r)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "name")
}

// TestHandleAgentsCreate verifies POST /api/v1/agents with a valid name returns 201.
// BDD: Given a create-agent request with name="Scout",
// When POST /api/v1/agents is called,
// Then the response has status 201 and id derived from the name.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Create a new custom agent (US-8 AC1)
func TestHandleAgentsCreate(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	body := `{"name": "Scout", "model": "claude-sonnet-4-6"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	api.HandleAgents(w, r)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "scout", resp.ID, "id must be lowercased name")
	assert.Equal(t, "Scout", resp.Name)
	assert.Equal(t, "custom", resp.Type)
}

// TestHandleAgentsCreateWithExplicitID verifies POST uses the provided ID if given.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Create a new custom agent (US-8 AC2)
func TestHandleAgentsCreateWithExplicitID(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	body := `{"id": "my-scout", "name": "Scout"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	api.HandleAgents(w, r)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "my-scout", resp.ID)
}

// --- HandleSessions tests ---

// TestHandleSessionsListNilPartitions verifies that when partitions is nil,
// GET /api/v1/sessions returns 200 with an empty sessions array.
// BDD: Given the session store is not configured (nil),
// When GET /api/v1/sessions is called,
// Then the response has status 200 and sessions:[].
// Traces to: wave5a-wire-ui-spec.md — Scenario: Session list returns empty when no store (US-15 AC1)
func TestHandleSessionsListNilPartitions(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()
	api.partitions = nil

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	api.HandleSessions(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var sessions []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sessions))
	assert.Empty(t, sessions, "sessions must be empty when partition store is nil")
}

// TestHandleSessionsGetNotFoundNilPartitions verifies 503 when store is nil and ID given.
// BDD: Given the session store is not configured,
// When GET /api/v1/sessions/some-id is called,
// Then the response has status 503.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Session detail unavailable (US-15 AC2)
func TestHandleSessionsGetNotFoundNilPartitions(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()
	api.partitions = nil

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session_1234", nil)
	api.HandleSessions(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestHandleSessionsGetNotFound verifies 404 when session ID does not exist in store.
// BDD: Given a PartitionStore with no sessions,
// When GET /api/v1/sessions/unknown-id is called,
// Then the response has status 404.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Session not found returns 404 (US-15 AC3)
func TestHandleSessionsGetNotFound(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	// Real PartitionStore pointed at a temp dir — session doesn't exist
	ps := newTestPartitionStore(t)
	api.partitions = ps

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/session_does_not_exist", nil)
	api.HandleSessions(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// newTestPartitionStore returns a PartitionStore backed by a temp dir.
func newTestPartitionStore(t *testing.T) *session.PartitionStore {
	t.Helper()
	return session.NewPartitionStore(t.TempDir(), "test-agent")
}

// --- HandleDoctor tests ---

// TestHandleDoctorReturnsOK verifies GET /api/v1/doctor returns 200 with status "ok".
// BDD: Given the gateway is running,
// When GET /api/v1/doctor is called,
// Then the response has status 200 and top-level status "ok".
// Traces to: wave5a-wire-ui-spec.md — Scenario: Doctor endpoint returns health status (US-16 AC1)
func TestHandleDoctorReturnsOK(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/doctor", nil)
	api.HandleDoctor(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Status string         `json:"status"`
		Checks map[string]any `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Contains(t, resp.Checks, "gateway")
	assert.Contains(t, resp.Checks, "agent_loop")
	assert.Contains(t, resp.Checks, "session_store")
	assert.Contains(t, resp.Checks, "go_runtime")
}

// TestHandleDoctorMethodNotAllowed verifies non-GET returns 405.
// Traces to: wave5a-wire-ui-spec.md — Dataset: Doctor endpoint — method validation
func TestHandleDoctorMethodNotAllowed(t *testing.T) {
	api, cleanup := newTestRestAPI(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/doctor", nil)
	api.HandleDoctor(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- redactSensitiveFields tests ---

// TestRedactSensitiveFields verifies that credential fields are redacted in config responses.
// BDD: Given a config map containing fields named "api_key", "token", "secret",
// When redactSensitiveFields is called,
// Then those fields are replaced with "[redacted]" and non-sensitive fields are unchanged.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Config endpoint redacts credentials (SEC-23)
func TestRedactSensitiveFields(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		wantKey   string
		wantValue any
	}{
		// Dataset: Redaction — row 1
		{
			name:      "api_key is redacted",
			input:     map[string]any{"api_key": "sk-abc-123"},
			wantKey:   "api_key",
			wantValue: "[redacted]",
		},
		// Dataset: Redaction — row 2
		{
			name:      "token is redacted",
			input:     map[string]any{"bearer_token": "tok-xyz"},
			wantKey:   "bearer_token",
			wantValue: "[redacted]",
		},
		// Dataset: Redaction — row 3
		{
			name:      "secret is redacted",
			input:     map[string]any{"client_secret": "very-secret"},
			wantKey:   "client_secret",
			wantValue: "[redacted]",
		},
		// Dataset: Redaction — row 4
		{
			name:      "password is redacted",
			input:     map[string]any{"password": "hunter2"},
			wantKey:   "password",
			wantValue: "[redacted]",
		},
		// Dataset: Redaction — row 5 (empty string not redacted)
		{
			name:      "empty string value not redacted",
			input:     map[string]any{"api_key": ""},
			wantKey:   "api_key",
			wantValue: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			redactSensitiveFields(tc.input)
			assert.Equal(t, tc.wantValue, tc.input[tc.wantKey])
		})
	}
}

// TestRedactSensitiveFieldsPreservesNonSensitive verifies safe fields are unchanged.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Config endpoint redacts credentials (SEC-23)
func TestRedactSensitiveFieldsPreservesNonSensitive(t *testing.T) {
	m := map[string]any{
		"host":    "localhost",
		"port":    8080,
		"api_key": "should-be-gone",
		"version": "1.0.0",
	}
	redactSensitiveFields(m)

	assert.Equal(t, "localhost", m["host"])
	assert.Equal(t, 8080, m["port"])
	assert.Equal(t, "[redacted]", m["api_key"])
	assert.Equal(t, "1.0.0", m["version"])
}

// TestRedactSensitiveFieldsNested verifies nested maps are recursively redacted.
// Traces to: wave5a-wire-ui-spec.md — Scenario: Config endpoint redacts credentials (SEC-23)
func TestRedactSensitiveFieldsNested(t *testing.T) {
	m := map[string]any{
		"provider": map[string]any{
			"name":    "anthropic",
			"api_key": "sk-nested",
		},
	}
	redactSensitiveFields(m)

	provider, ok := m["provider"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "anthropic", provider["name"])
	assert.Equal(t, "[redacted]", provider["api_key"])
}
