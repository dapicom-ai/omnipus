// Omnipus — Central MCP Tool Registry
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package tools — MCPRegistry is the central registry for tools provided by
// connected MCP servers (FR-001). Unlike the BuiltinRegistry, the MCPRegistry
// is dynamic: entries are added when MCP servers connect and removed when they
// disconnect. It is populated only after the BuiltinRegistry is fully populated
// (Boot Order step 7 per the tool-registry-redesign spec).
//
// Design invariants (binding):
//   - First-server-wins for collisions between MCP servers (FR-034). The second
//     registration of the same name from a different server is rejected and an
//     audit/warn path is triggered.
//   - Tools whose name begins with "system." are rejected (FR-060) — the
//     BuiltinRegistry.ValidateMCPName check is applied before admission.
//   - When a server disconnects, ALL entries for that server are evicted atomically
//     under a single lock acquisition (no torn state).
//   - On server rename (detected by caller via FR-083), the caller must evict the
//     old server then add the new one — the MCPRegistry is rename-agnostic; it
//     stores by serverID and tool name only.
//   - The registry is safe for concurrent reads and writes (RWMutex). Per-LLM-call
//     filter snapshots via All() observe a consistent state.

package tools

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// MCPEntry describes a single MCP tool as returned by Describe.
type MCPEntry struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scope       ToolScope    `json:"scope"`
	Category    ToolCategory `json:"category"`
	Source      string       `json:"source"` // "mcp:<serverID>"
	ServerID    string       `json:"server_id"`
}

// mcpToolRecord holds a registered MCP tool plus its owning server.
type mcpToolRecord struct {
	serverID string
	tool     Tool
}

// MCPRegistry is the dynamic central registry for MCP server tools.
// There is one per process. Entries are added and removed at runtime
// as servers connect and disconnect.
type MCPRegistry struct {
	mu      sync.RWMutex
	byName  map[string]*mcpToolRecord // name → record (first-server-wins)
	byServer map[string][]string      // serverID → []tool names (for fast eviction)
}

// NewMCPRegistry creates an empty MCPRegistry.
func NewMCPRegistry() *MCPRegistry {
	return &MCPRegistry{
		byName:   make(map[string]*mcpToolRecord),
		byServer: make(map[string][]string),
	}
}

// RegisterServerTools registers all tools from a single MCP server.
// Rules (FR-034, FR-060):
//   - Tools whose name begins with "system." are rejected unconditionally.
//   - If a tool name is already registered by a different server, the new
//     entry is rejected (first-server-wins) and a warning is emitted.
//   - If the tool name is already registered by the SAME server, it is
//     replaced (reconnect case, idempotent).
//
// Returns a slice of (name, reason) pairs for each rejected registration.
// The caller should emit audit events for each rejected entry.
func (r *MCPRegistry) RegisterServerTools(serverID string, tools []Tool, builtins *BuiltinRegistry) []MCPCollision {
	r.mu.Lock()
	defer r.mu.Unlock()

	var collisions []MCPCollision
	var accepted []string

	for _, t := range tools {
		name := t.Name()

		// FR-060: reject system.* prefix unconditionally.
		if builtins != nil {
			if err := builtins.ValidateMCPName(name); err != nil {
				collisions = append(collisions, MCPCollision{
					ToolName:     name,
					ServerID:     serverID,
					ConflictWith: "reserved_prefix",
					Reason:       err.Error(),
				})
				slog.Warn("tools.MCPRegistry: MCP tool rejected (reserved prefix)", "tool", name, "server", serverID)
				continue
			}
		}

		// FR-034: first-server-wins on name collision.
		if existing, exists := r.byName[name]; exists && existing.serverID != serverID {
			collisions = append(collisions, MCPCollision{
				ToolName:     name,
				ServerID:     serverID,
				ConflictWith: existing.serverID,
				Reason:       fmt.Sprintf("already registered by server %q", existing.serverID),
			})
			slog.Warn("tools.MCPRegistry: MCP tool rejected (first-server-wins)",
				"tool", name, "new_server", serverID, "existing_server", existing.serverID)
			continue
		}

		// Accept: overwrite if same server (reconnect), add if new.
		r.byName[name] = &mcpToolRecord{serverID: serverID, tool: t}
		accepted = append(accepted, name)
	}

	// Replace the server's tool list atomically.
	// Remove any old names that are no longer in the new set.
	oldNames := r.byServer[serverID]
	newNameSet := make(map[string]struct{}, len(accepted))
	for _, n := range accepted {
		newNameSet[n] = struct{}{}
	}
	for _, old := range oldNames {
		if _, still := newNameSet[old]; !still {
			// Evict stale entry from this server.
			if rec, ok := r.byName[old]; ok && rec.serverID == serverID {
				delete(r.byName, old)
			}
		}
	}
	r.byServer[serverID] = accepted

	slog.Info("tools.MCPRegistry: server tools registered",
		"server", serverID, "accepted", len(accepted), "rejected", len(collisions))
	return collisions
}

// EvictServer removes all tools from the named server atomically.
func (r *MCPRegistry) EvictServer(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := r.byServer[serverID]
	for _, name := range names {
		if rec, ok := r.byName[name]; ok && rec.serverID == serverID {
			delete(r.byName, name)
		}
	}
	delete(r.byServer, serverID)
	slog.Info("tools.MCPRegistry: server evicted", "server", serverID, "tools_removed", len(names))
}

// Get returns the MCP tool registered under name, or (nil, false).
func (r *MCPRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.byName[name]
	if !ok {
		return nil, false
	}
	return rec.tool, true
}

// All returns all accepted MCP tools in sorted name order (snapshot).
func (r *MCPRegistry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, r.byName[n].tool)
	}
	return out
}

// Describe returns a snapshot of all MCP entries as MCPEntry structs.
func (r *MCPRegistry) Describe() []MCPEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]MCPEntry, 0, len(names))
	for _, n := range names {
		rec := r.byName[n]
		t := rec.tool
		out = append(out, MCPEntry{
			Name:        n,
			Description: t.Description(),
			Scope:       t.Scope(),
			Category:    t.Category(),
			Source:      "mcp:" + rec.serverID,
			ServerID:    rec.serverID,
		})
	}
	return out
}

// Count returns the number of accepted MCP tools.
func (r *MCPRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byName)
}

// MCPCollision records a rejected MCP tool registration.
type MCPCollision struct {
	ToolName     string
	ServerID     string
	ConflictWith string // "builtin", "reserved_prefix", or another server ID
	Reason       string
}
