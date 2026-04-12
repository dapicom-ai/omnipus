// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oklog/ulid/v2"

	"github.com/dapicom-ai/omnipus/pkg/tools"
)

type project struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Color       string   `json:"color,omitempty"`
	AgentIDs    []string `json:"agent_ids,omitempty"`
	TaskCount   int      `json:"task_count"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func projectsDir(home string) string { return filepath.Join(home, "projects") }

// ---- system.project.create ----

type ProjectCreateTool struct{ deps *Deps }

func NewProjectCreateTool(d *Deps) *ProjectCreateTool { return &ProjectCreateTool{deps: d} }
func (t *ProjectCreateTool) Name() string              { return "system.project.create" }
func (t *ProjectCreateTool) Scope() tools.ToolScope    { return tools.ScopeSystem }
func (t *ProjectCreateTool) Description() string {
	return "Create a new project.\nParameters: name (required), description, color, agent_ids."
}

func (t *ProjectCreateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"color":       map[string]any{"type": "string"},
			"agent_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"name"},
	}
}

func (t *ProjectCreateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	id := ulid.Make().String()
	p := project{
		ID:        id,
		Name:      name,
		TaskCount: 0,
		CreatedAt: nowISO(),
		UpdatedAt: nowISO(),
	}
	if v, ok := args["description"].(string); ok {
		p.Description = v
	}
	if v, ok := args["color"].(string); ok {
		p.Color = v
	}
	if v, ok := args["agent_ids"].([]any); ok {
		for _, aid := range v {
			if s, ok := aid.(string); ok {
				p.AgentIDs = append(p.AgentIDs, s)
			}
		}
	}
	if err := writeEntity(projectsDir(t.deps.Home), id, p); err != nil {
		return tools.ErrorResult(errorJSON("SAVE_FAILED", err.Error(), ""))
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"id": id, "name": p.Name, "color": p.Color, "task_count": 0,
	}))
}

// ---- system.project.update ----

type ProjectUpdateTool struct{ deps *Deps }

func NewProjectUpdateTool(d *Deps) *ProjectUpdateTool { return &ProjectUpdateTool{deps: d} }
func (t *ProjectUpdateTool) Name() string              { return "system.project.update" }
func (t *ProjectUpdateTool) Scope() tools.ToolScope    { return tools.ScopeSystem }
func (t *ProjectUpdateTool) Description() string {
	return "Update an existing project.\nParameters: id (required), name, description, color, agent_ids."
}

func (t *ProjectUpdateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"color":       map[string]any{"type": "string"},
			"agent_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"id"},
	}
}

func (t *ProjectUpdateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	var p project
	if err := readEntity(projectsDir(t.deps.Home), id, &p); err != nil {
		return tools.ErrorResult(errorJSON("PROJECT_NOT_FOUND", fmt.Sprintf("No project %q", id),
			"Use system.project.list to see available projects"))
	}
	updated := []string{}
	if v, ok := args["name"].(string); ok && v != "" {
		p.Name = v
		updated = append(updated, "name")
	}
	if v, ok := args["description"].(string); ok {
		p.Description = v
		updated = append(updated, "description")
	}
	if v, ok := args["color"].(string); ok {
		p.Color = v
		updated = append(updated, "color")
	}
	p.UpdatedAt = nowISO()
	if err := writeEntity(projectsDir(t.deps.Home), id, p); err != nil {
		return tools.ErrorResult(errorJSON("SAVE_FAILED", err.Error(), ""))
	}
	return tools.NewToolResult(successJSON(map[string]any{"id": id, "updated_fields": updated}))
}

// ---- system.project.delete ----

type ProjectDeleteTool struct{ deps *Deps }

func NewProjectDeleteTool(d *Deps) *ProjectDeleteTool { return &ProjectDeleteTool{deps: d} }
func (t *ProjectDeleteTool) Name() string              { return "system.project.delete" }
func (t *ProjectDeleteTool) Scope() tools.ToolScope    { return tools.ScopeSystem }
func (t *ProjectDeleteTool) Description() string {
	return "Delete a project. Parameters: id (required), confirm (bool, must be true)."
}

func (t *ProjectDeleteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string"},
			"confirm": map[string]any{"type": "boolean"},
		},
		"required": []string{"id", "confirm"},
	}
}

func (t *ProjectDeleteTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	id, _ := args["id"].(string)
	confirm, _ := args["confirm"].(bool)
	if id == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "id is required", ""))
	}
	if !confirm {
		return tools.ErrorResult(errorJSON("CONFIRMATION_REQUIRED",
			"confirm must be true to delete a project", ""))
	}
	if err := deleteEntity(projectsDir(t.deps.Home), id); err != nil {
		return tools.ErrorResult(errorJSON("PROJECT_NOT_FOUND", err.Error(),
			"Use system.project.list to see available projects"))
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"id": id, "deleted": true, "tasks_deleted": 0,
	}))
}

// ---- system.project.list ----

type ProjectListTool struct{ deps *Deps }

func NewProjectListTool(d *Deps) *ProjectListTool { return &ProjectListTool{deps: d} }
func (t *ProjectListTool) Name() string              { return "system.project.list" }
func (t *ProjectListTool) Scope() tools.ToolScope    { return tools.ScopeSystem }
func (t *ProjectListTool) Description() string {
	return "List all projects with task counts. No parameters required."
}

func (t *ProjectListTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t *ProjectListTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	projects, err := listEntities[project](projectsDir(t.deps.Home))
	if err != nil {
		return tools.ErrorResult(errorJSON("LIST_FAILED", err.Error(), ""))
	}
	return tools.NewToolResult(successJSON(map[string]any{"projects": projects}))
}
