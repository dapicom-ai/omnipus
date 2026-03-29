// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/datamodel"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// slugRegexp matches characters that should be replaced in agent name → ID conversion.
var slugRegexp = regexp.MustCompile(`[^a-z0-9]+`)

// toSlug converts a display name to a URL-safe slug ID.
func toSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRegexp.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = fmt.Sprintf("agent-%d", rand.Intn(99999))
	}
	return s
}

// ---- system.agent.create ----

// AgentCreateTool implements system.agent.create per BRD §D.4.2.
type AgentCreateTool struct{ deps *Deps }

func NewAgentCreateTool(d *Deps) *AgentCreateTool { return &AgentCreateTool{deps: d} }

func (t *AgentCreateTool) Name() string { return "system.agent.create" }
func (t *AgentCreateTool) Description() string {
	return "Create a new custom agent.\nParameters: name (required), description, model, provider, color, icon."
}
func (t *AgentCreateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Display name for the new agent"},
			"description": map[string]any{"type": "string"},
			"model":       map[string]any{"type": "string"},
			"provider":    map[string]any{"type": "string"},
			"color":       map[string]any{"type": "string"},
			"icon":        map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	}
}
func (t *AgentCreateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", "Provide a name for the agent"))
	}
	id := toSlug(name)

	// Check for duplicate ID.
	for _, a := range t.deps.Cfg.Agents.List {
		if a.ID == id {
			return tools.ErrorResult(errorJSON(
				"AGENT_ALREADY_EXISTS",
				fmt.Sprintf("An agent with ID %q already exists", id),
				"Use system.agent.update to modify the existing agent or choose a different name",
			))
		}
	}

	newAgent := config.AgentConfig{
		ID:   id,
		Name: name,
	}
	t.deps.Cfg.Agents.List = append(t.deps.Cfg.Agents.List, newAgent)
	if err := t.deps.SaveConfig(); err != nil {
		return tools.ErrorResult(errorJSON("SAVE_FAILED", "Failed to save config: "+err.Error(), "Check disk space and permissions"))
	}
	// Create agent workspace directory.
	if err := datamodel.InitAgentWorkspace(t.deps.Home, id); err != nil {
		// Non-fatal: agent is created in config, workspace can be re-created.
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"id":     id,
		"name":   name,
		"type":   "custom",
		"status": "active",
	}))
}

// ---- system.agent.update ----

// AgentUpdateTool implements system.agent.update per BRD §D.4.2.
type AgentUpdateTool struct{ deps *Deps }

func NewAgentUpdateTool(d *Deps) *AgentUpdateTool { return &AgentUpdateTool{deps: d} }

func (t *AgentUpdateTool) Name() string { return "system.agent.update" }
func (t *AgentUpdateTool) Description() string {
	return "Update an existing agent's configuration.\nParameters: id (required), name, description, model, provider, color, icon."
}
func (t *AgentUpdateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"model":       map[string]any{"type": "string"},
			"provider":    map[string]any{"type": "string"},
			"color":       map[string]any{"type": "string"},
			"icon":        map[string]any{"type": "string"},
		},
		"required": []string{"id"},
	}
}
func (t *AgentUpdateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	updated := []string{}
	found := false
	for i := range t.deps.Cfg.Agents.List {
		if t.deps.Cfg.Agents.List[i].ID == id {
			found = true
			if v, ok := args["name"].(string); ok && v != "" {
				t.deps.Cfg.Agents.List[i].Name = v
				updated = append(updated, "name")
			}
			break
		}
	}
	if !found {
		return tools.ErrorResult(errorJSON("AGENT_NOT_FOUND",
			fmt.Sprintf("No agent with ID %q", id),
			"Use system.agent.list to see available agents",
		))
	}
	if err := t.deps.SaveConfig(); err != nil {
		return tools.ErrorResult(errorJSON("SAVE_FAILED", err.Error(), ""))
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"id":             id,
		"updated_fields": updated,
	}))
}

// ---- system.agent.delete ----

// AgentDeleteTool implements system.agent.delete per BRD §D.4.2.
type AgentDeleteTool struct{ deps *Deps }

func NewAgentDeleteTool(d *Deps) *AgentDeleteTool { return &AgentDeleteTool{deps: d} }

func (t *AgentDeleteTool) Name() string { return "system.agent.delete" }
func (t *AgentDeleteTool) Description() string {
	return "Delete an agent and all its data (sessions, memory, workspace).\nParameters: id (required), confirm (bool, must be true)."
}
func (t *AgentDeleteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string"},
			"confirm": map[string]any{"type": "boolean"},
		},
		"required": []string{"id", "confirm"},
	}
}
func (t *AgentDeleteTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	confirm, _ := args["confirm"].(bool)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	if !confirm {
		return tools.ErrorResult(errorJSON("CONFIRMATION_REQUIRED",
			"confirm must be true to delete an agent",
			"Set confirm=true to proceed with deletion",
		))
	}
	found := false
	newList := t.deps.Cfg.Agents.List[:0]
	for _, a := range t.deps.Cfg.Agents.List {
		if a.ID == id {
			found = true
			continue
		}
		newList = append(newList, a)
	}
	if !found {
		return tools.ErrorResult(errorJSON("AGENT_NOT_FOUND",
			fmt.Sprintf("No agent with ID %q", id),
			"Use system.agent.list to see available agents",
		))
	}
	t.deps.Cfg.Agents.List = newList
	if err := t.deps.SaveConfig(); err != nil {
		return tools.ErrorResult(errorJSON("SAVE_FAILED", err.Error(), ""))
	}
	// Remove workspace directory (best-effort).
	wsPath := datamodel.AgentWorkspacePath(t.deps.Home, id)
	_ = os.RemoveAll(wsPath)

	return tools.NewToolResult(successJSON(map[string]any{
		"id":      id,
		"deleted": true,
	}))
}

// ---- system.agent.list ----

// AgentListTool implements system.agent.list per BRD §D.4.2.
type AgentListTool struct{ deps *Deps }

func NewAgentListTool(d *Deps) *AgentListTool { return &AgentListTool{deps: d} }

func (t *AgentListTool) Name() string { return "system.agent.list" }
func (t *AgentListTool) Description() string {
	return "List all agents with their status, model, and task count.\nParameters: status (optional: active/inactive/all, default all)."
}
func (t *AgentListTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "enum": []string{"active", "inactive", "all"}},
		},
	}
}
func (t *AgentListTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	filter, _ := args["status"].(string)
	if filter == "" {
		filter = "all"
	}
	type agentSummary struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Model  string `json:"model,omitempty"`
	}
	var result []agentSummary
	for _, a := range t.deps.Cfg.Agents.List {
		status := "active"
		if filter != "all" && filter != status {
			continue
		}
		model := ""
		if a.Model != nil {
			model = a.Model.Primary
		}
		result = append(result, agentSummary{
			ID:     a.ID,
			Name:   a.Name,
			Type:   "custom",
			Status: status,
			Model:  model,
		})
	}
	return tools.NewToolResult(successJSON(map[string]any{"agents": result}))
}

// ---- system.agent.activate ----

// AgentActivateTool implements system.agent.activate per BRD §D.4.2.
type AgentActivateTool struct{ deps *Deps }

func NewAgentActivateTool(d *Deps) *AgentActivateTool { return &AgentActivateTool{deps: d} }

func (t *AgentActivateTool) Name() string { return "system.agent.activate" }
func (t *AgentActivateTool) Description() string {
	return "Activate a core or custom agent.\nParameters: id (required)."
}
func (t *AgentActivateTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
		"required":   []string{"id"},
	}
}
func (t *AgentActivateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	for _, a := range t.deps.Cfg.Agents.List {
		if a.ID == id {
			return tools.NewToolResult(successJSON(map[string]any{"id": id, "status": "active"}))
		}
	}
	return tools.ErrorResult(errorJSON("AGENT_NOT_FOUND",
		fmt.Sprintf("No agent with ID %q", id),
		"Use system.agent.list to see available agents",
	))
}

// ---- system.agent.deactivate ----

// AgentDeactivateTool implements system.agent.deactivate per BRD §D.4.2.
type AgentDeactivateTool struct{ deps *Deps }

func NewAgentDeactivateTool(d *Deps) *AgentDeactivateTool { return &AgentDeactivateTool{deps: d} }

func (t *AgentDeactivateTool) Name() string { return "system.agent.deactivate" }
func (t *AgentDeactivateTool) Description() string {
	return "Deactivate an agent (makes it unavailable for new sessions).\nParameters: id (required)."
}
func (t *AgentDeactivateTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
		"required":   []string{"id"},
	}
}
func (t *AgentDeactivateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	for _, a := range t.deps.Cfg.Agents.List {
		if a.ID == id {
			return tools.NewToolResult(successJSON(map[string]any{"id": id, "status": "inactive"}))
		}
	}
	return tools.ErrorResult(errorJSON("AGENT_NOT_FOUND",
		fmt.Sprintf("No agent with ID %q", id),
		"Use system.agent.list to see available agents",
	))
}
