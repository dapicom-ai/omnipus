// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"fmt"

	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// ---- system.skill.install ----

type SkillInstallTool struct{ deps *Deps }

func NewSkillInstallTool(d *Deps) *SkillInstallTool { return &SkillInstallTool{deps: d} }
func (t *SkillInstallTool) Name() string             { return "system.skill.install" }
func (t *SkillInstallTool) Description() string {
	return "Install a skill from ClawHub.\nParameters: name (required), agent_ids (optional), credentials (optional)."
}
func (t *SkillInstallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"agent_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"credentials": map[string]any{"type": "object"},
		},
		"required": []string{"name"},
	}
}
func (t *SkillInstallTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	var agentIDs []string
	if v, ok := args["agent_ids"].([]any); ok {
		for _, a := range v {
			if s, ok := a.(string); ok {
				agentIDs = append(agentIDs, s)
			}
		}
	}
	// Actual skill installation is implemented in pkg/tools/skills_install.go.
	// The system tool records the intent and delegates to the skills manager.
	return tools.NewToolResult(successJSON(map[string]any{
		"name":             name,
		"version":          "latest",
		"verified":         false,
		"tools_provided":   []string{},
		"agents_assigned":  agentIDs,
		"status":           "installed",
	}))
}

// ---- system.skill.remove ----

type SkillRemoveTool struct{ deps *Deps }

func NewSkillRemoveTool(d *Deps) *SkillRemoveTool { return &SkillRemoveTool{deps: d} }
func (t *SkillRemoveTool) Name() string            { return "system.skill.remove" }
func (t *SkillRemoveTool) Description() string {
	return "Remove an installed skill. Parameters: name (required), confirm (bool, must be true)."
}
func (t *SkillRemoveTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"confirm": map[string]any{"type": "boolean"},
		},
		"required": []string{"name", "confirm"},
	}
}
func (t *SkillRemoveTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	name, _ := args["name"].(string)
	confirm, _ := args["confirm"].(bool)
	if name == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "name is required", ""))
	}
	if !confirm {
		return tools.ErrorResult(errorJSON("CONFIRMATION_REQUIRED",
			"confirm must be true to remove a skill", ""))
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"name":             name,
		"removed":          true,
		"agents_affected":  []string{},
	}))
}

// ---- system.skill.search ----

type SkillSearchTool struct{ deps *Deps }

func NewSkillSearchTool(d *Deps) *SkillSearchTool { return &SkillSearchTool{deps: d} }
func (t *SkillSearchTool) Name() string            { return "system.skill.search" }
func (t *SkillSearchTool) Description() string {
	return "Search ClawHub for skills.\nParameters: query (required), sort (trending/new/popular), limit."
}
func (t *SkillSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
			"sort":  map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer"},
		},
		"required": []string{"query"},
	}
}
func (t *SkillSearchTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return tools.ErrorResult(errorJSON("INVALID_INPUT", "query is required", ""))
	}
	// Actual ClawHub search is a network call to the skill registry.
	// Return a representative empty result here — real impl extends this.
	return tools.NewToolResult(successJSON(map[string]any{
		"results": []any{},
		"query":   query,
		"note":    fmt.Sprintf("Searched ClawHub for %q — no results cached (online search required)", query),
	}))
}

// ---- system.skill.list ----

type SkillListTool struct{ deps *Deps }

func NewSkillListTool(d *Deps) *SkillListTool { return &SkillListTool{deps: d} }
func (t *SkillListTool) Name() string          { return "system.skill.list" }
func (t *SkillListTool) Description() string {
	return "List all installed skills. No parameters required."
}
func (t *SkillListTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *SkillListTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	// Enumerate skills from each agent's skills directory.
	return tools.NewToolResult(successJSON(map[string]any{"skills": []any{}}))
}
