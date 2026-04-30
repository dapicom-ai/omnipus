// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/providers"
)

// mustAgentLoop is a gateway test helper that calls agent.NewAgentLoop and
// fatals on error. Reduces boilerplate after the return-type change (FR-029a).
func mustAgentLoop(t *testing.T, cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider) *agent.AgentLoop {
	t.Helper()
	al, err := agent.NewAgentLoop(cfg, msgBus, provider)
	if err != nil {
		t.Fatalf("agent.NewAgentLoop: %v", err)
	}
	return al
}
