// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveAgentForMessage is a helper that simulates Omnipus multi-agent routing.
// It finds the first binding matching channel+senderID, or returns the default agent.
// Returns ("", false) if no match and no default.
func resolveAgentForMessage(cfg *Config, channel, senderID string) (agentID string, found bool) {
	// First pass: find a specific rule match.
	for _, b := range cfg.Bindings {
		if b.Match.Channel != channel {
			continue
		}
		if b.Match.AccountID == senderID || b.Match.AccountID == "*" {
			return b.AgentID, true
		}
	}

	// Second pass: look for wildcard channel match (default agent).
	for _, b := range cfg.Bindings {
		if b.Match.Channel == channel && b.Match.AccountID == "" {
			return b.AgentID, true
		}
	}

	return "", false
}

// TestMessageBusRouting verifies routing rule matching dispatches to correct agent.
// Traces to: wave1-core-foundation-spec.md Scenario: Route message to correct agent by channel+user (US-6 AC1, FR-014)
func TestMessageBusRouting(t *testing.T) {
	cfg := &Config{
		ChannelPolicies: map[string]OmnipusChannelPolicy{
			"telegram": {
				RoutingRules: []OmnipusChannelRoutingRule{
					{UserID: "123", AgentID: "agent-a"},
					{UserID: "456", AgentID: "agent-b"},
				},
			},
		},
	}
	cfg.MergeChannelPoliciesIntoBindings()

	require.Len(t, cfg.Bindings, 2, "2 routing rules must produce 2 bindings")

	tests := []struct {
		name            string
		channel         string
		senderID        string
		expectedAgentID string
	}{
		// Dataset: Multi-agent routing — Telegram user 123 → agent-A
		{"user 123 maps to agent-a", "telegram", "123", "agent-a"},
		// Dataset: Multi-agent routing — Telegram user 456 → agent-B
		{"user 456 maps to agent-b", "telegram", "456", "agent-b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agentID, found := resolveAgentForMessage(cfg, tc.channel, tc.senderID)
			assert.True(t, found, "routing must find a match for %s:%s", tc.channel, tc.senderID)
			assert.Equal(t, tc.expectedAgentID, agentID)
		})
	}
}

// TestMessageBusDefaultAgent verifies unmatched messages fall back to default agent.
// Traces to: wave1-core-foundation-spec.md Scenario: Unrouted message goes to default agent (US-6 AC2, FR-014)
func TestMessageBusDefaultAgent(t *testing.T) {
	cfg := &Config{
		ChannelPolicies: map[string]OmnipusChannelPolicy{
			"telegram": {
				RoutingRules: []OmnipusChannelRoutingRule{
					{UserID: "123", AgentID: "personal"},
					// wildcard rule: "*" matches any user (default for channel)
					{UserID: "*", AgentID: "default-agent"},
				},
			},
		},
	}
	cfg.MergeChannelPoliciesIntoBindings()

	t.Run("known user gets specific agent", func(t *testing.T) {
		agentID, found := resolveAgentForMessage(cfg, "telegram", "123")
		assert.True(t, found)
		assert.Equal(t, "personal", agentID)
	})

	t.Run("unknown user gets default agent via wildcard", func(t *testing.T) {
		agentID, found := resolveAgentForMessage(cfg, "telegram", "999")
		assert.True(t, found, "unknown user must fall back to wildcard rule")
		assert.Equal(t, "default-agent", agentID)
	})
}

// TestMessageBusNoMatch verifies no match and no default results in routing failure.
// Traces to: wave1-core-foundation-spec.md Scenario: No default agent and no matching rule (US-6 AC3, FR-015)
func TestMessageBusNoMatch(t *testing.T) {
	cfg := &Config{
		ChannelPolicies: map[string]OmnipusChannelPolicy{
			"telegram": {
				RoutingRules: []OmnipusChannelRoutingRule{
					{UserID: "123", AgentID: "only-this-user"},
				},
			},
		},
	}
	cfg.MergeChannelPoliciesIntoBindings()

	t.Run("unknown user on configured channel returns not found", func(t *testing.T) {
		_, found := resolveAgentForMessage(cfg, "telegram", "999")
		assert.False(t, found, "unmatched message with no default must return not found")
	})

	t.Run("message from unconfigured channel returns not found", func(t *testing.T) {
		_, found := resolveAgentForMessage(cfg, "discord", "123")
		assert.False(t, found, "unconfigured channel must return not found")
	})
}

// TestMergeChannelPoliciesIntoBindings verifies routing rules are merged into Bindings.
// Traces to: wave1-core-foundation-spec.md Scenario: Routing rules loaded from config (US-6 AC4, FR-014)
func TestMergeChannelPoliciesIntoBindings(t *testing.T) {
	cfg := &Config{
		ChannelPolicies: map[string]OmnipusChannelPolicy{
			"telegram": {
				RoutingRules: []OmnipusChannelRoutingRule{
					{UserID: "abc", AgentID: "agent-1"},
					{UserID: "def", AgentID: "agent-2"},
				},
			},
			"discord": {
				RoutingRules: []OmnipusChannelRoutingRule{
					{UserID: "guild-member", AgentID: "agent-3"},
				},
			},
		},
	}

	require.Empty(t, cfg.Bindings, "bindings must be empty before merge")
	cfg.MergeChannelPoliciesIntoBindings()

	assert.Len(t, cfg.Bindings, 3, "3 routing rules must produce 3 bindings")

	// Find telegram bindings.
	var telegramBindings []AgentBinding
	for _, b := range cfg.Bindings {
		if b.Match.Channel == "telegram" {
			telegramBindings = append(telegramBindings, b)
		}
	}
	assert.Len(t, telegramBindings, 2, "telegram must have 2 bindings")

	// Find discord binding.
	var discordBindings []AgentBinding
	for _, b := range cfg.Bindings {
		if b.Match.Channel == "discord" {
			discordBindings = append(discordBindings, b)
		}
	}
	assert.Len(t, discordBindings, 1, "discord must have 1 binding")
	assert.Equal(t, "agent-3", discordBindings[0].AgentID)
	assert.Equal(t, "guild-member", discordBindings[0].Match.AccountID)
}
