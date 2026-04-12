// Omnipus — Core Agents
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Wave 5b spec tests #15 and #16: core agent roster v1 (issue #45).
// Tests the 5 new core agents (jim, ava, mia, ray, max) and their
// compiled-in metadata, prompts, tools, and deletion protection.

package coreagent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/coreagent"
	"github.com/dapicom-ai/omnipus/pkg/sysagent"
)

// =====================================================================
// Test #15 — TestCoreAgentRoster
// =====================================================================

// TestCoreAgentCount verifies exactly 5 core agents are returned by All().
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (core agent roster v1, issue #45)
// BDD: "Given Omnipus default config, When All() called,
//
//	Then 5 core agents returned in display order (Mia first)"
func TestCoreAgentCount(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	all := coreagent.All()
	require.Len(t, all, 5,
		"All() must return exactly 5 core agents per issue #45 (jim, ava, mia, ray, max)")
}

// TestCoreAgentDisplayOrder verifies Mia is first (default selection for new users).
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (Mia first for default selection)
func TestCoreAgentDisplayOrder(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	all := coreagent.All()
	require.NotEmpty(t, all)
	assert.Equal(t, coreagent.IDMia, all[0].ID,
		"Mia must be first in All() — she is the default selection for new users")
}

// TestCoreAgentIDs verifies all 5 expected IDs exist and are correct.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (core agent roster v1)
// BDD: "Given the 5 core agents, When ByID called for each,
//
//	Then each agent is found with the correct ID"
func TestCoreAgentIDs(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	tests := []struct {
		id   coreagent.CoreAgentID
		name string
	}{
		// Dataset: core agent roster (issue #45)
		{coreagent.IDJim, "Jim"},
		{coreagent.IDAva, "Ava"},
		{coreagent.IDMia, "Mia"},
		{coreagent.IDRay, "Ray"},
		{coreagent.IDMax, "Max"},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			agent := coreagent.ByID(tc.id)
			require.NotNil(t, agent, "%s must be registered as a core agent", tc.id)
			assert.Equal(t, tc.id, agent.ID, "ID field must match the lookup key")
			assert.Equal(t, tc.name, agent.Name, "Name must match expected display name")
		})
	}
}

// TestCoreAgentMetadata verifies each agent has a non-empty subtitle, description,
// color, and icon — the fields needed for the UI roster card.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (UI metadata fields)
// BDD: "Given a core agent, When its fields are read,
//
//	Then Subtitle, Description, Color, and Icon are all non-empty"
func TestCoreAgentMetadata(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	for _, agent := range coreagent.All() {
		// capture loop variable
		t.Run(string(agent.ID), func(t *testing.T) {
			assert.NotEmpty(t, agent.Subtitle,
				"core agent %s must have a non-empty Subtitle", agent.ID)
			assert.NotEmpty(t, agent.Description,
				"core agent %s must have a non-empty Description", agent.ID)
			assert.NotEmpty(t, agent.Color,
				"core agent %s must have a non-empty Color (hex code for avatar)", agent.ID)
			assert.NotEmpty(t, agent.Icon,
				"core agent %s must have a non-empty Icon (Phosphor icon name)", agent.ID)
		})
	}
}

// TestCoreAgentMetadataDifferentiation verifies each agent has a DIFFERENT color and
// subtitle, proving metadata is not hardcoded to a single value.
//
// Differentiation test: if all 5 agents returned the same color, this would catch it.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (each agent distinct identity)
func TestCoreAgentMetadataDifferentiation(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	colors := make(map[string]coreagent.CoreAgentID)
	subtitles := make(map[string]coreagent.CoreAgentID)

	for _, agent := range coreagent.All() {
		// Each agent must have a unique color
		if prev, exists := colors[agent.Color]; exists {
			t.Errorf("agents %s and %s share the same color %q — each agent must have a distinct color",
				prev, agent.ID, agent.Color)
		}
		colors[agent.Color] = agent.ID

		// Each agent must have a unique subtitle
		if prev, exists := subtitles[agent.Subtitle]; exists {
			t.Errorf("agents %s and %s share the same subtitle %q — each agent must have a distinct role",
				prev, agent.ID, agent.Subtitle)
		}
		subtitles[agent.Subtitle] = agent.ID
	}
}

// TestCoreAgentPromptsHardcoded verifies that all core agent prompts are
// non-empty strings compiled into the binary (not external file paths).
//
// Traces to: wave5b-system-agent-spec.md — US-8 AC2 (prompt compiled into binary)
// BDD: "Given a core agent, When GetPrompt(id) called,
//
//	Then a non-empty system prompt is returned — compiled, not a file reference"
func TestCoreAgentPromptsHardcoded(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 152 (US-8 AC2)
	for _, agent := range coreagent.All() {
		t.Run(string(agent.ID)+" has non-empty compiled prompt", func(t *testing.T) {
			prompt := coreagent.GetPrompt(string(agent.ID))
			assert.NotEmpty(t, prompt,
				"core agent %s must have a non-empty hardcoded prompt compiled into the binary", agent.ID)
			// Prompts must not be file paths themselves. Mentioning SOUL.md within
			// prompt instructions (e.g., Ava writing SOUL.md for new agents) is allowed.
			assert.NotContains(t, prompt, "/home/",
				"core agent prompt must NOT be a file path")
			assert.NotContains(t, prompt, "/var/",
				"core agent prompt must NOT be a filesystem path")
		})
	}
}

// TestCoreAgentPromptsDifferentiation verifies each agent has a DIFFERENT prompt.
// This catches a bug where all agents return the same compiled prompt.
//
// Differentiation test: call GetPrompt with two different IDs, assert different content.
//
// Traces to: wave5b-system-agent-spec.md — US-8 AC2 (per-agent compiled prompts)
func TestCoreAgentPromptsDifferentiation(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 152
	jimPrompt := coreagent.GetPrompt("jim")
	maxPrompt := coreagent.GetPrompt("max")
	assert.NotEqual(t, jimPrompt, maxPrompt,
		"Jim and Max must have different compiled prompts — not the same hardcoded string")

	miaPrompt := coreagent.GetPrompt("mia")
	rayPrompt := coreagent.GetPrompt("ray")
	assert.NotEqual(t, miaPrompt, rayPrompt,
		"Mia and Ray must have different compiled prompts")
}

// TestGetPromptUnknownID verifies GetPrompt returns empty string for unknown IDs.
// Callers fall back to SOUL.md on disk when the empty string is returned.
//
// Traces to: wave5b-system-agent-spec.md — US-8 AC2 (fallback for custom agents)
func TestGetPromptUnknownID(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 152
	result := coreagent.GetPrompt("nonexistent-agent")
	assert.Empty(t, result,
		"GetPrompt must return empty string for unknown IDs — caller falls back to SOUL.md")
}

// TestCoreAgentDefaultTools verifies each core agent has a defined default tool set.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (default tool sets per BRD D.9)
func TestCoreAgentDefaultTools(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	for _, agent := range coreagent.All() {
		t.Run(string(agent.ID)+" has default tools", func(t *testing.T) {
			assert.NotEmpty(t, agent.DefaultTools,
				"core agent %s must have at least one default tool", agent.ID)
		})
	}
}

// =====================================================================
// Test #16 — TestCoreAgentCannotDelete
// =====================================================================

// TestCoreAgentIsCoreAgentFunction verifies the IsCoreAgent function correctly
// identifies all 5 core agents and rejects non-core IDs.
//
// Traces to: wave5b-system-agent-spec.md — Scenario: Core agent cannot be deleted (US-8 AC4)
// BDD: "Given a core agent ID, When IsCoreAgent called,
//
//	Then true is returned; for custom IDs, false"
func TestCoreAgentIsCoreAgentFunction(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 679
	coreIDs := []struct {
		id       string
		isCore   bool
		scenario string
	}{
		// Dataset: core agent IDs that must be protected from deletion
		{"jim", true, "Jim is a core agent"},
		{"ava", true, "Ava is a core agent"},
		{"mia", true, "Mia is a core agent"},
		{"ray", true, "Ray is a core agent"},
		{"max", true, "Max is a core agent"},
		// Dataset: IDs that must NOT be identified as core
		{"omnipus-system", false, "omnipus-system is the system agent, protected separately"},
		{"financial-analyst", false, "custom agent is not a core agent"},
		{"devops-helper", false, "custom agent is not a core agent"},
		{"my-custom-agent", false, "custom agent is not a core agent"},
		{"", false, "empty string is not a core agent"},
		// Old IDs that no longer exist must return false
		{"general-assistant", false, "old ID removed in issue #45"},
		{"researcher", false, "old ID removed in issue #45"},
		{"content-creator", false, "old ID removed in issue #45"},
	}

	for _, tc := range coreIDs {
		t.Run(tc.scenario, func(t *testing.T) {
			got := coreagent.IsCoreAgent(tc.id)
			assert.Equal(t, tc.isCore, got,
				"IsCoreAgent(%q): expected %v, got %v — %s", tc.id, tc.isCore, got, tc.scenario)
		})
	}
}

// TestCoreAgentByIDNotFound verifies ByID returns nil for unknown IDs.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (ByID lookup safety)
func TestCoreAgentByIDNotFound(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	result := coreagent.ByID(coreagent.CoreAgentID("nonexistent"))
	assert.Nil(t, result, "ByID must return nil for unknown agent IDs")
}

// TestAdminRBACAllowedToCallDelete verifies RBAC permits admin to reach
// system.agent.delete — the tool itself enforces IsCoreAgent protection.
//
// Traces to: wave5b-system-agent-spec.md — Scenario: Core agent cannot be deleted (US-8 AC4)
// BDD: "Given admin role, When system.agent.delete RBAC checked,
//
//	Then RBAC allows it — core agent protection is enforced at the tool level"
func TestAdminRBACAllowedToCallDelete(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 679
	// Admin CAN reach system.agent.delete via RBAC; the tool implementation
	// must then check IsCoreAgent and return PERMISSION_DENIED for core IDs.
	err := sysagent.CheckRBAC(sysagent.RoleAdmin, "system.agent.delete")
	assert.NoError(t, err,
		"admin RBAC check for delete must pass — core agent protection is at the tool level, not RBAC")
}

// =====================================================================
// Test: SeedConfig
// =====================================================================

// TestSeedConfigAddsAllCoreAgents verifies SeedConfig adds all 5 core agents
// to an empty config with Locked=true and returns true (modified).
//
// Persistence test: write via SeedConfig, read back from cfg.Agents.List.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (seed on first boot)
// BDD: "Given empty config, When SeedConfig called,
//
//	Then all 5 core agents are in cfg.Agents.List with Locked=true"
func TestSeedConfigAddsAllCoreAgents(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	cfg := &config.Config{}
	modified := coreagent.SeedConfig(cfg)

	assert.True(t, modified,
		"SeedConfig must return true when agents are added to an empty config")
	assert.Len(t, cfg.Agents.List, 5,
		"SeedConfig must add exactly 5 core agents to an empty config")

	// Verify each core agent was seeded with Locked=true
	seededIDs := make(map[string]config.AgentConfig)
	for _, ac := range cfg.Agents.List {
		seededIDs[ac.ID] = ac
	}

	for _, ca := range coreagent.All() {
		t.Run(string(ca.ID)+" is seeded correctly", func(t *testing.T) {
			ac, found := seededIDs[string(ca.ID)]
			require.True(t, found, "agent %s must be present in seeded config", ca.ID)
			assert.True(t, ac.Locked,
				"core agent %s must be seeded with Locked=true to protect identity fields", ca.ID)
			assert.Equal(t, config.AgentTypeCore, ac.Type,
				"core agent %s must have Type=core", ca.ID)
			assert.Equal(t, ca.Name, ac.Name,
				"core agent %s name must match compiled metadata", ca.ID)
			require.NotNil(t, ac.Enabled,
				"core agent %s must have Enabled set", ca.ID)
			assert.True(t, *ac.Enabled,
				"core agent %s must be enabled=true by default", ca.ID)
		})
	}
}

// TestSeedConfigIsIdempotent verifies SeedConfig does NOT duplicate agents when
// called twice on the same config.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (SeedConfig idempotent)
// BDD: "Given config with all 5 core agents, When SeedConfig called again,
//
//	Then no agents are added and false is returned"
func TestSeedConfigIsIdempotent(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	cfg := &config.Config{}

	// First call: adds all 5 agents
	modified1 := coreagent.SeedConfig(cfg)
	assert.True(t, modified1, "first SeedConfig call must return true (agents added)")
	assert.Len(t, cfg.Agents.List, 5)

	// Second call: must be a no-op
	modified2 := coreagent.SeedConfig(cfg)
	assert.False(t, modified2,
		"second SeedConfig call must return false — all agents already present")
	assert.Len(t, cfg.Agents.List, 5,
		"SeedConfig must not duplicate agents on repeated calls")
}

// TestSeedConfigPreservesExistingAgents verifies SeedConfig adds missing core agents
// but does NOT overwrite existing ones (including custom agents).
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (SeedConfig preserves custom agents)
func TestSeedConfigPreservesExistingAgents(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			List: []config.AgentConfig{
				{ID: "my-custom-agent", Name: "My Custom Agent"},
			},
		},
	}

	modified := coreagent.SeedConfig(cfg)
	assert.True(t, modified, "SeedConfig must add missing core agents")
	// 5 core agents + 1 existing custom agent
	assert.Len(t, cfg.Agents.List, 6,
		"SeedConfig must preserve existing agents while adding the 5 core agents")

	// Verify custom agent is still present and unchanged
	found := false
	for _, ac := range cfg.Agents.List {
		if ac.ID == "my-custom-agent" {
			found = true
			assert.Equal(t, "My Custom Agent", ac.Name,
				"existing custom agent name must not be overwritten by SeedConfig")
			assert.False(t, ac.Locked,
				"custom agent must NOT be locked by SeedConfig")
		}
	}
	assert.True(t, found, "existing custom agent must still be present after SeedConfig")
}
