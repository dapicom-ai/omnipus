// Omnipus — Tool Catalog
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// This file is the single source of truth for all tools available in the system.
// Both the REST API (GET /api/v1/tools/builtin) and Ava's context injection
// read from builtinCatalog (via GetBuiltinCatalog). When adding a new tool, add it here.

package tools

import (
	"fmt"
	"strings"
)

// ToolCategory groups tools by function for the UI tool picker.
type ToolCategory string

const (
	CategoryFile          ToolCategory = "file"
	CategoryCode          ToolCategory = "code"
	CategoryWeb           ToolCategory = "web"
	CategoryBrowser       ToolCategory = "browser"
	CategoryCommunication ToolCategory = "communication"
	CategoryTask          ToolCategory = "task"
	CategoryAutomation    ToolCategory = "automation"
	CategorySearch        ToolCategory = "search"
	CategorySkills        ToolCategory = "skills"
	CategoryHardware      ToolCategory = "hardware"
	CategorySystem        ToolCategory = "system"
)

// CatalogEntry describes a single tool's metadata for the UI and context injection.
type CatalogEntry struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scope       ToolScope    `json:"scope"`
	Category    ToolCategory `json:"category"`
}

// builtinCatalog is the canonical list of every tool in the system.
// Grouped by category for readability. Update this when adding new tools.
// External packages must use GetBuiltinCatalog() — the variable is unexported to
// prevent accidental mutation of the slice header by callers.
var builtinCatalog = []CatalogEntry{
	// ── File & Code ──────────────────────────────────────────────────────
	{"read_file", "Read file contents from the workspace", ScopeCore, CategoryFile},
	{"write_file", "Write or create files in the workspace", ScopeCore, CategoryFile},
	{"edit_file", "Edit existing files using find and replace", ScopeCore, CategoryFile},
	{"append_file", "Append content to an existing file", ScopeCore, CategoryFile},
	{"list_dir", "List directory contents", ScopeCore, CategoryFile},
	{"exec", "Execute shell commands", ScopeCore, CategoryCode},

	// ── Web & Search ─────────────────────────────────────────────────────
	{
		"web_search",
		"Search the web using configured search engines (Brave, Tavily, DuckDuckGo, etc.)",
		ScopeGeneral,
		CategoryWeb,
	},
	{"web_fetch", "Fetch and read web page content", ScopeGeneral, CategoryWeb},

	// ── Browser Automation ───────────────────────────────────────────────
	{"browser.navigate", "Navigate to a URL in the browser", ScopeCore, CategoryBrowser},
	{"browser.click", "Click an element on the page", ScopeCore, CategoryBrowser},
	{"browser.type", "Type text into an input element", ScopeCore, CategoryBrowser},
	{"browser.screenshot", "Take a screenshot of the page", ScopeCore, CategoryBrowser},
	{"browser.get_text", "Extract text content from the page", ScopeCore, CategoryBrowser},
	{"browser.wait", "Wait for an element or condition", ScopeCore, CategoryBrowser},
	{"browser.evaluate", "Execute JavaScript in the browser (requires explicit opt-in)", ScopeCore, CategoryBrowser},

	// ── Communication ────────────────────────────────────────────────────
	{"message", "Send messages to other agents or channels", ScopeGeneral, CategoryCommunication},
	{"send_file", "Send a file to a channel or agent", ScopeGeneral, CategoryCommunication},

	// ── Task Management ──────────────────────────────────────────────────
	{"task_create", "Create and assign tasks to agents", ScopeGeneral, CategoryTask},
	{"task_update", "Update task status (running, completed, failed)", ScopeGeneral, CategoryTask},
	{"task_list", "List tasks by role (assignee or delegator)", ScopeGeneral, CategoryTask},
	{"task_delete", "Delete a task by ID", ScopeGeneral, CategoryTask},
	{"agent_list", "List all available agents with IDs and names", ScopeGeneral, CategoryTask},

	// ── Automation ───────────────────────────────────────────────────────
	{"cron", "Schedule recurring tasks with cron expressions", ScopeCore, CategoryAutomation},
	{"spawn", "Spawn a background process", ScopeCore, CategoryAutomation},
	{"spawn_status", "Check status of spawned background processes", ScopeCore, CategoryAutomation},
	{"subagent", "Delegate work to a sub-agent with a focused task", ScopeCore, CategoryAutomation},

	// ── Search & Discovery ───────────────────────────────────────────────
	{"regex_search", "Search files using regular expressions", ScopeCore, CategorySearch},
	{"bm25_search", "Semantic search across files using BM25 ranking", ScopeCore, CategorySearch},

	// ── Skills ───────────────────────────────────────────────────────────
	{"install_skill", "Install a skill from ClawHub or local path", ScopeCore, CategorySkills},
	{"remove_skill", "Remove an installed skill", ScopeCore, CategorySkills},
	{"find_skills", "Search for available skills on ClawHub", ScopeCore, CategorySkills},

	// ── Hardware (IoT) ───────────────────────────────────────────────────
	{"i2c", "Communicate with I2C devices (Linux only)", ScopeCore, CategoryHardware},
	{"spi", "Communicate with SPI devices (Linux only)", ScopeCore, CategoryHardware},

	// ── System Tools (used by core agents, not assignable to custom agents) ──
	{
		"system.agent.create",
		"Create a new custom agent with personality and configuration",
		ScopeSystem,
		CategorySystem,
	},
	{"system.agent.update", "Update an existing agent's configuration", ScopeSystem, CategorySystem},
	{"system.agent.delete", "Delete an agent and all its data", ScopeSystem, CategorySystem},
	{"system.agent.list", "List all agents with status and model", ScopeSystem, CategorySystem},
	{"system.agent.activate", "Activate an agent", ScopeSystem, CategorySystem},
	{"system.agent.deactivate", "Deactivate an agent", ScopeSystem, CategorySystem},
	{"system.project.create", "Create a new project", ScopeSystem, CategorySystem},
	{"system.project.update", "Update a project", ScopeSystem, CategorySystem},
	{"system.project.delete", "Delete a project", ScopeSystem, CategorySystem},
	{"system.project.list", "List all projects", ScopeSystem, CategorySystem},
	{"system.task.create", "Create a task on the GTD board", ScopeSystem, CategorySystem},
	{"system.task.update", "Update a task's status or assignment", ScopeSystem, CategorySystem},
	{"system.task.delete", "Delete a task", ScopeSystem, CategorySystem},
	{"system.task.list", "List tasks with optional filters", ScopeSystem, CategorySystem},
	{"system.channel.enable", "Enable a channel", ScopeSystem, CategorySystem},
	{"system.channel.configure", "Configure a channel with credentials", ScopeSystem, CategorySystem},
	{"system.channel.disable", "Disable a channel", ScopeSystem, CategorySystem},
	{"system.channel.list", "List all channels with status", ScopeSystem, CategorySystem},
	{"system.channel.test", "Test a channel connection", ScopeSystem, CategorySystem},
	{"system.skill.install", "Install a skill from ClawHub", ScopeSystem, CategorySystem},
	{"system.skill.remove", "Remove an installed skill", ScopeSystem, CategorySystem},
	{"system.skill.search", "Search ClawHub for skills", ScopeSystem, CategorySystem},
	{"system.skill.list", "List all installed skills", ScopeSystem, CategorySystem},
	{"system.mcp.add", "Add an MCP server", ScopeSystem, CategorySystem},
	{"system.mcp.remove", "Remove an MCP server", ScopeSystem, CategorySystem},
	{"system.mcp.list", "List all MCP servers", ScopeSystem, CategorySystem},
	{"system.provider.configure", "Add or update an LLM provider", ScopeSystem, CategorySystem},
	{"system.provider.list", "List configured providers with status", ScopeSystem, CategorySystem},
	{"system.provider.test", "Test a provider connection", ScopeSystem, CategorySystem},
	{"system.models.list", "List available models from configured providers", ScopeSystem, CategorySystem},
	{"system.pin.list", "List pinned artifacts", ScopeSystem, CategorySystem},
	{"system.pin.create", "Pin a chat response", ScopeSystem, CategorySystem},
	{"system.pin.delete", "Delete a pin", ScopeSystem, CategorySystem},
	{"system.config.get", "Read a configuration value", ScopeSystem, CategorySystem},
	{"system.config.set", "Update a configuration value", ScopeSystem, CategorySystem},
	{"system.doctor.run", "Run diagnostics and health checks", ScopeSystem, CategorySystem},
	{"system.backup.create", "Create a backup of the data directory", ScopeSystem, CategorySystem},
	{"system.cost.query", "Query LLM cost data by period", ScopeSystem, CategorySystem},
	{"system.navigate", "Navigate the UI to a specific screen", ScopeSystem, CategorySystem},
}

// GetBuiltinCatalog returns a copy of the builtin catalog slice.
// Callers outside this package must use this function — the underlying variable
// is unexported to prevent accidental mutation.
func GetBuiltinCatalog() []CatalogEntry {
	c := make([]CatalogEntry, len(builtinCatalog))
	copy(c, builtinCatalog)
	return c
}

// CatalogAsMapSlice returns the catalog as []map[string]any for the REST API.
func CatalogAsMapSlice() []map[string]any {
	result := make([]map[string]any, len(builtinCatalog))
	for i, e := range builtinCatalog {
		result[i] = map[string]any{
			"name":        e.Name,
			"description": e.Description,
			"scope":       string(e.Scope),
			"category":    string(e.Category),
		}
	}
	return result
}

// CatalogMarkdown returns a categorized markdown listing of non-system tools
// for injection into agent context prompts (e.g., Ava's "Available Resources").
// System tools are excluded since they aren't assignable to custom agents.
func CatalogMarkdown() string {
	// Group by category, preserving insertion order.
	order := []ToolCategory{
		CategoryFile, CategoryCode, CategoryWeb, CategoryBrowser,
		CategoryCommunication, CategoryTask, CategoryAutomation,
		CategorySearch, CategorySkills, CategoryHardware,
	}
	labels := map[ToolCategory]string{
		CategoryFile:          "File & Code",
		CategoryCode:          "Code Execution",
		CategoryWeb:           "Web & Search",
		CategoryBrowser:       "Browser Automation",
		CategoryCommunication: "Communication",
		CategoryTask:          "Task Management",
		CategoryAutomation:    "Automation",
		CategorySearch:        "Search & Discovery",
		CategorySkills:        "Skills",
		CategoryHardware:      "Hardware (IoT)",
	}
	groups := make(map[ToolCategory][]CatalogEntry)
	for _, e := range builtinCatalog {
		if e.Scope == ScopeSystem {
			continue
		}
		groups[e.Category] = append(groups[e.Category], e)
	}

	var sb strings.Builder
	sb.WriteString("## Builtin Tools\n")
	sb.WriteString("These tools can be assigned to new agents via `tools_mode` and `tools_visible`.\n")
	sb.WriteString("When `tools_mode` is `inherit`, the agent gets all tools appropriate for its scope.\n\n")
	for _, cat := range order {
		entries := groups[cat]
		if len(entries) == 0 {
			continue
		}
		label := labels[cat]
		if label == "" {
			label = string(cat)
		}
		sb.WriteString(fmt.Sprintf("### %s\n", label))
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("- `%s` — %s\n", e.Name, e.Description))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
