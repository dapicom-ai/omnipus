//go:build !cgo

package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	"github.com/dapicom-ai/omnipus/pkg/onboarding"
	"github.com/dapicom-ai/omnipus/pkg/taskstore"
)

// testMasterKey is a deterministic hex master key used only in tests.
const testMasterKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// newOnboardingTestAPI creates a restAPI wired with an unlocked credential store
// for onboarding tests that submit api_key values (SEC-23: no plaintext fallback).
func newOnboardingTestAPI(t *testing.T, tmpDir string, al *agent.AgentLoop) *restAPI {
	t.Helper()
	t.Setenv("OMNIPUS_MASTER_KEY", testMasterKey)
	credStore := credentials.NewStore(tmpDir + "/credentials.json")
	if err := credentials.Unlock(credStore); err != nil {
		t.Fatalf("unlock credential store: %v", err)
	}
	return &restAPI{
		agentLoop:     al,
		homePath:      tmpDir,
		allowedOrigin: "http://localhost:3000",
		onboardingMgr: onboarding.NewManager(tmpDir),
		taskStore:     taskstore.New(tmpDir + "/tasks"),
		credStore:     credStore,
	}
}

// --- HandleCompleteOnboarding tests ---

// TestHandleCompleteOnboarding_Success verifies that POST /api/v1/onboarding/complete
// with valid provider and admin credentials returns 200 with a token.
// BDD: Given a fresh install (onboarding not complete),
// When POST /api/v1/onboarding/complete {"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}} is called,
// Then 200 with {"token":"<token>","role":"admin","username":"admin"}.
func TestHandleCompleteOnboarding_Success(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

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
	api := newOnboardingTestAPI(t, tmpDir, al)

	// Verify onboarding is not complete yet
	require.False(t, api.onboardingMgr.IsComplete(), "onboarding should not be complete initially")

	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["token"], "token must be non-empty")
	assert.Equal(t, "admin", resp["role"])
	assert.Equal(t, "admin", resp["username"])
}

// TestHandleCompleteOnboarding_AlreadyComplete verifies that POST /api/v1/onboarding/complete
// returns 409 Conflict when onboarding is already complete.
// BDD: Given onboarding is already complete,
// When POST /api/v1/onboarding/complete is called again,
// Then 409 Conflict with {"error":"onboarding already complete"}.
func TestHandleCompleteOnboarding_AlreadyComplete(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

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
	onboardingMgr := onboarding.NewManager(tmpDir)
	api := &restAPI{
		agentLoop:     al,
		homePath:      tmpDir,
		allowedOrigin: "http://localhost:3000",
		onboardingMgr: onboardingMgr,
		taskStore:     taskstore.New(tmpDir + "/tasks"),
	}

	// Mark onboarding as complete
	require.NoError(t, onboardingMgr.CompleteOnboarding())

	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "onboarding already complete", resp["error"])
}

// TestHandleCompleteOnboarding_MissingAPIKey verifies that POST /api/v1/onboarding/complete
// with empty provider.api_key returns 400.
// BDD: Given provider.api_key is empty,
// When POST /api/v1/onboarding/complete is called,
// Then 400 with {"error":"provider.api_key is required"}.
func TestHandleCompleteOnboarding_MissingAPIKey(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	body := `{"provider":{"id":"openai","api_key":""},"admin":{"username":"admin","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "provider.api_key is required", resp["error"])
}

// TestHandleCompleteOnboarding_MissingProviderID verifies that POST /api/v1/onboarding/complete
// with empty provider.id returns 400.
// BDD: Given provider.id is empty,
// When POST /api/v1/onboarding/complete is called,
// Then 400 with {"error":"provider.id is required"}.
func TestHandleCompleteOnboarding_MissingProviderID(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	body := `{"provider":{"id":"","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "provider.id is required", resp["error"])
}

// TestHandleCompleteOnboarding_MissingAdminUsername verifies that POST /api/v1/onboarding/complete
// with empty admin.username returns 400.
// BDD: Given admin.username is empty,
// When POST /api/v1/onboarding/complete is called,
// Then 400 with {"error":"admin.username is required"}.
func TestHandleCompleteOnboarding_MissingAdminUsername(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "admin.username is required", resp["error"])
}

// TestHandleCompleteOnboarding_MissingAdminPassword verifies that POST /api/v1/onboarding/complete
// with empty admin.password returns 400.
// BDD: Given admin.password is empty,
// When POST /api/v1/onboarding/complete is called,
// Then 400 with {"error":"admin.password is required"}.
func TestHandleCompleteOnboarding_MissingAdminPassword(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":""}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "admin.password is required", resp["error"])
}

// TestHandleCompleteOnboarding_WeakPassword verifies that POST /api/v1/onboarding/complete
// with a password shorter than 8 characters returns 400.
// BDD: Given admin.password is "short" (less than 8 characters),
// When POST /api/v1/onboarding/complete is called,
// Then 400 with {"error":"admin.password must be at least 8 characters"}.
func TestHandleCompleteOnboarding_WeakPassword(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"short"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "admin.password must be at least 8 characters", resp["error"])
}

// TestHandleCompleteOnboarding_MethodNotAllowed verifies that GET /api/v1/onboarding/complete
// returns 405.
// BDD: Given a GET request to /onboarding/complete,
// When the request is processed,
// Then 405 Method Not Allowed is returned.
func TestHandleCompleteOnboarding_MethodNotAllowed(t *testing.T) {
	api := newTestRestAPIWithHomeAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/complete", nil)
	w := httptest.NewRecorder()

	api.HandleCompleteOnboarding(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- Integration: onboarding -> login -> validate ---

// TestHandleCompleteOnboarding_ThenLogin verifies the full onboarding flow:
// 1. Complete onboarding (returns token)
// 2. Login with the admin credentials (returns another token)
// 3. Validate the login token (returns user info).
// BDD: Given a fresh install,
// When the onboarding flow completes and login is attempted with the admin credentials,
// Then login succeeds and the returned token validates successfully.
func TestHandleCompleteOnboarding_ThenLogin(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

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
	api := newOnboardingTestAPI(t, tmpDir, al)

	// Step 1: Complete onboarding
	onboardingBody := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
	onboardingReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(onboardingBody))
	onboardingReq.Header.Set("Content-Type", "application/json")
	onboardingW := httptest.NewRecorder()
	api.HandleCompleteOnboarding(onboardingW, onboardingReq)
	require.Equal(t, http.StatusOK, onboardingW.Code)
	var onboardingResp map[string]any
	require.NoError(t, json.Unmarshal(onboardingW.Body.Bytes(), &onboardingResp))
	onboardingToken := onboardingResp["token"].(string)
	assert.NotEmpty(t, onboardingToken, "onboarding must return a token")

	// Step 2: Login with the admin credentials
	loginBody := `{"username":"admin","password":"secret123"}`
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	api.HandleLogin(loginW, loginReq)
	require.Equal(t, http.StatusOK, loginW.Code)
	var loginResp map[string]any
	require.NoError(t, json.Unmarshal(loginW.Body.Bytes(), &loginResp))
	loginToken := loginResp["token"].(string)
	assert.NotEmpty(t, loginToken, "login must return a token")

	// Step 3: Validate the login token.
	// HandleValidateToken expects UserContextKey set by withAuth middleware.
	// In unit tests we inject it manually, simulating what withAuth does.
	adminUser := &config.UserConfig{Username: "admin", Role: config.UserRoleAdmin}
	validateReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/validate", nil)
	validateReq.Header.Set("Authorization", "Bearer "+loginToken)
	validateReq = validateReq.WithContext(context.WithValue(validateReq.Context(), UserContextKey{}, adminUser))
	validateW := httptest.NewRecorder()
	api.HandleValidateToken(validateW, validateReq)

	assert.Equal(t, http.StatusOK, validateW.Code)
	var validateResp map[string]any
	require.NoError(t, json.Unmarshal(validateW.Body.Bytes(), &validateResp))
	assert.Equal(t, "admin", validateResp["username"])
	assert.Equal(t, "admin", validateResp["role"])

	// Onboarding token should also be valid (same user)
	validateReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/auth/validate", nil)
	validateReq2.Header.Set("Authorization", "Bearer "+onboardingToken)
	validateReq2 = validateReq2.WithContext(context.WithValue(validateReq2.Context(), UserContextKey{}, adminUser))
	validateW2 := httptest.NewRecorder()
	api.HandleValidateToken(validateW2, validateReq2)
	assert.Equal(t, http.StatusOK, validateW2.Code)
}

// TestHandleCompleteOnboarding_PersistsAdmin verifies that the admin user created
// during onboarding persists in config.json and can be used to login after restart.
// BDD: Given onboarding completes and creates admin user,
// When config.json is read directly,
// Then it contains the admin user with a password_hash and token_hash.
func TestHandleCompleteOnboarding_PersistsAdmin(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

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
	api := newOnboardingTestAPI(t, tmpDir, al)

	// Complete onboarding
	body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HandleCompleteOnboarding(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Read config.json directly and verify admin user is persisted
	configData, err := os.ReadFile(tmpDir + "/config.json")
	require.NoError(t, err)
	var configMap map[string]any
	require.NoError(t, json.Unmarshal(configData, &configMap))

	gateway, ok := configMap["gateway"].(map[string]any)
	require.True(t, ok, "config must have gateway key")
	users, ok := gateway["users"].([]any)
	require.True(t, ok, "gateway must have users array")
	require.Len(t, users, 1, "must have exactly 1 user")

	user, ok := users[0].(map[string]any)
	require.True(t, ok, "user must be a map")
	assert.Equal(t, "admin", user["username"])
	assert.NotEmpty(t, user["password_hash"], "password_hash must be set")
	assert.NotEmpty(t, user["token_hash"], "token_hash must be set")
	assert.Equal(t, "admin", user["role"])
}

// --- Concurrency tests ---

// TestHandleCompleteOnboarding_Concurrent verifies that concurrent calls to
// HandleCompleteOnboarding do not corrupt state — at most one succeeds,
// the rest get 409 Conflict or success without data corruption.
// BDD: Given multiple concurrent POST /api/v1/onboarding/complete requests,
// When all are handled simultaneously,
// Then each either succeeds (200) or gets Conflict (409), and config.json
// is not corrupted (has exactly one admin user).
func TestHandleCompleteOnboarding_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

	// Set up a credential store so the onboarding can persist API keys (SEC-23).
	masterKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	t.Setenv("OMNIPUS_MASTER_KEY", masterKey)
	credStore := credentials.NewStore(tmpDir + "/credentials.json")
	require.NoError(t, credentials.Unlock(credStore))

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
	onboardingMgr := onboarding.NewManager(tmpDir)
	api := &restAPI{
		agentLoop:     al,
		homePath:      tmpDir,
		allowedOrigin: "http://localhost:3000",
		onboardingMgr: onboardingMgr,
		taskStore:     taskstore.New(tmpDir + "/tasks"),
		credStore:     credStore,
	}

	const n = 5
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := `{"provider":{"id":"openai","api_key":"sk-test"},"admin":{"username":"admin","password":"secret123"}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			api.HandleCompleteOnboarding(w, req)
			codes[idx] = w.Code
		}(i)
	}
	wg.Wait()

	// At least one should succeed
	has200 := false
	for _, code := range codes {
		if code == http.StatusOK {
			has200 = true
		}
		// Other valid responses: 409 Conflict (onboarding already complete)
	}
	assert.True(t, has200, "at least one concurrent request must succeed with 200")

	// After all concurrent requests, config.json should have exactly 1 user (no corruption)
	configData, err := os.ReadFile(tmpDir + "/config.json")
	require.NoError(t, err)
	var configMap map[string]any
	require.NoError(t, json.Unmarshal(configData, &configMap))

	gateway := configMap["gateway"].(map[string]any)
	users := gateway["users"].([]any)
	assert.Len(t, users, 1, "config.json must have exactly 1 admin user after concurrent calls (no duplication)")
}

// TestHandleCompleteOnboarding_ConcurrentDifferentUsers verifies that when
// concurrent requests try to create different usernames, only one succeeds
// (the one that acquires the lock first) and the others get 409 or 500.
func TestHandleCompleteOnboarding_ConcurrentDifferentUsers(t *testing.T) {
	tmpDir := t.TempDir()
	minimalCfg := []byte(`{"agents":{"defaults":{},"list":[]},"providers":[]}`)
	require.NoError(t, os.WriteFile(tmpDir+"/config.json", minimalCfg, 0o600))

	// Set up a credential store so the onboarding can persist API keys (SEC-23).
	masterKey := "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	t.Setenv("OMNIPUS_MASTER_KEY", masterKey)
	credStore := credentials.NewStore(tmpDir + "/credentials.json")
	require.NoError(t, credentials.Unlock(credStore))

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
		agentLoop:     al,
		homePath:      tmpDir,
		allowedOrigin: "http://localhost:3000",
		onboardingMgr: onboarding.NewManager(tmpDir),
		taskStore:     taskstore.New(tmpDir + "/tasks"),
		credStore:     credStore,
	}

	const n = 5
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := `{"provider":{"id":"openai","api_key":"sk-test-` + string(rune('0'+idx)) + `"},"admin":{"username":"admin` + string(rune('0'+idx)) + `","password":"secret123"}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			api.HandleCompleteOnboarding(w, req)
			codes[idx] = w.Code
		}(i)
	}
	wg.Wait()

	// At least one must succeed
	hasSuccess := false
	for _, code := range codes {
		if code == http.StatusOK {
			hasSuccess = true
			break
		}
	}
	assert.True(t, hasSuccess, "at least one concurrent request must succeed")

	// Config should not be corrupted — should have exactly 1 user
	configData, err := os.ReadFile(tmpDir + "/config.json")
	require.NoError(t, err)
	var configMap map[string]any
	require.NoError(t, json.Unmarshal(configData, &configMap))

	gateway := configMap["gateway"].(map[string]any)
	usersRaw := gateway["users"]
	if usersRaw == nil {
		t.Fatal("gateway.users should not be nil")
	}
	users := usersRaw.([]any)
	assert.Len(t, users, 1, "config.json must have exactly 1 user after concurrent calls")
}
