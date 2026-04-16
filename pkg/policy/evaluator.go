package policy

import (
	"fmt"
	"strings"
)

// Evaluator checks tool invocations against security policies.
// Immutable after construction — safe for concurrent use (SEC-12).
type Evaluator struct {
	defaultPolicy     DefaultPolicy
	agents            map[string]AgentPolicy
	execAllowedBins   []string
	toolPolicies      map[string]ToolPolicy
	defaultToolPolicy ToolPolicy
}

// NewEvaluator creates a policy evaluator from a SecurityConfig.
// A nil config uses deny-by-default with no agent policies.
func NewEvaluator(cfg *SecurityConfig) *Evaluator {
	if cfg == nil {
		return &Evaluator{defaultPolicy: PolicyDeny}
	}
	dp := PolicyDeny
	if cfg.DefaultPolicy != "" {
		dp = cfg.DefaultPolicy
	}
	return &Evaluator{
		defaultPolicy:     dp,
		agents:            cfg.Agents,
		execAllowedBins:   cfg.Policy.Exec.AllowedBinaries,
		toolPolicies:      cfg.ToolPolicies,
		defaultToolPolicy: cfg.DefaultToolPolicy,
	}
}

// EvaluateTool checks whether an agent is permitted to invoke a tool.
//
// When called with 2 args (agentID, toolName), the agent policy is looked up
// from the config provided at construction.
// When called with 3 args (agentID, toolName, *AgentPolicy), the explicit
// policy is used instead of config lookup.
//
// Evaluation order (SEC-04, SEC-07):
//  1. Check global tool_policies — deny wins outright; ask is held as a floor
//  2. Check agent-level tools.deny — if listed, deny (deny always wins)
//  3. Check agent-level tools.allow — if list exists and tool not in it, deny
//  4. If tools.allow is empty array, deny all (explicit empty = no tools)
//  5. If default_policy is "deny" and no explicit allow, deny
func (e *Evaluator) EvaluateTool(agentID, toolName string, agentPolicy ...*AgentPolicy) Decision {
	// Step 0: Check global tool policy (floor constraint).
	// Global "deny" blocks immediately, regardless of agent policy.
	// Global "ask" is the minimum — agent policy can only raise to "deny",
	// not lower back to "allow" (strictest wins).
	globalPolicy := e.resolveGlobalToolPolicy(toolName)
	if globalPolicy == ToolPolicyDeny {
		return Decision{
			Allowed:    false,
			Policy:     string(ToolPolicyDeny),
			PolicyRule: fmt.Sprintf("tool '%s' denied by global tool policy", toolName),
		}
	}

	// Use explicit policy if provided
	if len(agentPolicy) > 0 && agentPolicy[0] != nil {
		d := e.evaluateWithPolicy(agentID, toolName, agentPolicy[0])
		// Elevate to "ask" if global floor requires it and agent policy said "allow".
		if d.Allowed && d.Policy == "" && globalPolicy == ToolPolicyAsk {
			d.Policy = string(ToolPolicyAsk)
		} else if d.Policy == "" {
			d.Policy = string(ToolPolicyAllow)
		}
		return d
	}

	// Look up from config
	if ap, ok := e.agents[agentID]; ok {
		d := e.evaluateWithPolicy(agentID, toolName, &ap)
		if d.Allowed && d.Policy == "" && globalPolicy == ToolPolicyAsk {
			d.Policy = string(ToolPolicyAsk)
		} else if d.Policy == "" {
			d.Policy = string(ToolPolicyAllow)
		}
		return d
	}

	d := e.evaluateDefault(agentID, toolName)
	if d.Allowed && globalPolicy == ToolPolicyAsk {
		d.Policy = string(ToolPolicyAsk)
	} else if d.Allowed {
		d.Policy = string(ToolPolicyAllow)
	} else {
		d.Policy = string(ToolPolicyDeny)
	}
	return d
}

// resolveGlobalToolPolicy returns the effective global policy for a tool name.
// Resolution order mirrors SecurityConfig.ResolveToolPolicy:
// user override → builtin safety default → DefaultToolPolicy → ToolPolicyAllow.
func (e *Evaluator) resolveGlobalToolPolicy(toolName string) ToolPolicy {
	if p, ok := e.toolPolicies[toolName]; ok {
		return p
	}
	if p, ok := builtinToolPolicies[toolName]; ok {
		return p
	}
	if e.defaultToolPolicy != "" {
		return e.defaultToolPolicy
	}
	return ToolPolicyAllow
}

func (e *Evaluator) evaluateWithPolicy(agentID, toolName string, ap *AgentPolicy) Decision {
	deny := ap.effectiveDeny()
	allow := ap.effectiveAllow()

	// Step 1: Check deny list (deny always wins)
	for _, denied := range deny {
		if denied == toolName {
			for _, allowed := range allow {
				if allowed == toolName {
					return Decision{
						Allowed: false,
						Policy:  string(ToolPolicyDeny),
						PolicyRule: fmt.Sprintf(
							"tool '%s' in tools.deny for agent '%s' (deny takes precedence over allow)",
							toolName,
							agentID,
						),
					}
				}
			}
			return Decision{
				Allowed:    false,
				Policy:     string(ToolPolicyDeny),
				PolicyRule: fmt.Sprintf("tool '%s' in tools.deny for agent '%s'", toolName, agentID),
			}
		}
	}

	// Step 2: Check allow list
	if ap.hasAllowList() {
		if len(allow) == 0 {
			return Decision{
				Allowed:    false,
				Policy:     string(ToolPolicyDeny),
				PolicyRule: fmt.Sprintf("tools.allow is empty for agent '%s' (no tools permitted)", agentID),
			}
		}
		for _, allowed := range allow {
			if allowed == toolName {
				return Decision{
					Allowed:    true,
					Policy:     string(ToolPolicyAllow),
					PolicyRule: fmt.Sprintf("tools.allow matched '%s' for agent '%s'", toolName, agentID),
				}
			}
		}
		return Decision{
			Allowed:    false,
			Policy:     string(ToolPolicyDeny),
			PolicyRule: fmt.Sprintf("tool '%s' not in tools.allow for agent '%s'", toolName, agentID),
		}
	}

	return e.evaluateDefault(agentID, toolName)
}

// EvaluateExec checks whether an agent is permitted to execute a command
// against the exec allowlist (SEC-05).
func (e *Evaluator) EvaluateExec(agentID, command string) Decision {
	if len(e.execAllowedBins) == 0 {
		if e.defaultPolicy == PolicyDeny {
			binary := FirstToken(command)
			return Decision{
				Allowed:    false,
				PolicyRule: fmt.Sprintf("binary %q not in exec allowlist (empty allowlist)", binary),
			}
		}
		return Decision{
			Allowed:    true,
			PolicyRule: "exec allowed: no exec allowlist configured, default_policy is 'allow'",
		}
	}

	for _, pat := range e.execAllowedBins {
		if MatchGlob(pat, command) {
			return Decision{
				Allowed:    true,
				PolicyRule: fmt.Sprintf("exec allowed: command matched pattern %q", pat),
			}
		}
	}

	binary := FirstToken(command)
	return Decision{
		Allowed:    false,
		PolicyRule: fmt.Sprintf("binary %q not in exec allowlist", binary),
	}
}

func (e *Evaluator) evaluateDefault(agentID, toolName string) Decision {
	if e.defaultPolicy == PolicyDeny {
		return Decision{
			Allowed: false,
			Policy:  string(ToolPolicyDeny),
			PolicyRule: fmt.Sprintf(
				"default_policy is 'deny', no allow rule for tool '%s' (agent '%s')",
				toolName,
				agentID,
			),
		}
	}

	return Decision{
		Allowed:    true,
		Policy:     string(ToolPolicyAllow),
		PolicyRule: fmt.Sprintf("security.default_policy is 'allow', no deny rule matched for tool '%s'", toolName),
	}
}

// MatchGlob returns true if s matches pattern where '*' matches any substring.
// Exported for use by pkg/security exec allowlist matching.
func MatchGlob(pattern, s string) bool {
	idx := strings.Index(pattern, "*")
	if idx < 0 {
		return pattern == s
	}
	prefix := pattern[:idx]
	suffix := pattern[idx+1:]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	rest := s[len(prefix):]
	if suffix == "" {
		return true
	}
	i := strings.LastIndex(rest, suffix)
	if i < 0 {
		return false
	}
	return MatchGlob(suffix, rest[i:])
}

// FirstToken returns the first space-separated token of s.
// Exported for use by pkg/security exec allowlist matching.
func FirstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// EngineConfig configures the policy engine.
type EngineConfig struct {
	DefaultPolicy string
}

// Engine is a higher-level wrapper around the Evaluator for integration use.
type Engine struct {
	eval *Evaluator
}

// NewEngine creates a policy engine with the given config.
func NewEngine(cfg EngineConfig) *Engine {
	sc := &SecurityConfig{DefaultPolicy: DefaultPolicy(cfg.DefaultPolicy)}
	return &Engine{eval: NewEvaluator(sc)}
}

// Evaluate checks a tool invocation against an explicit agent policy.
func (eng *Engine) Evaluate(agentID, toolName string, agentPolicy *AgentPolicy) Decision {
	return eng.eval.EvaluateTool(agentID, toolName, agentPolicy)
}
