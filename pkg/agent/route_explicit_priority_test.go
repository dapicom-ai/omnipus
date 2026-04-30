package agent

import (
	"path/filepath"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/config"
)

// TestResolveMessageRoute_ExplicitAgentIDOverridesHandoffOverride pins the
// post-fix priority: when a message carries an explicit agent_id (the SPA
// dropdown selection) AND a stale sessionActiveAgent override exists for
// that chat from a prior handoff, the explicit selection wins. Without
// this priority, switching the dropdown back to Jim after a Mia → Ray
// handoff would silently route the message to Ray and Jim would never
// hear it — manifesting as "Ray's tool calls leak into Jim's session".
func TestResolveMessageRoute_ExplicitAgentIDOverridesHandoffOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMNIPUS_HOME", home)

	cfg := &config.Config{}
	cfg.Agents.Defaults.Workspace = filepath.Join(home, "default-workspace")
	cfg.Agents.Defaults.ModelName = "test-model"

	msgBus := bus.NewMessageBus()
	al := mustNewAgentLoop(t, cfg, msgBus, &mockProvider{})
	t.Cleanup(func() { al.Close() })

	for _, id := range []string{"jim", "ray"} {
		ag := NewAgentInstance(&config.AgentConfig{ID: id, Name: id},
			&cfg.Agents.Defaults, cfg, &mockProvider{})
		ag.Workspace = filepath.Join(home, "agents", id)
		ag.ContextBuilder = NewContextBuilder(ag.Workspace).WithAgentInfo(id, id)
		al.registry.mu.Lock()
		al.registry.agents[id] = ag
		al.registry.mu.Unlock()
	}

	const chatID = "chat-leak-1"
	// Simulate the state left by a prior Mia → Ray handoff.
	al.sessionActiveAgent.Store("chat:"+chatID, "ray")

	// User picks Jim in the SPA dropdown — webchat sends agent_id=jim.
	msg := bus.InboundMessage{
		Channel:  "webchat",
		ChatID:   chatID,
		Content:  "what is my name",
		Metadata: map[string]string{"agent_id": "jim"},
	}
	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatalf("resolveMessageRoute: %v", err)
	}
	if route.AgentID != "jim" {
		t.Fatalf("explicit agent_id=jim was overridden — routed to %q (Ray's tool calls leak into Jim's session)", route.AgentID)
	}
	if agent == nil || agent.ID != "jim" {
		t.Fatalf("returned agent should be jim, got %+v", agent)
	}
	if _, stale := al.sessionActiveAgent.Load("chat:" + chatID); stale {
		t.Error("explicit agent_id should have cleared the stale handoff override")
	}
}

// TestResolveMessageRoute_HandoffOverrideStillAppliesWhenNoExplicitAgentID
// confirms the override still works for inputs that don't carry an explicit
// agent_id (e.g. non-webchat channels, or webchat clients that haven't yet
// processed the agent_switched frame).
func TestResolveMessageRoute_HandoffOverrideStillAppliesWhenNoExplicitAgentID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OMNIPUS_HOME", home)

	cfg := &config.Config{}
	cfg.Agents.Defaults.Workspace = filepath.Join(home, "default-workspace")
	cfg.Agents.Defaults.ModelName = "test-model"

	msgBus := bus.NewMessageBus()
	al := mustNewAgentLoop(t, cfg, msgBus, &mockProvider{})
	t.Cleanup(func() { al.Close() })

	ag := NewAgentInstance(&config.AgentConfig{ID: "ray", Name: "ray"},
		&cfg.Agents.Defaults, cfg, &mockProvider{})
	ag.Workspace = filepath.Join(home, "agents", "ray")
	ag.ContextBuilder = NewContextBuilder(ag.Workspace).WithAgentInfo("ray", "ray")
	al.registry.mu.Lock()
	al.registry.agents["ray"] = ag
	al.registry.mu.Unlock()

	const chatID = "chat-leak-2"
	al.sessionActiveAgent.Store("chat:"+chatID, "ray")

	msg := bus.InboundMessage{
		Channel: "webchat",
		ChatID:  chatID,
		Content: "follow up after handoff with no agent_id metadata",
	}
	route, _, err := al.resolveMessageRoute(msg)
	if err != nil {
		t.Fatalf("resolveMessageRoute: %v", err)
	}
	if route.AgentID != "ray" {
		t.Fatalf("handoff override should still route to ray when no explicit agent_id, got %q", route.AgentID)
	}
}
