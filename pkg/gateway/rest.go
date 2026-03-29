// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/session"
)

// restAPI holds shared dependencies for all REST endpoint handlers.
// Handlers are registered as method-dispatching http.HandlerFuncs in gateway.go.
type restAPI struct {
	cfg           *config.Config
	agentLoop     *agent.AgentLoop
	partitions    *session.PartitionStore // may be nil
	allowedOrigin string
}

// --- CORS / JSON helpers ---

func (a *restAPI) setCORSHeaders(w http.ResponseWriter, r ...* http.Request) {
	origin := a.allowedOrigin
	if origin == "" {
		origin = "*"
	}
	// When the SPA is embedded (same-origin), reflect the request origin
	// to avoid CORS issues with the public IP.
	if len(r) > 0 && r[0] != nil {
		reqOrigin := r[0].Header.Get("Origin")
		if reqOrigin != "" {
			origin = reqOrigin
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
}

func (a *restAPI) handlePreflight(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodOptions {
		a.setCORSHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
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
	if a.handlePreflight(w, r) {
		return
	}
	if !checkBearerAuth(w, r) {
		return
	}
	a.setCORSHeaders(w)

	// Extract optional session ID from path.
	// Registered at both "/api/v1/sessions" and "/api/v1/sessions/".
	path := strings.TrimSuffix(r.URL.Path, "/")
	sessionID := strings.TrimPrefix(path, "/api/v1/sessions")
	sessionID = strings.TrimPrefix(sessionID, "/")

	switch r.Method {
	case http.MethodGet:
		if sessionID == "" {
			a.listSessions(w, r)
		} else {
			a.getSession(w, r, sessionID)
		}
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *restAPI) listSessions(w http.ResponseWriter, _ *http.Request) {
	if a.partitions == nil {
		jsonOK(w, []*session.SessionMeta{})
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
		slog.Warn("rest: could not read messages", "session_id", id, "error", err)
		messages = []session.TranscriptEntry{}
	}
	jsonOK(w, map[string]any{
		"session":  meta,
		"messages": messages,
	})
}

// --- Agents ---

// HandleAgents handles /api/v1/agents (list + create) and /api/v1/agents/{id} (detail).
func (a *restAPI) HandleAgents(w http.ResponseWriter, r *http.Request) {
	if a.handlePreflight(w, r) {
		return
	}
	if !checkBearerAuth(w, r) {
		return
	}
	a.setCORSHeaders(w)

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
	default:
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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
		Name:   "Omnipus System",
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
			Name:   "Omnipus System",
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

type createAgentRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	Description string `json:"description,omitempty"`
}

func (a *restAPI) createAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" {
		jsonErr(w, http.StatusUnprocessableEntity, "name is required")
		return
	}

	// [INFERRED] Creating agents via API adds them to the in-memory config for this session.
	// Persistent storage (config file update) is deferred — this is a P2 feature.
	cfg := a.agentLoop.GetConfig()
	id := req.ID
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
	}

	var modelCfg *config.AgentModelConfig
	if req.Model != "" {
		modelCfg = &config.AgentModelConfig{Primary: req.Model}
	}

	newAgent := config.AgentConfig{
		ID:    id,
		Name:  req.Name,
		Model: modelCfg,
	}
	cfg.Agents.List = append(cfg.Agents.List, newAgent)

	model := cfg.Agents.Defaults.ModelName
	if modelCfg != nil {
		model = modelCfg.Primary
	}

	w.WriteHeader(http.StatusCreated)
	jsonOK(w, agentResponse{
		ID:          id,
		Name:        req.Name,
		Type:        "custom",
		Model:       model,
		Description: req.Description,
		Status:      "idle",
	})
}

// --- Config ---

// HandleConfig handles GET /api/v1/config and PUT /api/v1/config.
func (a *restAPI) HandleConfig(w http.ResponseWriter, r *http.Request) {
	if a.handlePreflight(w, r) {
		return
	}
	if !checkBearerAuth(w, r) {
		return
	}
	a.setCORSHeaders(w)

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
	if a.handlePreflight(w, r) {
		return
	}
	if !checkBearerAuth(w, r) {
		return
	}
	a.setCORSHeaders(w)

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
	skills, ok := info["skills"].(map[string]any)
	if !ok {
		skills = map[string]any{}
	}
	jsonOK(w, skills)
}

func (a *restAPI) searchSkills(w http.ResponseWriter, _ *http.Request) {
	// [INFERRED] ClawHub search is not yet implemented in the backend. Return stub.
	jsonOK(w, map[string]any{"results": []any{}, "message": "ClawHub search not yet available"})
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

// HandleDoctor handles GET /api/v1/doctor.
func (a *restAPI) HandleDoctor(w http.ResponseWriter, r *http.Request) {
	if a.handlePreflight(w, r) {
		return
	}
	if !checkBearerAuth(w, r) {
		return
	}
	a.setCORSHeaders(w)

	if r.Method != http.MethodGet {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := a.agentLoop.GetConfig()
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

	jsonOK(w, map[string]any{
		"status": "ok",
		"checks": checks,
	})
}
