package agent

import (
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/policy"
)

func TestResolveToolPolicy_NilConfig(t *testing.T) {
	// With nil config and nil agentCfg, every tool falls back to allow EXCEPT
	// the baked-in safety defaults (browser.evaluate=deny).
	if got := resolveToolPolicy(nil, nil, "read_file"); got != policy.ToolPolicyAllow {
		t.Errorf("read_file under nil cfg: got %q, want allow", got)
	}
	if got := resolveToolPolicy(nil, nil, "browser.evaluate"); got != policy.ToolPolicyDeny {
		t.Errorf("browser.evaluate under nil cfg: got %q, want deny (builtin safety default)", got)
	}
}

func TestResolveToolPolicy_GlobalDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.DefaultToolPolicy = "deny"
	if got := resolveToolPolicy(cfg, nil, "read_file"); got != policy.ToolPolicyDeny {
		t.Errorf("global default=deny: got %q, want deny", got)
	}

	cfg.Sandbox.DefaultToolPolicy = "ask"
	if got := resolveToolPolicy(cfg, nil, "read_file"); got != policy.ToolPolicyAsk {
		t.Errorf("global default=ask: got %q, want ask", got)
	}
}

func TestResolveToolPolicy_GlobalPerTool(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.ToolPolicies = map[string]string{
		"exec": "deny",
	}
	if got := resolveToolPolicy(cfg, nil, "exec"); got != policy.ToolPolicyDeny {
		t.Errorf("global per-tool deny on exec: got %q, want deny", got)
	}
	if got := resolveToolPolicy(cfg, nil, "read_file"); got != policy.ToolPolicyAllow {
		t.Errorf("untouched tool with no global default: got %q, want allow", got)
	}
}

func TestResolveToolPolicy_PerAgentDeny(t *testing.T) {
	cfg := &config.Config{}
	agentCfg := &config.AgentConfig{
		ID: "custom-1",
		Tools: &config.AgentToolsCfg{
			Builtin: config.AgentBuiltinToolsCfg{
				Policies: map[string]config.ToolPolicy{
					"read_file": config.ToolPolicyDeny,
				},
			},
		},
	}
	if got := resolveToolPolicy(cfg, agentCfg, "read_file"); got != policy.ToolPolicyDeny {
		t.Errorf("per-agent deny on read_file: got %q, want deny", got)
	}
	if got := resolveToolPolicy(cfg, agentCfg, "write_file"); got != policy.ToolPolicyAllow {
		t.Errorf("untouched tool: got %q, want allow", got)
	}
}

func TestResolveToolPolicy_StrictestWins(t *testing.T) {
	// Per-agent allow + global deny → deny (strictest).
	cfg := &config.Config{}
	cfg.Sandbox.ToolPolicies = map[string]string{"exec": "deny"}
	agentCfg := &config.AgentConfig{
		ID: "custom-1",
		Tools: &config.AgentToolsCfg{
			Builtin: config.AgentBuiltinToolsCfg{
				Policies: map[string]config.ToolPolicy{
					"exec": config.ToolPolicyAllow,
				},
			},
		},
	}
	if got := resolveToolPolicy(cfg, agentCfg, "exec"); got != policy.ToolPolicyDeny {
		t.Errorf("global deny + per-agent allow: got %q, want deny (strictest)", got)
	}

	// Per-agent deny + global allow → deny.
	cfg2 := &config.Config{}
	cfg2.Sandbox.ToolPolicies = map[string]string{"exec": "allow"}
	agentCfg2 := &config.AgentConfig{
		ID: "custom-1",
		Tools: &config.AgentToolsCfg{
			Builtin: config.AgentBuiltinToolsCfg{
				Policies: map[string]config.ToolPolicy{
					"exec": config.ToolPolicyDeny,
				},
			},
		},
	}
	if got := resolveToolPolicy(cfg2, agentCfg2, "exec"); got != policy.ToolPolicyDeny {
		t.Errorf("global allow + per-agent deny: got %q, want deny", got)
	}

	// Per-agent ask + global allow → ask (ask beats allow).
	cfg3 := &config.Config{}
	agentCfg3 := &config.AgentConfig{
		ID: "custom-1",
		Tools: &config.AgentToolsCfg{
			Builtin: config.AgentBuiltinToolsCfg{
				Policies: map[string]config.ToolPolicy{
					"exec": config.ToolPolicyAsk,
				},
			},
		},
	}
	if got := resolveToolPolicy(cfg3, agentCfg3, "exec"); got != policy.ToolPolicyAsk {
		t.Errorf("per-agent ask, no global override: got %q, want ask", got)
	}
}

func TestStrictestPolicy(t *testing.T) {
	cases := []struct {
		a, b, want policy.ToolPolicy
	}{
		{policy.ToolPolicyAllow, policy.ToolPolicyAllow, policy.ToolPolicyAllow},
		{policy.ToolPolicyAllow, policy.ToolPolicyAsk, policy.ToolPolicyAsk},
		{policy.ToolPolicyAsk, policy.ToolPolicyAllow, policy.ToolPolicyAsk},
		{policy.ToolPolicyAsk, policy.ToolPolicyAsk, policy.ToolPolicyAsk},
		{policy.ToolPolicyAllow, policy.ToolPolicyDeny, policy.ToolPolicyDeny},
		{policy.ToolPolicyDeny, policy.ToolPolicyAllow, policy.ToolPolicyDeny},
		{policy.ToolPolicyAsk, policy.ToolPolicyDeny, policy.ToolPolicyDeny},
		{policy.ToolPolicyDeny, policy.ToolPolicyDeny, policy.ToolPolicyDeny},
		// Empty / unset → treated as allow.
		{"", policy.ToolPolicyDeny, policy.ToolPolicyDeny},
		{"", "", policy.ToolPolicyAllow},
	}
	for _, tc := range cases {
		if got := strictestPolicy(tc.a, tc.b); got != tc.want {
			t.Errorf("strictestPolicy(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestToolAllowed_DenyMakesInvisible(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.ToolPolicies = map[string]string{"exec": "deny"}
	if toolAllowed(cfg, nil, "exec") {
		t.Error("toolAllowed must return false for denied tool — denied tools must be invisible to LLM")
	}
	if !toolAllowed(cfg, nil, "read_file") {
		t.Error("toolAllowed must return true for tools with no override")
	}
}

func TestToolAllowed_AskStillRegisters(t *testing.T) {
	// "ask" tools register normally — the runtime approval prompt fires at
	// dispatch, not at registration. The LLM still sees them in its tool list.
	cfg := &config.Config{}
	cfg.Sandbox.ToolPolicies = map[string]string{"exec": "ask"}
	if !toolAllowed(cfg, nil, "exec") {
		t.Error("ask-policy tools must still register so the LLM can see them")
	}
}
