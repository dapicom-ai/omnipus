// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/fileutil"
	"github.com/dapicom-ai/omnipus/pkg/onboarding"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// Version is set at build time via -ldflags "-X github.com/dapicom-ai/omnipus/pkg/gateway.Version=x.y.z".
var Version = "dev"

// restAPI holds shared dependencies for all REST endpoint handlers.
// Handlers are registered as method-dispatching http.HandlerFuncs in gateway.go.
// Note: do NOT cache *config.Config here — use a.agentLoop.GetConfig() for
// the current config, since config can hot-reload.
type restAPI struct {
	agentLoop      *agent.AgentLoop
	partitions     *session.PartitionStore  // may be nil
	allowedOrigin  string
	onboardingMgr  *onboarding.Manager     // manages first-launch + doctor state
	homePath       string                  // ~/.omnipus — root of the data directory
}

// --- CORS / JSON helpers ---

func (a *restAPI) setCORSHeaders(w http.ResponseWriter, r ...*http.Request) {
	origin := a.allowedOrigin
	if origin == "" {
		origin = "*"
	}
	// Allow same-origin requests: if the request Origin matches the Host header,
	// reflect it so the SPA works when accessed via public IP.
	// Only reflect origins that are same-origin or localhost — never arbitrary origins.
	if len(r) > 0 && r[0] != nil {
		reqOrigin := r[0].Header.Get("Origin")
		if reqOrigin != "" && isAllowedOrigin(reqOrigin, r[0].Host, a.allowedOrigin) {
			origin = reqOrigin
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
}

// isAllowedOrigin checks whether a request origin should be reflected in CORS headers.
// Allows: the configured origin, same-origin (host match), and localhost/127.0.0.1.
func isAllowedOrigin(reqOrigin, host, configuredOrigin string) bool {
	if configuredOrigin != "" && reqOrigin == configuredOrigin {
		return true
	}
	parsed, err := url.Parse(reqOrigin)
	if err != nil {
		return false
	}
	hostname := parsed.Hostname()
	originPort := parsed.Port()
	// Same-origin: request Origin hostname AND port must match the Host header.
	if host != "" {
		hostOnly := host
		hostPort := ""
		if h, p, err := net.SplitHostPort(host); err == nil {
			hostOnly = h
			hostPort = p
		}
		if hostname == hostOnly && originPort == hostPort {
			return true
		}
	}
	// Allow localhost and loopback for development.
	return hostname == "localhost" || hostname == "127.0.0.1"
}

func (a *restAPI) handlePreflight(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodOptions {
		a.setCORSHeaders(w, r)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// withAuth wraps a handler with preflight, bearer auth, and CORS header boilerplate.
func (a *restAPI) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.handlePreflight(w, r) {
			return
		}
		if !checkBearerAuth(w, r) {
			return
		}
		a.setCORSHeaders(w, r)
		handler(w, r)
	}
}

func jsonOK(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("rest: json encode failed", "error", err)
	}
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Debug("rest: write error response failed", "error", err)
	}
}

// --- Sessions ---

// HandleSessions handles GET /api/v1/sessions (list) and GET /api/v1/sessions/{id} (detail).
func (a *restAPI) HandleSessions(w http.ResponseWriter, r *http.Request) {
	// Extract optional session ID and sub-path from the URL.
	// Supports: /api/v1/sessions, /api/v1/sessions/{id}, /api/v1/sessions/{id}/messages
	path := strings.TrimSuffix(r.URL.Path, "/")
	remainder := strings.TrimPrefix(path, "/api/v1/sessions")
	remainder = strings.TrimPrefix(remainder, "/")

	var sessionID, subPath string
	if remainder != "" {
		parts := strings.SplitN(remainder, "/", 2)
		sessionID = parts[0]
		if len(parts) > 1 {
			subPath = parts[1]
		}
	}

	if sessionID != "" {
		if err := validateEntityID(sessionID); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid session ID")
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		if sessionID == "" {
			a.listSessions(w, r)
		} else if subPath == "messages" {
			a.getSessionMessages(w, r, sessionID)
		} else {
			a.getSession(w, r, sessionID)
		}
	case http.MethodPost:
		if sessionID == "" {
			a.createSessionHTTP(w, r)
		} else {
			jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) listSessions(w http.ResponseWriter, _ *http.Request) {
	if a.partitions == nil {
		jsonErr(w, http.StatusServiceUnavailable, "session store unavailable")
		return
	}
	metas, err := a.partitions.ListSessions()
	if err != nil {
		slog.Error("rest: list sessions", "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not list sessions: %v", err))
		return
	}
	if metas == nil {
		metas = []*session.SessionMeta{}
	}
	jsonOK(w, metas)
}

func (a *restAPI) getSession(w http.ResponseWriter, _ *http.Request, id string) {
	if a.partitions == nil {
		jsonErr(w, http.StatusServiceUnavailable, "session store not available")
		return
	}
	meta, err := a.partitions.GetMeta(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, fmt.Sprintf("session not found: %v", err))
		return
	}
	messages, err := a.partitions.ReadMessages(id)
	if err != nil {
		slog.Error("rest: could not read messages", "session_id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not read messages: %v", err))
		return
	}
	jsonOK(w, sessionDetailResponse{Session: meta, Messages: messages})
}

func (a *restAPI) getSessionMessages(w http.ResponseWriter, _ *http.Request, id string) {
	if a.partitions == nil {
		jsonErr(w, http.StatusServiceUnavailable, "session store unavailable")
		return
	}
	messages, err := a.partitions.ReadMessages(id)
	if err != nil {
		slog.Error("rest: could not read messages", "session_id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not read messages: %v", err))
		return
	}
	jsonOK(w, messages)
}

func (a *restAPI) createSessionHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if a.partitions == nil {
		jsonErr(w, http.StatusServiceUnavailable, "session store not available")
		return
	}
	meta, err := a.partitions.NewSession("webchat", req.AgentID, "")
	if err != nil {
		slog.Error("rest: create session", "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not create session: %v", err))
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, meta)
}

// --- Agents ---

// HandleAgents handles /api/v1/agents (list + create) and /api/v1/agents/{id} (detail).
func (a *restAPI) HandleAgents(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	agentID := strings.TrimPrefix(path, "/api/v1/agents")
	agentID = strings.TrimPrefix(agentID, "/")

	switch r.Method {
	case http.MethodGet:
		if agentID == "" {
			a.listAgents(w)
		} else {
			a.getAgent(w, agentID)
		}
	case http.MethodPost:
		if agentID == "" {
			a.createAgent(w, r)
		} else {
			jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	case http.MethodPut:
		if agentID != "" {
			jsonErr(w, http.StatusNotImplemented, "Agent update not yet available via API — edit config.json and restart")
		} else {
			jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// skillResponse is the JSON shape returned for a single installed skill.
// Matches the frontend Skill interface.
type skillResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Verified    bool   `json:"verified"`
	Status      string `json:"status"` // "active" | "disabled"
}

// sessionDetailResponse is the JSON shape returned by GET /api/v1/sessions/{id}.
type sessionDetailResponse struct {
	Session  *session.SessionMeta      `json:"session"`
	Messages []session.TranscriptEntry `json:"messages"`
}

// gatewayStatusResponse is the JSON shape returned by GET /api/v1/status.
// Matches the frontend GatewayStatus type.
type gatewayStatusResponse struct {
	Online       bool    `json:"online"`
	AgentCount   int     `json:"agent_count"`
	ChannelCount int     `json:"channel_count"`
	DailyCost    float64 `json:"daily_cost"`
	Version      string  `json:"version"`
}

// providerResponse is the JSON shape returned for a single LLM provider.
type providerResponse struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"` // "connected" | "disconnected"
	Models []string `json:"models"`
}

// agentResponse is the JSON shape returned for a single agent.
type agentResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // "system" | "core" | "custom"
	Model       string `json:"model,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"` // "active" | "idle"
}

func (a *restAPI) listAgents(w http.ResponseWriter) {
	cfg := a.agentLoop.GetConfig()
	agents := make([]agentResponse, 0, len(cfg.Agents.List)+1)

	// System agent is always present.
	defaultModel := cfg.Agents.Defaults.ModelName
	agents = append(agents, agentResponse{
		ID:     "omnipus-system",
		Name:   "Omio",
		Type:   "system",
		Model:  defaultModel,
		Status: "active",
	})

	for _, ac := range cfg.Agents.List {
		model := defaultModel
		if ac.Model != nil && ac.Model.Primary != "" {
			model = ac.Model.Primary
		}
		agents = append(agents, agentResponse{
			ID:     ac.ID,
			Name:   ac.Name,
			Type:   "custom",
			Model:  model,
			Status: "idle",
		})
	}

	jsonOK(w, agents)
}

func (a *restAPI) getAgent(w http.ResponseWriter, id string) {
	cfg := a.agentLoop.GetConfig()

	if id == "omnipus-system" {
		jsonOK(w, agentResponse{
			ID:     "omnipus-system",
			Name:   "Omio",
			Type:   "system",
			Model:  cfg.Agents.Defaults.ModelName,
			Status: "active",
		})
		return
	}

	for _, ac := range cfg.Agents.List {
		if ac.ID == id {
			model := cfg.Agents.Defaults.ModelName
			if ac.Model != nil && ac.Model.Primary != "" {
				model = ac.Model.Primary
			}
			jsonOK(w, agentResponse{
				ID:     ac.ID,
				Name:   ac.Name,
				Type:   "custom",
				Model:  model,
				Status: "idle",
			})
			return
		}
	}

	jsonErr(w, http.StatusNotFound, fmt.Sprintf("agent %q not found", id))
}

func (a *restAPI) createAgent(w http.ResponseWriter, _ *http.Request) {
	jsonErr(w, http.StatusNotImplemented, "agent creation via API not yet persisted — add agents to config.json and restart")
}

// --- Config ---

// HandleConfig handles GET /api/v1/config and PUT /api/v1/config.
func (a *restAPI) HandleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getConfig(w)
	case http.MethodPut:
		a.updateConfig(w, r)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) getConfig(w http.ResponseWriter) {
	cfg := a.agentLoop.GetConfig()

	// Marshal to JSON then unmarshal to a generic map so we can redact credential fields.
	raw, err := json.Marshal(cfg)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not serialize config")
		return
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not process config")
		return
	}

	// Redact any top-level field names that look like credentials.
	redactSensitiveFields(m)

	jsonOK(w, m)
}

// redactSensitiveFields recursively redacts map values whose keys contain
// sensitive keywords. Credential data must live in credentials.json, not config.json,
// but this is a defense-in-depth measure per BRD SEC-23.
func redactSensitiveFields(m map[string]any) {
	sensitive := []string{"key", "token", "secret", "password", "credential", "api_key"}
	for k, v := range m {
		kl := strings.ToLower(k)
		for _, s := range sensitive {
			if strings.Contains(kl, s) {
				if str, ok := v.(string); ok && str != "" {
					m[k] = "[redacted]"
				}
				break
			}
		}
		if sub, ok := v.(map[string]any); ok {
			redactSensitiveFields(sub)
		}
		if arr, ok := v.([]any); ok {
			for _, elem := range arr {
				if subMap, ok := elem.(map[string]any); ok {
					redactSensitiveFields(subMap)
				}
			}
		}
	}
}

func (a *restAPI) updateConfig(w http.ResponseWriter, _ *http.Request) {
	// Config persistence is not implemented in Wave 5a.
	// Return 501 rather than silently pretending the update succeeded.
	jsonErr(w, http.StatusNotImplemented, "config update not yet implemented; restart with an updated config.json file")
}

// --- Skills ---

// HandleSkills handles GET /api/v1/skills and POST sub-paths (search, install).
func (a *restAPI) HandleSkills(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	sub := strings.TrimPrefix(path, "/api/v1/skills")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case r.Method == http.MethodGet && sub == "":
		a.listSkills(w)
	case r.Method == http.MethodPost && sub == "search":
		a.searchSkills(w, r)
	case r.Method == http.MethodPost && sub == "install":
		a.installSkill(w, r)
	case r.Method == http.MethodDelete && sub != "":
		a.deleteSkill(w, sub)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) listSkills(w http.ResponseWriter) {
	info := a.agentLoop.GetStartupInfo()
	skillsMap, ok := info["skills"].(map[string]any)
	if !ok || len(skillsMap) == 0 {
		jsonOK(w, []skillResponse{})
		return
	}
	// Convert map to typed array matching the frontend Skill interface.
	skills := make([]skillResponse, 0, len(skillsMap))
	for id, v := range skillsMap {
		sr := skillResponse{
			ID:      id,
			Name:    id,
			Version: "0.0.0",
			Status:  "active",
		}
		if m, ok := v.(map[string]any); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				sr.Name = name
			}
			if version, ok := m["version"].(string); ok && version != "" {
				sr.Version = version
			}
			if desc, ok := m["description"].(string); ok {
				sr.Description = desc
			}
			if author, ok := m["author"].(string); ok {
				sr.Author = author
			}
			if verified, ok := m["verified"].(bool); ok {
				sr.Verified = verified
			}
			if status, ok := m["status"].(string); ok && status != "" {
				sr.Status = status
			}
		}
		skills = append(skills, sr)
	}
	jsonOK(w, skills)
}

func (a *restAPI) searchSkills(w http.ResponseWriter, _ *http.Request) {
	jsonErr(w, http.StatusNotImplemented, "ClawHub search not yet implemented")
}

func (a *restAPI) installSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonErr(w, http.StatusBadRequest, "name is required")
		return
	}
	jsonErr(w, http.StatusNotImplemented, "skill installation not yet available")
}

func (a *restAPI) deleteSkill(w http.ResponseWriter, name string) {
	jsonErr(w, http.StatusNotImplemented, fmt.Sprintf("skill deletion not yet available for %q", name))
}

// --- Doctor / Diagnostics ---

// HandleDoctor handles GET/POST /api/v1/doctor.
func (a *restAPI) HandleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := a.agentLoop.GetConfig()

	// Run real diagnostic checks and compute a score.
	issues := a.runDiagnosticChecks(cfg)
	score := 100
	for _, iss := range issues {
		sev, _ := iss["severity"].(string)
		switch sev {
		case "high":
			score -= 20
		case "medium":
			score -= 10
		case "low":
			score -= 5
		}
	}
	if score < 0 {
		score = 0
	}

	// Persist the doctor run result.
	if a.onboardingMgr != nil {
		if err := a.onboardingMgr.RecordDoctorRun(score); err != nil {
			slog.Warn("rest: could not persist doctor run", "error", err)
		}
	}

	result := map[string]any{
		"score":      score,
		"issues":     issues,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	}

	if r.Method == http.MethodGet {
		info := a.agentLoop.GetStartupInfo()
		checks := map[string]any{
			"gateway": map[string]any{
				"status":  "ok",
				"address": fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port),
			},
			"agent_loop": map[string]any{
				"status": "ok",
				"info":   info,
			},
			"session_store": map[string]any{
				"status":    func() string { if a.partitions != nil { return "ok" }; return "unavailable" }(),
				"available": a.partitions != nil,
			},
			"go_runtime": map[string]any{
				"version":    runtime.Version(),
				"goroutines": runtime.NumGoroutine(),
				"os":         runtime.GOOS,
				"arch":       runtime.GOARCH,
			},
		}
		result["status"] = "ok"
		result["checks"] = checks
	}

	jsonOK(w, result)
}

// runDiagnosticChecks performs real diagnostic checks and returns issues found.
func (a *restAPI) runDiagnosticChecks(cfg *config.Config) []map[string]any {
	var issues []map[string]any

	// Check if a default model is configured.
	if len(cfg.ModelList) == 0 {
		issues = append(issues, map[string]any{
			"id":             "no-models",
			"severity":       "high",
			"title":          "No LLM models configured",
			"description":    "No models are configured in model_list. The agent cannot generate responses without at least one model.",
			"recommendation": "Add at least one model to config.json model_list with a valid API key in credentials.json.",
		})
	}

	// Check session store availability.
	if a.partitions == nil {
		issues = append(issues, map[string]any{
			"id":             "no-session-store",
			"severity":       "medium",
			"title":          "Session store unavailable",
			"description":    "The day-partitioned session store failed to initialize. Conversations will not be saved.",
			"recommendation": "Check file permissions on the ~/.omnipus/ directory.",
		})
	}

	// Check if any agents are configured.
	if len(cfg.Agents.List) == 0 {
		issues = append(issues, map[string]any{
			"id":             "no-custom-agents",
			"severity":       "low",
			"title":          "No custom agents configured",
			"description":    "Only the system agent is available. Custom agents can be defined in config.json.",
			"recommendation": "Add agent configurations to personalize your assistant.",
		})
	}

	// Check sandbox configuration.
	if !cfg.Sandbox.Enabled {
		issues = append(issues, map[string]any{
			"id":             "sandbox-disabled",
			"severity":       "medium",
			"title":          "Sandbox is disabled",
			"description":    "Filesystem and process sandboxing is not enabled. Agent tool executions run without confinement.",
			"recommendation": "Enable sandbox in config.json for production use.",
		})
	}

	return issues
}

// registerAdditionalEndpoints registers handlers for endpoints the frontend calls.
// Each returns a valid JSON response matching the shape the frontend expects,
// preventing "Unexpected token '<'" errors from the SPA catch-all.
func (a *restAPI) registerAdditionalEndpoints(cm httpHandlerRegistrar) {
	cm.RegisterHTTPHandler("/api/v1/state", a.withAuth(a.HandleState))
	cm.RegisterHTTPHandler("/api/v1/status", a.withAuth(a.HandleStatus))
	cm.RegisterHTTPHandler("/api/v1/tasks", a.withAuth(a.HandleTasks))
	cm.RegisterHTTPHandler("/api/v1/tasks/", a.withAuth(a.HandleTasks))
	cm.RegisterHTTPHandler("/api/v1/providers", a.withAuth(a.HandleProviders))
	cm.RegisterHTTPHandler("/api/v1/providers/", a.withAuth(a.HandleProviders))
	cm.RegisterHTTPHandler("/api/v1/mcp-servers", a.withAuth(a.HandleMCPServers))
	cm.RegisterHTTPHandler("/api/v1/storage/stats", a.withAuth(a.HandleStorageStats))
	cm.RegisterHTTPHandler("/api/v1/tools", a.withAuth(a.HandleTools))
	cm.RegisterHTTPHandler("/api/v1/channels", a.withAuth(a.HandleChannels))
}

// httpHandlerRegistrar is the subset of channels.Manager used for route registration.
type httpHandlerRegistrar interface {
	RegisterHTTPHandler(pattern string, handler http.Handler)
}

// --- App State ---

// HandleState handles GET/PATCH /api/v1/state (onboarding state).
func (a *restAPI) HandleState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		complete := true
		var lastRun *time.Time
		var lastScore *int
		if a.onboardingMgr != nil {
			complete = a.onboardingMgr.IsOnboardingComplete()
			lastRun = a.onboardingMgr.LastDoctorRun()
			lastScore = a.onboardingMgr.LastDoctorScore()
		}
		resp := map[string]any{
			"onboarding_complete": complete,
		}
		if lastRun != nil {
			resp["last_doctor_run"] = lastRun.Format(time.RFC3339)
		}
		if lastScore != nil {
			resp["last_doctor_score"] = *lastScore
		}
		jsonOK(w, resp)
	case http.MethodPatch:
		var body struct {
			OnboardingComplete *bool `json:"onboarding_complete"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.OnboardingComplete == nil || !*body.OnboardingComplete {
			jsonErr(w, http.StatusBadRequest, "onboarding_complete must be true")
			return
		}
		if a.onboardingMgr != nil {
			if err := a.onboardingMgr.CompleteOnboarding(); err != nil {
				slog.Error("rest: could not persist onboarding completion", "error", err)
				jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not save onboarding state: %v", err))
				return
			}
		}
		jsonOK(w, map[string]any{
			"onboarding_complete": true,
		})
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- Gateway Status ---

// HandleStatus handles GET /api/v1/status (polled by StatusBar every 15s).
func (a *restAPI) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := a.agentLoop.GetConfig()
	jsonOK(w, gatewayStatusResponse{
		Online:       true,
		AgentCount:   len(cfg.Agents.List) + 1, // +1 for system agent
		ChannelCount: countEnabledChannels(cfg) + 1, // +1 for webchat (always available)
		DailyCost:    0,
		Version:      Version,
	})
}

// --- Tasks ---

// taskEntity is the persistent shape for a task stored in ~/.omnipus/tasks/{id}.json.
type taskEntity struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	AgentID     string    `json:"agent_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// tasksDir returns the path to the tasks directory.
func (a *restAPI) tasksDir() string {
	return filepath.Join(a.homePath, "tasks")
}

// HandleTasks handles GET/POST /api/v1/tasks and PUT /api/v1/tasks/{id}.
func (a *restAPI) HandleTasks(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	taskID := strings.TrimPrefix(path, "/api/v1/tasks")
	taskID = strings.TrimPrefix(taskID, "/")

	switch r.Method {
	case http.MethodGet:
		if taskID == "" {
			a.listTasks(w)
		} else {
			a.getTask(w, taskID)
		}
	case http.MethodPost:
		if taskID != "" {
			jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		a.createTask(w, r)
	case http.MethodPut:
		if taskID == "" {
			jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		a.updateTask(w, r, taskID)
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) listTasks(w http.ResponseWriter) {
	dir := a.tasksDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			jsonOK(w, []taskEntity{})
			return
		}
		slog.Error("rest: list tasks", "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not list tasks: %v", err))
		return
	}
	tasks := make([]taskEntity, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			slog.Warn("rest: read task file", "file", e.Name(), "error", err)
			continue
		}
		var t taskEntity
		if err := json.Unmarshal(data, &t); err != nil {
			slog.Warn("rest: parse task file", "file", e.Name(), "error", err)
			continue
		}
		tasks = append(tasks, t)
	}
	jsonOK(w, tasks)
}

// validateEntityID rejects IDs that contain path separators, "..", or null bytes
// to prevent path traversal attacks.
func validateEntityID(id string) error {
	if id == "" {
		return fmt.Errorf("id must not be empty")
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") || strings.ContainsRune(id, 0) {
		return fmt.Errorf("invalid id")
	}
	return nil
}

func (a *restAPI) getTask(w http.ResponseWriter, id string) {
	if err := validateEntityID(id); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task ID")
		return
	}
	data, err := os.ReadFile(filepath.Join(a.tasksDir(), id+".json"))
	if err != nil {
		if os.IsNotExist(err) {
			jsonErr(w, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
			return
		}
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not read task: %v", err))
		return
	}
	var t taskEntity
	if err := json.Unmarshal(data, &t); err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not parse task")
		return
	}
	jsonOK(w, t)
}

func (a *restAPI) createTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
		AgentID     string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	if req.Status == "" {
		req.Status = "inbox"
	}
	now := time.Now().UTC()
	t := taskEntity{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
		AgentID:     req.AgentID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not serialize task")
		return
	}
	if err := fileutil.WriteFileAtomic(filepath.Join(a.tasksDir(), t.ID+".json"), data, 0o600); err != nil {
		slog.Error("rest: write task", "id", t.ID, "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not save task: %v", err))
		return
	}
	w.WriteHeader(http.StatusCreated)
	jsonOK(w, t)
}

func (a *restAPI) updateTask(w http.ResponseWriter, r *http.Request, id string) {
	if err := validateEntityID(id); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task ID")
		return
	}
	taskPath := filepath.Join(a.tasksDir(), id+".json")
	existing, err := os.ReadFile(taskPath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonErr(w, http.StatusNotFound, fmt.Sprintf("task %q not found", id))
			return
		}
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not read task: %v", err))
		return
	}
	var t taskEntity
	if err := json.Unmarshal(existing, &t); err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not parse task")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		AgentID     *string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	if req.Status != nil {
		t.Status = *req.Status
	}
	if req.AgentID != nil {
		t.AgentID = *req.AgentID
	}
	t.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "could not serialize task")
		return
	}
	if err := fileutil.WriteFileAtomic(taskPath, data, 0o600); err != nil {
		slog.Error("rest: update task", "id", id, "error", err)
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("could not save task: %v", err))
		return
	}
	jsonOK(w, t)
}

// --- Providers ---

// HandleProviders handles GET/PUT/POST /api/v1/providers and sub-paths.
func (a *restAPI) HandleProviders(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	sub := strings.TrimPrefix(path, "/api/v1/providers")
	sub = strings.TrimPrefix(sub, "/")

	switch {
	case r.Method == http.MethodGet && sub == "":
		// Return provider list derived from config model_list, accumulating all models per provider.
		cfg := a.agentLoop.GetConfig()
		providerModels := make(map[string][]string)
		providerOrder := make([]string, 0)
		for _, m := range cfg.ModelList {
			providerName := "default"
			if parts := strings.SplitN(m.Model, "/", 2); len(parts) == 2 {
				providerName = parts[0]
			}
			if _, exists := providerModels[providerName]; !exists {
				providerOrder = append(providerOrder, providerName)
			}
			providerModels[providerName] = append(providerModels[providerName], m.ModelName)
		}
		providers := make([]providerResponse, 0, len(providerOrder))
		for _, name := range providerOrder {
			providers = append(providers, providerResponse{
				ID:     name,
				Name:   name,
				Status: "connected",
				Models: providerModels[name],
			})
		}
		if len(providers) == 0 {
			providers = append(providers, providerResponse{
				ID:     "default",
				Name:   "Default",
				Status: "disconnected",
				Models: []string{},
			})
		}
		jsonOK(w, providers)

	case r.Method == http.MethodPut && sub != "":
		// Provider configuration — strip /test suffix if present.
		jsonErr(w, http.StatusNotImplemented, "Provider configuration not yet available via API — add API key to credentials.json or set PROVIDER_API_KEY environment variable")

	case r.Method == http.MethodPost && strings.HasSuffix(sub, "/test"):
		// Provider connectivity test — not yet implemented.
		jsonErr(w, http.StatusNotImplemented, "provider connectivity testing not yet implemented — verify your API key manually")

	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- MCP Servers ---

// HandleMCPServers handles GET /api/v1/mcp-servers.
func (a *restAPI) HandleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	info := a.agentLoop.GetStartupInfo()
	if mcpInfo, ok := info["mcp"]; ok {
		jsonOK(w, mcpInfo)
		return
	}
	jsonOK(w, []any{})
}

// --- Tools ---

// HandleTools handles GET /api/v1/tools — returns the list of tools available to the agent.
func (a *restAPI) HandleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	info := a.agentLoop.GetStartupInfo()
	toolsMap, _ := info["tools"].(map[string]any)
	names, _ := toolsMap["names"].([]string)
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		// Infer category from the tool name prefix (e.g. "system.read_file" → "system").
		category := "general"
		if idx := strings.Index(name, "."); idx > 0 {
			category = name[:idx]
		}
		tools = append(tools, map[string]any{
			"name":        name,
			"category":    category,
			"description": "",
		})
	}
	jsonOK(w, tools)
}

// --- Channels ---

// HandleChannels handles GET /api/v1/channels — returns configured channel status.
func (a *restAPI) HandleChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cfg := a.agentLoop.GetConfig()
	ch := cfg.Channels
	type channelEntry struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Transport   string `json:"transport"`
		Enabled     bool   `json:"enabled"`
		Description string `json:"description"`
	}
	channels := []channelEntry{
		{ID: "webchat", Name: "Web Chat", Transport: "websocket", Enabled: true, Description: "Built-in browser chat"},
		{ID: "telegram", Name: "Telegram", Transport: "webhook", Enabled: ch.Telegram.Enabled, Description: "Telegram Bot API"},
		{ID: "discord", Name: "Discord", Transport: "websocket", Enabled: ch.Discord.Enabled, Description: "Discord Gateway"},
		{ID: "slack", Name: "Slack", Transport: "websocket", Enabled: ch.Slack.Enabled, Description: "Slack Socket Mode"},
		{ID: "whatsapp", Name: "WhatsApp", Transport: "bridge", Enabled: ch.WhatsApp.Enabled, Description: "WhatsApp via bridge or native"},
		{ID: "feishu", Name: "Feishu / Lark", Transport: "webhook", Enabled: ch.Feishu.Enabled, Description: "Feishu (Lark) Bot"},
		{ID: "dingtalk", Name: "DingTalk", Transport: "webhook", Enabled: ch.DingTalk.Enabled, Description: "DingTalk Bot"},
		{ID: "wecom", Name: "WeCom", Transport: "webhook", Enabled: ch.WeCom.Enabled, Description: "WeCom (WeChat Work) Bot"},
		{ID: "weixin", Name: "Weixin", Transport: "webhook", Enabled: ch.Weixin.Enabled, Description: "Weixin (WeChat) Official Account"},
		{ID: "line", Name: "LINE", Transport: "webhook", Enabled: ch.LINE.Enabled, Description: "LINE Messaging API"},
		{ID: "qq", Name: "QQ", Transport: "websocket", Enabled: ch.QQ.Enabled, Description: "QQ via napcat"},
		{ID: "onebot", Name: "OneBot", Transport: "websocket", Enabled: ch.OneBot.Enabled, Description: "OneBot v11 protocol"},
		{ID: "irc", Name: "IRC", Transport: "tcp", Enabled: ch.IRC.Enabled, Description: "Internet Relay Chat"},
		{ID: "matrix", Name: "Matrix", Transport: "http", Enabled: ch.Matrix.Enabled, Description: "Matrix protocol"},
		{ID: "pico", Name: "PicoClaw", Transport: "http", Enabled: ch.Pico.Enabled, Description: "PicoClaw bridge channel"},
		{ID: "maixcam", Name: "MaixCam", Transport: "serial", Enabled: ch.MaixCam.Enabled, Description: "MaixCam edge device"},
	}
	jsonOK(w, channels)
}

// countEnabledChannels returns the number of non-webchat channels currently enabled in cfg.
func countEnabledChannels(cfg *config.Config) int {
	ch := cfg.Channels
	count := 0
	for _, enabled := range []bool{
		ch.Telegram.Enabled,
		ch.Discord.Enabled,
		ch.Slack.Enabled,
		ch.WhatsApp.Enabled,
		ch.Feishu.Enabled,
		ch.DingTalk.Enabled,
		ch.WeCom.Enabled,
		ch.Weixin.Enabled,
		ch.LINE.Enabled,
		ch.QQ.Enabled,
		ch.OneBot.Enabled,
		ch.IRC.Enabled,
		ch.Matrix.Enabled,
		ch.Pico.Enabled,
		ch.MaixCam.Enabled,
	} {
		if enabled {
			count++
		}
	}
	return count
}

// --- Storage Stats ---

// HandleStorageStats handles GET /api/v1/storage/stats.
func (a *restAPI) HandleStorageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var sessionCount int
	var workspaceSize int64
	if a.partitions != nil {
		if metas, err := a.partitions.ListSessions(); err == nil {
			sessionCount = len(metas)
		}
	}
	// Walk the home directory for workspace size.
	homeDir := a.homePath
	_ = filepath.Walk(homeDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		workspaceSize += info.Size()
		return nil
	})

	jsonOK(w, map[string]any{
		"workspace_size_bytes": workspaceSize,
		"session_count":        sessionCount,
		"memory_entry_count":   0,
	})
}
