// Omnipus — Agent Loop System Tools Wiring Tests
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	systools "github.com/dapicom-ai/omnipus/pkg/sysagent/tools"
)

// TestNewAgentInstance_SystemAgentHasSysagentTools verifies that WireSystemTools
// registers system.* tools only on the system agent ("main") and not on any other
// agent entry.
//
// Traces to: wave5b-system-agent-spec.md — "omnipus-system is the only agent that
// receives system.* tools from BuildRegistry" (layer 1 of #41).
func TestNewAgentInstance_SystemAgentHasSysagentTools(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sysagent-wiring-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.ModelName = "test-model"
	// Add a non-system custom agent to verify it does NOT get system tools.
	cfg.Agents.List = []config.AgentConfig{
		{ID: "custom-bot", Name: "Custom Bot"},
	}

	msgBus := bus.NewMessageBus()
	al := NewAgentLoop(cfg, msgBus, &mockProvider{})

	credStore := credentials.NewStore(tmpDir + "/credentials.json")
	deps := &systools.Deps{
		Home:       tmpDir,
		ConfigPath: tmpDir + "/config.json",
		GetCfg:     func() *config.Config { return cfg },
		SaveConfig: func() error { return nil },
		CredStore:  credStore,
	}
	// navCb is nil — navigate tool tolerates nil callback.
	if err := al.WireSystemTools(deps, nil); err != nil {
		t.Fatalf("WireSystemTools: %v", err)
	}

	// The system agent ("main"/"omnipus-system") must have system.agent.create.
	sysAgent, ok := al.GetRegistry().GetAgent("main")
	if !ok || sysAgent == nil {
		t.Fatal("system agent (main) not found in registry")
	}
	systemToolNames := []string{
		"system.agent.create",
		"system.agent.update",
		"system.agent.delete",
		"system.agent.list",
		"system.agent.activate",
		"system.agent.deactivate",
	}
	for _, name := range systemToolNames {
		if _, exists := sysAgent.Tools.Get(name); !exists {
			t.Errorf("system agent missing expected tool %q after WireSystemTools", name)
		}
	}

	// The custom agent must NOT have any system.* tools.
	customAgent, ok := al.GetRegistry().GetAgent("custom-bot")
	if !ok || customAgent == nil {
		t.Fatal("custom-bot agent not found in registry")
	}
	for _, name := range customAgent.Tools.List() {
		if strings.HasPrefix(name, "system.") {
			t.Errorf("custom-bot agent has unexpected system tool %q", name)
		}
	}
}
