package agent

import (
	"sort"
	"sync"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/logger"
	"github.com/dapicom-ai/omnipus/pkg/providers"
	"github.com/dapicom-ai/omnipus/pkg/routing"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// AgentRegistry manages multiple agent instances and routes messages to them.
type AgentRegistry struct {
	agents   map[string]*AgentInstance
	resolver *routing.RouteResolver
	mu       sync.RWMutex
}

// NewAgentRegistry creates a registry from config, instantiating all agents.
func NewAgentRegistry(
	cfg *config.Config,
	provider providers.LLMProvider,
) *AgentRegistry {
	registry := &AgentRegistry{
		agents:   make(map[string]*AgentInstance),
		resolver: routing.NewRouteResolver(cfg),
	}

	// Always register the default/system agent. This handles messages that
	// don't target a specific custom agent (e.g., system agent in webchat,
	// unrouted channel messages). Uses the default workspace.
	defaultAgent := &config.AgentConfig{
		ID:      "main",
		Default: true,
	}
	defaultInstance := NewAgentInstance(defaultAgent, &cfg.Agents.Defaults, cfg, provider)
	registry.agents["main"] = defaultInstance
	logger.InfoCF("agent", "Registered default agent (main)", map[string]any{
		"workspace": defaultInstance.Workspace,
		"model":     defaultInstance.Model,
	})

	// Register custom agents from config.
	for i := range cfg.Agents.List {
		ac := &cfg.Agents.List[i]
		id := routing.NormalizeAgentID(ac.ID)
		instance := NewAgentInstance(ac, &cfg.Agents.Defaults, cfg, provider)
		registry.agents[id] = instance
		logger.InfoCF("agent", "Registered agent",
			map[string]any{
				"agent_id":  id,
				"name":      ac.Name,
				"workspace": instance.Workspace,
				"model":     instance.Model,
			})
	}

	return registry
}

// GetAgent returns the agent instance for a given ID.
func (r *AgentRegistry) GetAgent(agentID string) (*AgentInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id := routing.NormalizeAgentID(agentID)
	agent, ok := r.agents[id]
	return agent, ok
}

// ResolveRoute determines which agent handles the message.
func (r *AgentRegistry) ResolveRoute(input routing.RouteInput) routing.ResolvedRoute {
	return r.resolver.ResolveRoute(input)
}

// ListAgentIDs returns all registered agent IDs.
func (r *AgentRegistry) ListAgentIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	return ids
}

// CanSpawnSubagent checks if parentAgentID is allowed to spawn targetAgentID.
func (r *AgentRegistry) CanSpawnSubagent(parentAgentID, targetAgentID string) bool {
	parent, ok := r.GetAgent(parentAgentID)
	if !ok {
		return false
	}
	if parent.Subagents == nil || parent.Subagents.AllowAgents == nil {
		return false
	}
	targetNorm := routing.NormalizeAgentID(targetAgentID)
	for _, allowed := range parent.Subagents.AllowAgents {
		if allowed == "*" {
			return true
		}
		if routing.NormalizeAgentID(allowed) == targetNorm {
			return true
		}
	}
	return false
}

// ForEachTool calls fn for every tool registered under the given name
// across all agents. This is useful for propagating dependencies (e.g.
// MediaStore) to tools after registry construction.
func (r *AgentRegistry) ForEachTool(name string, fn func(tools.Tool)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, agent := range r.agents {
		if t, ok := agent.Tools.Get(name); ok {
			fn(t)
		}
	}
}

// Close releases resources held by all registered agents and clears the map (M9).
func (r *AgentRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, agent := range r.agents {
		if err := agent.Close(); err != nil {
			logger.WarnCF("agent", "Failed to close agent",
				map[string]any{"agent_id": agent.ID, "error": err.Error()})
		}
	}
	// Clear the map so any post-Close access gets nil rather than stale data.
	r.agents = nil
}

// GetDefaultAgent returns the default agent instance.
// "main" is preferred; otherwise the agent with the lexicographically first ID is
// returned to give deterministic selection regardless of map iteration order (M10).
func (r *AgentRegistry) GetDefaultAgent() *AgentInstance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if agent, ok := r.agents["main"]; ok {
		return agent
	}
	// Collect and sort IDs so we always pick the same agent.
	ids := make([]string, 0, len(r.agents))
	for id := range r.agents {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return r.agents[ids[0]]
}
