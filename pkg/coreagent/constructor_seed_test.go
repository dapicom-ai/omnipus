// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package coreagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapicom-ai/omnipus/pkg/config"
)

// TestBoot_ConstructorSeedDispositionMap verifies that each core agent's
// constructor seed contains the expected policies.
//
// BDD: Given each core agent ID,
//
//	When coreAgentSeed is called,
//	Then defaultPolicy is "allow" for all core agents;
//	And the "system.*: deny" wildcard is present;
//	And Ava additionally has explicit allows for 4 system.* tools;
//	And non-Ava core agents do NOT have explicit system.* allows.
//
// Traces to: pkg/coreagent/core.go — coreAgentSeed (FR-008, FR-010, FR-022).
func TestBoot_ConstructorSeedDispositionMap(t *testing.T) {
	avaAllowedTools := []string{
		"system.agent.create",
		"system.agent.update",
		"system.agent.delete",
		"system.models.list",
	}

	tests := []struct {
		id                  CoreAgentID
		expectSystemDeny    bool
		expectExtraAllows   []string
		expectNoExtraAllow  bool
	}{
		{
			id:               IDAva,
			expectSystemDeny: true,
			expectExtraAllows: avaAllowedTools,
		},
		{
			id:               IDJim,
			expectSystemDeny: true,
			expectNoExtraAllow: true,
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.id), func(t *testing.T) {
			dp, policies := coreAgentSeed(tc.id)

			assert.Equal(t, config.ToolPolicyAllow, dp,
				"defaultPolicy must be 'allow' for all core agents (FR-022)")

			// system.* wildcard deny must be present.
			if tc.expectSystemDeny {
				p, ok := policies["system.*"]
				require.True(t, ok, "policies must contain 'system.*' key (FR-022)")
				assert.Equal(t, config.ToolPolicyDeny, p,
					"'system.*' must be set to deny (FR-022)")
			}

			// Ava gets 4 extra allows.
			for _, toolName := range tc.expectExtraAllows {
				p, ok := policies[toolName]
				require.True(t, ok, "Ava must have explicit allow for %q (FR-010)", toolName)
				assert.Equal(t, config.ToolPolicyAllow, p,
					"Ava's explicit allow for %q must be 'allow' (FR-010)", toolName)
			}

			// Non-Ava agents must NOT have extra system.* allows.
			if tc.expectNoExtraAllow {
				for key := range policies {
					if key != "system.*" {
						t.Errorf("agent %q must not have extra policy entries beyond 'system.*', found %q", tc.id, key)
					}
				}
			}
		})
	}
}

// TestBoot_HasSystemAllowsInConstructorSeed verifies that only Ava returns true
// from HasSystemAllowsInConstructorSeed.
//
// BDD: Given each core agent ID,
//
//	When HasSystemAllowsInConstructorSeed is called,
//	Then only Ava returns true;
//	And all other known core agents return false.
//
// Traces to: pkg/coreagent/core.go — HasSystemAllowsInConstructorSeed (FR-062).
func TestBoot_HasSystemAllowsInConstructorSeed(t *testing.T) {
	assert.True(t, HasSystemAllowsInConstructorSeed(string(IDAva)),
		"Ava must return true (she has explicit system.* allows)")

	nonAvaAgents := []CoreAgentID{IDJim, IDMia, IDRay, IDMax}
	for _, id := range nonAvaAgents {
		assert.False(t, HasSystemAllowsInConstructorSeed(string(id)),
			"agent %q must return false (no explicit system.* allows)", id)
	}

	// Unknown agent IDs must also return false.
	assert.False(t, HasSystemAllowsInConstructorSeed("some-custom-agent"))
	assert.False(t, HasSystemAllowsInConstructorSeed(""))
}

// TestAgentConstructor_CustomAgent_SeedsSystemDeny verifies that a newly created
// custom agent config has {"system.*": "deny"} in its policy map.
//
// BDD: Given a new custom agent created via NewCustomAgentToolsCfg,
//
//	When the resulting config is inspected,
//	Then default_policy is "allow" and policies["system.*"] is "deny".
//
// Traces to: pkg/coreagent/core.go — NewCustomAgentToolsCfg (FR-022).
func TestAgentConstructor_CustomAgent_SeedsSystemDeny(t *testing.T) {
	cfg := NewCustomAgentToolsCfg()
	require.NotNil(t, cfg, "NewCustomAgentToolsCfg must return a non-nil config")

	assert.Equal(t, config.ToolPolicyAllow, cfg.Builtin.DefaultPolicy,
		"custom agent default_policy must be 'allow' (FR-022)")

	p, ok := cfg.Builtin.Policies["system.*"]
	require.True(t, ok, "custom agent must have 'system.*' in Policies (FR-022)")
	assert.Equal(t, config.ToolPolicyDeny, p,
		"custom agent 'system.*' must be 'deny' (FR-022)")
}

// TestAgentConstructor_CoreAgent_SeedsRailPlusAllowances verifies that each core
// agent's SeedConfig call produces the correct policy configuration.
//
// BDD: Given SeedConfig is called for Ava's ID,
//
//	When the resulting agent config is found in cfg.Agents.List,
//	Then its Tools.Builtin.Policies has {"system.*": "deny"} plus 4 explicit allows.
//
// Traces to: pkg/coreagent/core.go — SeedConfig (FR-008, FR-022).
func TestAgentConstructor_CoreAgent_SeedsRailPlusAllowances(t *testing.T) {
	cfg := &config.Config{}
	SeedConfig(cfg)

	var avaAgent *config.AgentConfig
	for i := range cfg.Agents.List {
		if cfg.Agents.List[i].ID == string(IDAva) {
			avaAgent = &cfg.Agents.List[i]
			break
		}
	}
	require.NotNil(t, avaAgent, "SeedConfig must add Ava to cfg.Agents.List")
	require.NotNil(t, avaAgent.Tools, "Ava's Tools config must be non-nil after seed")

	p, ok := avaAgent.Tools.Builtin.Policies["system.*"]
	require.True(t, ok, "Ava must have 'system.*' deny in seeded config")
	assert.Equal(t, config.ToolPolicyDeny, p)

	for _, allow := range []string{"system.agent.create", "system.agent.update", "system.agent.delete", "system.models.list"} {
		ap, aok := avaAgent.Tools.Builtin.Policies[allow]
		require.True(t, aok, "Ava must have explicit allow for %q", allow)
		assert.Equal(t, config.ToolPolicyAllow, ap)
	}
}
