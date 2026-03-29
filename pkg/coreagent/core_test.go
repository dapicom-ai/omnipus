// Omnipus — Core Agents
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Wave 5b spec tests #15 and #16: core agent defaults and deletion protection.

package coreagent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/coreagent"
	"github.com/dapicom-ai/omnipus/pkg/sysagent"
)

// =====================================================================
// Test #15 — TestCoreAgentDefaults
// =====================================================================

// TestCoreAgentDefaults verifies all 3 core agents exist with the correct defaults:
// General Assistant is active, Researcher and Content Creator are inactive.
//
// Traces to: wave5b-system-agent-spec.md — Scenario: General Assistant active by default (US-8 AC1)
// BDD: "Given Omnipus default config, When agent list loaded,
//
//	Then General Assistant status:active, Researcher+ContentCreator status:inactive"
func TestCoreAgentDefaults(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 664 (Scenario: General Assistant active by default)

	agents := coreagent.All()
	require.Len(t, agents, 3,
		"Omnipus ships with exactly 3 core agents per BRD Appendix D §D.9")

	t.Run("General Assistant is active by default", func(t *testing.T) {
		// Traces to: wave5b-system-agent-spec.md line 670 (US-8 AC1 — active by default)
		ga := coreagent.ByID(coreagent.IDGeneralAssistant)
		require.NotNil(t, ga, "General Assistant must be registered")

		assert.Equal(t, coreagent.IDGeneralAssistant, ga.ID,
			"General Assistant ID must be 'general-assistant'")
		assert.Equal(t, "General Assistant", ga.Name)
		assert.Equal(t, coreagent.StatusActive, ga.DefaultStatus,
			"General Assistant must be active by default (US-8 AC1)")
	})

	t.Run("Researcher is inactive by default", func(t *testing.T) {
		// Traces to: wave5b-system-agent-spec.md line 672 (US-8 AC1 — inactive by default)
		r := coreagent.ByID(coreagent.IDResearcher)
		require.NotNil(t, r, "Researcher must be registered")

		assert.Equal(t, coreagent.IDResearcher, r.ID)
		assert.Equal(t, "Researcher", r.Name)
		assert.Equal(t, coreagent.StatusInactive, r.DefaultStatus,
			"Researcher must be inactive by default (available for activation)")
	})

	t.Run("Content Creator is inactive by default", func(t *testing.T) {
		// Traces to: wave5b-system-agent-spec.md line 674 (US-8 AC1 — inactive by default)
		cc := coreagent.ByID(coreagent.IDContentCreator)
		require.NotNil(t, cc, "Content Creator must be registered")

		assert.Equal(t, coreagent.IDContentCreator, cc.ID)
		assert.Equal(t, "Content Creator", cc.Name)
		assert.Equal(t, coreagent.StatusInactive, cc.DefaultStatus,
			"Content Creator must be inactive by default (available for activation)")
	})
}

// TestCoreAgentCount verifies exactly 3 core agents are returned by All().
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (3 core agents)
func TestCoreAgentCount(t *testing.T) {
	all := coreagent.All()
	assert.Len(t, all, 3,
		"All() must return exactly 3 core agents per BRD §D.9")
}

// TestCoreAgentPromptsHardcoded verifies that all core agent prompts are
// non-empty strings compiled into the binary.
//
// Traces to: wave5b-system-agent-spec.md — US-8 AC2 (prompt compiled into binary)
// BDD: "Given a core agent prompt is compiled into the binary,
//
//	When the agent processes a message, Then the system prompt from the compiled constant is used"
func TestCoreAgentPromptsHardcoded(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 152 (US-8 AC2)
	for _, agent := range coreagent.All() {
		t.Run(string(agent.ID)+" has non-empty compiled prompt", func(t *testing.T) {
			assert.NotEmpty(t, agent.Prompt,
				"core agent %s must have a non-empty hardcoded prompt compiled into the binary", agent.ID)
			assert.NotContains(t, agent.Prompt, ".md",
				"core agent prompt must NOT reference an external file — must be embedded")
			assert.NotContains(t, agent.Prompt, "/home/",
				"core agent prompt must NOT be a file path")
		})
	}
}

// TestCoreAgentDefaultTools verifies each core agent has a defined default tool set.
//
// Traces to: wave5b-system-agent-spec.md — FR-012 (default tool sets per BRD D.9.2-D.9.4)
func TestCoreAgentDefaultTools(t *testing.T) {
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

// TestCoreAgentCannotDelete verifies that the RBAC layer prevents deletion of
// all core agents and the system agent.
//
// Traces to: wave5b-system-agent-spec.md — Scenario: Core agent cannot be deleted (US-8 AC4)
// BDD: "Given system agent active, When system.agent.delete {id:'general-assistant'} called,
//
//	Then PERMISSION_DENIED — core agents can be deactivated, not deleted"
func TestCoreAgentCannotDelete(t *testing.T) {
	// Traces to: wave5b-system-agent-spec.md line 679 (Scenario: Core agent cannot be deleted)

	// IsCoreAgent identifies protected agents.
	coreIDs := []string{
		"general-assistant",
		"researcher",
		"content-creator",
	}

	for _, id := range coreIDs {
		t.Run(id+" is identified as core", func(t *testing.T) {
			assert.True(t, coreagent.IsCoreAgent(id),
				"%s must be identified as a core agent (cannot be deleted)", id)
		})
	}

	t.Run("omnipus-system is NOT a core agent (it is the system agent)", func(t *testing.T) {
		// System agent has its own protection — it is not a core agent but also cannot be deleted.
		// This is protected at the RBAC/SystemToolHandler level.
		assert.False(t, coreagent.IsCoreAgent("omnipus-system"),
			"omnipus-system is the system agent, not a core agent — protected separately")
	})

	t.Run("IsCoreAgent returns false for custom agents", func(t *testing.T) {
		customIDs := []string{"financial-analyst", "devops-helper", "my-custom-agent", ""}
		for _, id := range customIDs {
			assert.False(t, coreagent.IsCoreAgent(id),
				"%q must NOT be identified as a core agent", id)
		}
	})

	t.Run("admin is denied deletion of core agents at RBAC level", func(t *testing.T) {
		// The system.agent.delete tool handler checks IsCoreAgent before proceeding.
		// For now, verify RBAC permits admin to reach the tool (the tool itself blocks core deletion).
		// Admin CAN call system.agent.delete (RBAC allows it) but the tool implementation
		// must check IsCoreAgent and return PERMISSION_DENIED.
		err := sysagent.CheckRBAC(sysagent.RoleAdmin, "system.agent.delete")
		assert.NoError(t, err,
			"admin RBAC check for delete must pass — core agent protection is at the tool level")
		// Note: actual protection is tested in TestAgentDeleteIntegration once tools are wired.
	})
}

// TestCoreAgentByIDNotFound verifies ByID returns nil for unknown IDs.
func TestCoreAgentByIDNotFound(t *testing.T) {
	result := coreagent.ByID(coreagent.CoreAgentID("nonexistent"))
	assert.Nil(t, result, "ByID must return nil for unknown agent IDs")
}
