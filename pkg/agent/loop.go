// Omnipus - Ultra-lightweight personal AI agent
// Built on Omnipus's foundation. See CLAUDE.md for project lineage.
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/commands"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/constants"
	"github.com/dapicom-ai/omnipus/pkg/logger"
	"github.com/dapicom-ai/omnipus/pkg/media"
	"github.com/dapicom-ai/omnipus/pkg/policy"
	"github.com/dapicom-ai/omnipus/pkg/providers"
	"github.com/dapicom-ai/omnipus/pkg/routing"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
	"github.com/dapicom-ai/omnipus/pkg/security"
	"github.com/dapicom-ai/omnipus/pkg/session"
	"github.com/dapicom-ai/omnipus/pkg/skills"
	"github.com/dapicom-ai/omnipus/pkg/state"
	"github.com/dapicom-ai/omnipus/pkg/sysagent"
	systools "github.com/dapicom-ai/omnipus/pkg/sysagent/tools"
	"github.com/dapicom-ai/omnipus/pkg/taskstore"
	"github.com/dapicom-ai/omnipus/pkg/tools"
	"github.com/dapicom-ai/omnipus/pkg/tools/browser"
	"github.com/dapicom-ai/omnipus/pkg/utils"
	"github.com/dapicom-ai/omnipus/pkg/voice"
)

type AgentLoop struct {
	// Core dependencies
	bus      *bus.MessageBus
	cfg      *config.Config
	registry *AgentRegistry
	state    *state.Manager

	// Event system
	eventBus *EventBus
	hooks    *HookManager

	// Runtime state
	running        atomic.Bool
	summarizing    sync.Map
	fallback       *providers.FallbackChain
	channelManager *channels.Manager
	mediaStore     media.MediaStore
	transcriber    voice.Transcriber
	cmdRegistry    *commands.Registry
	mcp            mcpRuntime
	hookRuntime    hookRuntime
	steering       *steeringQueue
	pendingSkills  sync.Map
	mu             sync.RWMutex

	// Concurrent turn management
	activeTurnStates sync.Map     // key: sessionKey (string), value: *turnState
	subTurnCounter   atomic.Int64 // Counter for generating unique SubTurn IDs

	// Turn tracking
	turnSeq        atomic.Uint64
	activeRequests sync.WaitGroup

	reloadFunc func() error

	// Task management
	taskStore    *taskstore.TaskStore
	taskExecutor *TaskExecutor

	// Security (SEC-15, SEC-17): audit logging and policy evaluation.
	// Initialized in NewAgentLoop when sandbox.audit_log is enabled.
	auditLogger   *audit.Logger
	policyAuditor *policy.PolicyAuditor

	// Wave 2: Kernel-level sandbox backend (SEC-01, SEC-02, SEC-03).
	// Selected at startup via sandbox.SelectBackend: LinuxBackend on Linux
	// 5.13+ (Landlock+seccomp), FallbackBackend elsewhere (cooperative env
	// vars). Applied to every exec child via ExecTool.sandboxBackend.
	sandboxBackend sandbox.SandboxBackend

	// Wave 3: Prompt injection defense (SEC-25). Sanitizes untrusted tool
	// results — web_search, web_fetch, browser_*, read_file — before they
	// enter the LLM's context. Nil when the guard is misconfigured; callers
	// must nil-check. Trusted tool results (exec, spawn, message, etc.) are
	// NEVER sanitized so the LLM sees verbatim user and internal output.
	promptGuard *security.PromptGuard

	// Wave 3: SSRF proxy for exec child processes (SEC-28). Only started when
	// cfg.Tools.Exec.EnableProxy is true. The proxy is idle-stop: it exits
	// after DefaultIdleTimeout (30s) when no commands are active, and is
	// automatically restarted by PrepareCmd() on the next exec command so
	// long-lived agent loops continue to enforce SSRF protection. On initial
	// bind failure this field is nil and exec children run without proxy env
	// vars (degraded mode — LIM-02).
	execProxy *security.ExecProxy

	// Wave 4: Browser automation manager (US-4/US-6/US-7). Nil when browser
	// tools are disabled. Shutdown() is called in AgentLoop.Close().
	browserMgr *browser.BrowserManager

	// Ava agent CRUD deps — stored so WireAvaAgentTools can re-run on hot reload.
	avaDeps *systools.Deps

	// Wave 4: Per-agent rate limiting and global daily cost cap (SEC-26).
	// rateLimiter manages sliding-window counters; costTracker persists the
	// daily cost accumulator across restarts. Both are always non-nil after
	// NewAgentLoop — the registry exists even when no limits are configured
	// so it can record costs for observability. The per-call sites check
	// cfg.Sandbox.RateLimits.* > 0 to decide whether to enforce.
	rateLimiter *security.RateLimiterRegistry
	costTracker *security.CostTracker
}

// processOptions configures how a message is processed
type processOptions struct {
	SessionKey              string                // Session identifier for history/context
	Channel                 string                // Target channel for tool execution
	ChatID                  string                // Target chat ID for tool execution
	SenderID                string                // Current sender ID for dynamic context
	SenderDisplayName       string                // Current sender display name for dynamic context
	UserMessage             string                // User message content (may include prefix)
	ForcedSkills            []string              // Skills explicitly requested for this message
	SystemPromptOverride    string                // Override the default system prompt (Used by SubTurns)
	Media                   []string              // media:// refs from inbound message
	InitialSteeringMessages []providers.Message   // Steering messages from refactor/agent
	DefaultResponse         string                // Response when LLM returns empty
	EnableSummary           bool                  // Whether to trigger summarization
	SendResponse            bool                  // Whether to send response via bus
	SuppressToolFeedback    bool                  // Whether to suppress inline tool feedback messages
	NoHistory               bool                  // If true, don't load session history (for heartbeat)
	SkipInitialSteeringPoll bool                  // If true, skip the steering poll at loop start (used by Continue)
	TranscriptSessionID     string                // Session ID for transcript tool call recording (empty = disabled)
	TranscriptStore         *session.UnifiedStore // Store for transcript tool call recording (nil = disabled)
}

type continuationTarget struct {
	SessionKey string
	Channel    string
	ChatID     string
}

const (
	defaultResponse            = "The model returned an empty response. This may indicate a provider error or token limit."
	toolLimitResponse          = "I've reached `max_tool_iterations` without a final response. Increase `max_tool_iterations` in config.json if this task needs more tool steps."
	handledToolResponseSummary = "Requested output delivered via tool attachment."
	sessionKeyAgentPrefix      = "agent:"
	metadataKeyAccountID       = "account_id"
	metadataKeyGuildID         = "guild_id"
	metadataKeyTeamID          = "team_id"
	metadataKeyParentPeerKind  = "parent_peer_kind"
	metadataKeyParentPeerID    = "parent_peer_id"
)

func NewAgentLoop(
	cfg *config.Config,
	msgBus *bus.MessageBus,
	provider providers.LLMProvider,
) *AgentLoop {
	registry := NewAgentRegistry(cfg, provider)

	// Set up shared fallback chain
	cooldown := providers.NewCooldownTracker()
	fallbackChain := providers.NewFallbackChain(cooldown)

	// Create state manager using default agent's workspace for channel recording
	defaultAgent := registry.GetDefaultAgent()
	var stateManager *state.Manager
	if defaultAgent != nil {
		stateManager = state.NewManager(defaultAgent.Workspace)
	}

	eventBus := NewEventBus()
	al := &AgentLoop{
		bus:         msgBus,
		cfg:         cfg,
		registry:    registry,
		state:       stateManager,
		eventBus:    eventBus,
		summarizing: sync.Map{},
		fallback:    fallbackChain,
		cmdRegistry: commands.NewRegistry(commands.BuiltinDefinitions()),
		steering:    newSteeringQueue(parseSteeringMode(cfg.Agents.Defaults.SteeringMode)),
	}
	al.hooks = NewHookManager(eventBus)
	configureHookManagerFromConfig(al.hooks, cfg)

	// Initialize task store (sibling of workspace: ~/.omnipus/tasks).
	homePath := filepath.Dir(cfg.WorkspacePath())
	al.taskStore = taskstore.New(filepath.Join(homePath, "tasks"))
	al.taskExecutor = newTaskExecutor(al, al.taskStore)

	// SEC-15: Initialize structured audit logging (optional) and policy
	// evaluation (always on). Audit directory is ~/.omnipus/system/ (sibling of
	// workspace). The audit logger and the policy evaluator are decoupled:
	// disabling audit logging must NOT disable enforcement.
	if cfg.Sandbox.AuditLog {
		auditDir := filepath.Join(homePath, "system")
		auditLogger, auditErr := audit.NewLogger(audit.LoggerConfig{
			Dir:           auditDir,
			RetentionDays: 90,
		})
		if auditErr != nil {
			logger.ErrorCF("agent", "Failed to initialize audit logger; audit logging disabled",
				map[string]any{"error": auditErr.Error(), "dir": auditDir})
		} else {
			al.auditLogger = auditLogger

			// Log startup event.
			if err := auditLogger.Log(&audit.Entry{
				Event:    audit.EventStartup,
				Decision: audit.DecisionAllow,
				Details: map[string]any{
					"audit_dir": auditDir,
				},
			}); err != nil {
				logger.ErrorCF("agent", "Failed to write audit startup entry — audit logging may be non-functional",
					map[string]any{"error": err.Error()})
			}

			// Wire audit logger into all agent tool registries.
			for _, agentID := range registry.ListAgentIDs() {
				if agent, ok := registry.GetAgent(agentID); ok {
					agent.Tools.SetAuditLogger(auditLogger)
				}
			}
		}
	}

	// SEC-05/SEC-07: Build the policy evaluator from the live config.
	// `cfg.Tools.Exec.AllowedBinaries` is the single source of truth for the
	// exec allowlist (the same field the UI writes to via
	// /api/v1/security/exec-allowlist). Constructing with an explicit
	// SecurityConfig avoids the deny-everything trap of `NewEvaluator(nil)`.
	//
	// Default policy derivation:
	//   - A non-empty allowlist means the operator opted into SEC-05 binary
	//     restriction — default_policy is "deny" so unlisted binaries are blocked.
	//   - An empty allowlist means no opt-in — default_policy is "allow" so
	//     the existing guardCommand() checks remain the only exec restriction.
	// This preserves backward compatibility for agents that never touched the
	// allowlist, while honoring fail-closed semantics for agents that did.
	defaultPolicy := policy.PolicyAllow
	if len(cfg.Tools.Exec.AllowedBinaries) > 0 {
		defaultPolicy = policy.PolicyDeny
	}
	// Convert global tool policies from config (map[string]string) to the
	// typed map[string]ToolPolicy that SecurityConfig expects.
	var globalToolPolicies map[string]policy.ToolPolicy
	if len(cfg.Sandbox.ToolPolicies) > 0 {
		globalToolPolicies = make(map[string]policy.ToolPolicy, len(cfg.Sandbox.ToolPolicies))
		for k, v := range cfg.Sandbox.ToolPolicies {
			globalToolPolicies[k] = policy.ToolPolicy(v)
		}
	}
	secCfg := &policy.SecurityConfig{
		DefaultPolicy: defaultPolicy,
		Policy: policy.PolicySection{
			Exec: policy.ExecPolicy{
				AllowedBinaries: cfg.Tools.Exec.AllowedBinaries,
				Approval:        cfg.Tools.Exec.Approval,
			},
		},
		ToolPolicies:      globalToolPolicies,
		DefaultToolPolicy: policy.ToolPolicy(cfg.Sandbox.DefaultToolPolicy),
	}
	policyEval := policy.NewEvaluator(secCfg)

	// Wrap the evaluator in a PolicyAuditor so every decision is audit-logged
	// (ADR-002 §W-3). When audit logging is disabled the bridge is nil; the
	// PolicyAuditor tolerates a nil logger and still enforces — enforcement
	// must NOT depend on audit logging being enabled.
	var auditBridgeImpl *auditBridge
	if al.auditLogger != nil {
		auditBridgeImpl = newAuditBridge(al.auditLogger)
	}
	var policyAuditorLogger policy.AuditLogger
	if auditBridgeImpl != nil {
		policyAuditorLogger = auditBridgeImpl
	}
	al.policyAuditor = policy.NewPolicyAuditor(policyEval, policyAuditorLogger, "")

	// SEC-01/02/03: Select the best-available sandbox backend. This never
	// fails: on unsupported kernels SelectBackend returns a FallbackBackend.
	backend, backendName := sandbox.SelectBackend()
	al.sandboxBackend = backend
	logger.InfoCF("agent", "Sandbox backend selected", map[string]any{"backend": backendName})

	// SEC-25: Initialize the prompt-injection guard. NewPromptGuardFromConfig
	// defaults to "medium" strictness when the field is empty. Construction
	// is cheap and cannot fail, so we always build it — runTurn checks the
	// untrusted-tool allowlist before invoking it, so trusted results are
	// never sanitized even when the guard is non-nil.
	al.promptGuard = security.NewPromptGuardFromConfig(policy.PromptGuardConfig{
		Strictness: cfg.Sandbox.PromptInjectionLevel,
	})
	logger.InfoCF("agent", "Prompt guard initialized",
		map[string]any{"strictness": string(al.promptGuard.Strictness())})

	// SEC-28: Start the loopback SSRF proxy for exec child processes when
	// enabled. On bind failure we log and fall back to degraded mode (child
	// processes run without HTTP_PROXY env vars — LIM-02) rather than
	// failing startup, because exec is a core tool and a proxy bind failure
	// on a shared port should not take the whole agent loop down.
	if cfg.Tools.Exec.EnableProxy {
		ssrfChecker := security.NewSSRFChecker(nil)
		proxy := security.NewExecProxy(ssrfChecker, nil)
		if err := proxy.Start(); err != nil {
			logger.ErrorCF("agent", "Failed to start exec SSRF proxy; child processes will run without proxy env vars",
				map[string]any{"error": err.Error()})
		} else {
			al.execProxy = proxy
			logger.InfoCF("agent", "Exec SSRF proxy started",
				map[string]any{"addr": proxy.Addr()})
		}
	}

	// SEC-26: Initialize rate limiter registry and persistent cost tracker.
	// The registry always exists so per-agent windows can be created even when
	// no cap is configured; SetDailyCostCap(0) disables cost-cap enforcement.
	al.rateLimiter = security.NewRateLimiterRegistry()
	al.rateLimiter.SetDailyCostCap(cfg.Sandbox.RateLimits.DailyCostCapUSD)
	costPath := filepath.Join(homePath, "system", "cost.json")
	al.costTracker = security.NewCostTracker(costPath)
	al.costTracker.LoadIntoRegistry(al.rateLimiter)
	logger.InfoCF("agent", "Rate limiter initialized",
		map[string]any{
			"daily_cost_cap_usd":              cfg.Sandbox.RateLimits.DailyCostCapUSD,
			"max_agent_llm_calls_per_hour":    cfg.Sandbox.RateLimits.MaxAgentLLMCallsPerHour,
			"max_agent_tool_calls_per_minute": cfg.Sandbox.RateLimits.MaxAgentToolCallsPerMinute,
		})

	// Register shared tools to all agents (now that al is created)
	registerSharedTools(al, cfg, msgBus, registry, provider)

	// Wave 2: replace the exec tool in each agent's registry with a version
	// that has the policy auditor and sandbox backend wired in. Registering
	// the same tool name overwrites the previous entry (see ToolRegistry.Register).
	al.wireExecToolDeps()

	return al
}

// AuditLogger returns the audit logger, or nil if audit logging is disabled.
// Used by gateway handlers that need to log policy changes.
func (al *AgentLoop) AuditLogger() *audit.Logger {
	if al == nil {
		return nil
	}
	return al.auditLogger
}

// ExecProxy returns the SEC-28 SSRF proxy for exec child processes, or nil
// when the proxy is disabled or failed to bind. Used by gateway handlers that
// report the proxy status and by tests that exercise the proxy lifecycle.
func (al *AgentLoop) ExecProxy() *security.ExecProxy {
	if al == nil {
		return nil
	}
	return al.execProxy
}

// PromptGuard returns the SEC-25 prompt-injection guard. Always non-nil after
// NewAgentLoop — even when no config field is set, the factory returns a
// medium-strictness guard. Used by runTurn and by gateway status handlers.
func (al *AgentLoop) PromptGuard() *security.PromptGuard {
	if al == nil {
		return nil
	}
	return al.promptGuard
}

// RateLimiter returns the SEC-26 rate limiter registry. Always non-nil after
// NewAgentLoop. Used by runTurn for per-agent limit checks and by gateway
// handlers that report the current rate limit / cost status.
func (al *AgentLoop) RateLimiter() *security.RateLimiterRegistry {
	if al == nil {
		return nil
	}
	return al.rateLimiter
}

// SandboxBackend returns the active sandbox backend, or nil if sandboxing is
// disabled. Used by gateway handlers that report sandbox status.
func (al *AgentLoop) SandboxBackend() sandbox.SandboxBackend {
	if al == nil {
		return nil
	}
	return al.sandboxBackend
}

// recordRateLimitDenial writes an audit entry and emits a RateLimit event for
// a denied rate-limit or cost-cap check (SEC-26). Centralizing this avoids
// repeating the same audit + emit boilerplate for each of the three checks
// (LLM calls, tool calls, global cost cap). extraDetails is merged into the
// audit entry's Details map under a "limit_type" key and caller-supplied
// fields. Audit failures are logged at warn level and swallowed — a rate-limit
// denial must still be reported to the caller even when the audit logger is
// unhealthy.
func (al *AgentLoop) recordRateLimitDenial(
	ts *turnState,
	limitType string,
	payload RateLimitPayload,
	extraDetails map[string]any,
) {
	if al.auditLogger != nil {
		details := map[string]any{"limit_type": limitType}
		for k, v := range extraDetails {
			details[k] = v
		}
		if err := al.auditLogger.Log(&audit.Entry{
			Event:      audit.EventRateLimit,
			Decision:   audit.DecisionDeny,
			AgentID:    ts.agent.ID,
			Tool:       payload.Tool,
			PolicyRule: payload.PolicyRule,
			Details:    details,
		}); err != nil {
			logger.WarnCF("agent", "failed to write rate-limit audit entry",
				map[string]any{"limit_type": limitType, "error": err.Error()})
		}
	}
	al.emitEvent(
		EventKindRateLimit,
		ts.eventMeta("runTurn", "turn.rate_limit"),
		payload,
	)
}

// wireExecToolDeps replaces each agent's exec tool with one constructed via
// NewExecToolWithDeps, injecting the policy auditor (SEC-05) and the sandbox
// backend (SEC-01/02/03). This runs after NewAgentInstance has created the
// default exec tool so that all other tool setup (deny patterns, allow paths,
// timeouts) is preserved — we only add the Wave 2 security deps on top.
//
// No-op when the agent has exec disabled or when the registry lookup fails.
func (al *AgentLoop) wireExecToolDeps() {
	if al.registry == nil {
		return
	}
	cfg := al.cfg
	if cfg == nil || !cfg.Tools.IsToolEnabled("exec") {
		return
	}
	allowReadPaths := buildAllowReadPatterns(cfg)

	for _, agentID := range al.registry.ListAgentIDs() {
		agent, ok := al.registry.GetAgent(agentID)
		if !ok || agent == nil || agent.Tools == nil {
			continue
		}

		// Workspace-scoped sandbox policy: allow read/write/execute under
		// the agent's workspace. Landlock inherits to children natively on
		// Linux 5.13+, so no per-child application is actually required
		// there — the fallback backend still uses this to emit
		// OMNIPUS_SANDBOX_PATHS for cooperative scripts.
		policy := sandbox.SandboxPolicy{
			FilesystemRules: []sandbox.PathRule{
				{
					Path:   agent.Workspace,
					Access: sandbox.AccessRead | sandbox.AccessWrite | sandbox.AccessExecute,
				},
			},
			InheritToChildren: true,
		}

		deps := tools.ExecToolDeps{
			SandboxPolicy: policy,
		}
		// Both dependency fields use interfaces, so we must nil-guard at
		// assignment time to avoid typed-nil traps: storing a nil
		// *policy.PolicyAuditor or nil sandbox.SandboxBackend in an interface
		// field would create a non-nil interface holding a nil pointer,
		// defeating downstream `!= nil` checks and causing nil-pointer panics.
		if al.policyAuditor != nil {
			deps.PolicyAuditor = al.policyAuditor
		}
		if al.sandboxBackend != nil {
			deps.SandboxBackend = al.sandboxBackend
		}
		// SEC-28: Hand the exec proxy to the tool so it can inject
		// HTTP_PROXY env vars on every child. nil-guarded at assignment
		// time to avoid the typed-nil-in-interface trap.
		if al.execProxy != nil {
			deps.ExecProxy = al.execProxy
		}

		restrict := cfg.Agents.Defaults.RestrictToWorkspace
		execTool, err := tools.NewExecToolWithDeps(agent.Workspace, restrict, cfg, deps, allowReadPaths)
		if err != nil {
			// Fail closed: if Wave 2 security wiring fails, remove the exec
			// tool from the registry entirely. The agent will lose exec
			// capability but cannot run commands without the security layer.
			logger.ErrorCF("agent", "Failed to wire exec tool deps; removing exec tool (fail closed)",
				map[string]any{"agent_id": agentID, "error": err.Error()})
			agent.Tools.Unregister("exec")
			continue
		}
		agent.Tools.Register(execTool)
	}
}

// registerSharedTools registers tools that are shared across all agents.
func registerSharedTools(
	al *AgentLoop,
	cfg *config.Config,
	msgBus *bus.MessageBus,
	registry *AgentRegistry,
	provider providers.LLMProvider,
) {
	allowReadPaths := buildAllowReadPatterns(cfg)

	for _, agentID := range registry.ListAgentIDs() {
		agent, ok := registry.GetAgent(agentID)
		if !ok {
			continue
		}

		if cfg.Tools.IsToolEnabled("web") {
			searchTool, err := tools.NewWebSearchTool(tools.WebSearchToolOptions{
				BraveAPIKeys:          braveKeys(cfg.Tools.Web.Brave.APIKey()),
				BraveMaxResults:       cfg.Tools.Web.Brave.MaxResults,
				BraveEnabled:          cfg.Tools.Web.Brave.Enabled,
				TavilyAPIKeys:         tavilyKeys(cfg.Tools.Web.Tavily.APIKey()),
				TavilyBaseURL:         cfg.Tools.Web.Tavily.BaseURL,
				TavilyMaxResults:      cfg.Tools.Web.Tavily.MaxResults,
				TavilyEnabled:         cfg.Tools.Web.Tavily.Enabled,
				DuckDuckGoMaxResults:  cfg.Tools.Web.DuckDuckGo.MaxResults,
				DuckDuckGoEnabled:     cfg.Tools.Web.DuckDuckGo.Enabled,
				PerplexityAPIKeys:     perplexityKeys(cfg.Tools.Web.Perplexity.APIKey()),
				PerplexityMaxResults:  cfg.Tools.Web.Perplexity.MaxResults,
				PerplexityEnabled:     cfg.Tools.Web.Perplexity.Enabled,
				SearXNGBaseURL:        cfg.Tools.Web.SearXNG.BaseURL,
				SearXNGMaxResults:     cfg.Tools.Web.SearXNG.MaxResults,
				SearXNGEnabled:        cfg.Tools.Web.SearXNG.Enabled,
				GLMSearchAPIKey:       cfg.Tools.Web.GLMSearch.APIKey(),
				GLMSearchBaseURL:      cfg.Tools.Web.GLMSearch.BaseURL,
				GLMSearchEngine:       cfg.Tools.Web.GLMSearch.SearchEngine,
				GLMSearchMaxResults:   cfg.Tools.Web.GLMSearch.MaxResults,
				GLMSearchEnabled:      cfg.Tools.Web.GLMSearch.Enabled,
				BaiduSearchAPIKey:     cfg.Tools.Web.BaiduSearch.APIKey(),
				BaiduSearchBaseURL:    cfg.Tools.Web.BaiduSearch.BaseURL,
				BaiduSearchMaxResults: cfg.Tools.Web.BaiduSearch.MaxResults,
				BaiduSearchEnabled:    cfg.Tools.Web.BaiduSearch.Enabled,
				Proxy:                 cfg.Tools.Web.Proxy,
			})
			if err != nil {
				logger.ErrorCF("agent", "Failed to create web search tool", map[string]any{"error": err.Error()})
			} else if searchTool != nil {
				agent.Tools.Register(searchTool)
			}
		}
		if cfg.Tools.IsToolEnabled("web_fetch") {
			fetchTool, err := tools.NewWebFetchToolWithProxy(
				50000,
				cfg.Tools.Web.Proxy,
				cfg.Tools.Web.Format,
				cfg.Tools.Web.FetchLimitBytes,
				cfg.Tools.Web.PrivateHostWhitelist)
			if err != nil {
				logger.ErrorCF("agent", "Failed to create web fetch tool", map[string]any{"error": err.Error()})
			} else {
				agent.Tools.Register(fetchTool)
			}
		}

		// Hardware tools (I2C, SPI) - Linux only, returns error on other platforms
		if cfg.Tools.IsToolEnabled("i2c") {
			agent.Tools.Register(tools.NewI2CTool())
		}
		if cfg.Tools.IsToolEnabled("spi") {
			agent.Tools.Register(tools.NewSPITool())
		}

		// Message tool
		if cfg.Tools.IsToolEnabled("message") {
			messageTool := tools.NewMessageTool()
			messageTool.SetSendCallback(func(channel, chatID, content string) error {
				pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer pubCancel()
				return msgBus.PublishOutbound(pubCtx, bus.OutboundMessage{
					Channel: channel,
					ChatID:  chatID,
					Content: content,
				})
			})
			agent.Tools.Register(messageTool)
		}

		// Send file tool (outbound media via MediaStore — store injected later by SetMediaStore)
		if cfg.Tools.IsToolEnabled("send_file") {
			sendFileTool := tools.NewSendFileTool(
				agent.Workspace,
				cfg.Agents.Defaults.RestrictToWorkspace,
				cfg.Agents.Defaults.GetMaxMediaSize(),
				nil,
				allowReadPaths,
			)
			agent.Tools.Register(sendFileTool)
		}

		// Skill discovery and installation tools
		skillsEnabled := cfg.Tools.IsToolEnabled("skills")
		findSkillsEnabled := cfg.Tools.IsToolEnabled("find_skills")
		installSkillsEnabled := cfg.Tools.IsToolEnabled("install_skill")
		if skillsEnabled && (findSkillsEnabled || installSkillsEnabled) {
			clawHubConfig := cfg.Tools.Skills.Registries.ClawHub
			registryMgr := skills.NewRegistryManagerFromConfig(skills.RegistryConfig{
				MaxConcurrentSearches: cfg.Tools.Skills.MaxConcurrentSearches,
				ClawHub: skills.ClawHubConfig{
					Enabled:         clawHubConfig.Enabled,
					BaseURL:         clawHubConfig.BaseURL,
					AuthToken:       os.Getenv(clawHubConfig.AuthTokenRef),
					SearchPath:      clawHubConfig.SearchPath,
					SkillsPath:      clawHubConfig.SkillsPath,
					DownloadPath:    clawHubConfig.DownloadPath,
					Timeout:         clawHubConfig.Timeout,
					MaxZipSize:      clawHubConfig.MaxZipSize,
					MaxResponseSize: clawHubConfig.MaxResponseSize,
				},
			})

			if findSkillsEnabled {
				searchCache := skills.NewSearchCache(
					cfg.Tools.Skills.SearchCache.MaxSize,
					time.Duration(cfg.Tools.Skills.SearchCache.TTLSeconds)*time.Second,
				)
				agent.Tools.Register(tools.NewFindSkillsTool(registryMgr, searchCache))
			}

			if installSkillsEnabled {
				agent.Tools.Register(tools.NewInstallSkillTool(registryMgr, agent.Workspace))
			}
		}

		// Spawn and spawn_status tools share a SubagentManager.
		// Construct it when either tool is enabled (both require subagent).
		spawnEnabled := cfg.Tools.IsToolEnabled("spawn")
		spawnStatusEnabled := cfg.Tools.IsToolEnabled("spawn_status")
		if (spawnEnabled || spawnStatusEnabled) && cfg.Tools.IsToolEnabled("subagent") {
			subagentManager := tools.NewSubagentManager(provider, agent.Model, agent.Workspace)
			subagentManager.SetLLMOptions(agent.MaxTokens, agent.Temperature)

			// Set the spawner that links into AgentLoop's turnState
			subagentManager.SetSpawner(func(
				ctx context.Context,
				task, label, targetAgentID string,
				tls *tools.ToolRegistry,
				maxTokens int,
				temperature float64,
				hasMaxTokens, hasTemperature bool,
			) (*tools.ToolResult, error) {
				// 1. Recover parent Turn State from Context
				parentTS := turnStateFromContext(ctx)
				if parentTS == nil {
					// Fallback: If no turnState exists in context, create an isolated ad-hoc root turn state
					// so that the tool can still function outside of an agent loop (e.g. tests, raw invocations).
					// M2: log a warning when no real turnState is in context — this usually
					// means spawn was called outside of an agent loop (e.g. tests or raw
					// invocations). The ad-hoc state is functional but has no session.
					logger.WarnCF("agent", "Spawn callback using ad-hoc turnState: no parent turnState in context", nil)
					parentTS = &turnState{
						ctx:            ctx,
						turnID:         "adhoc-root",
						depth:          0,
						session:        nil, // Ephemeral session not needed for adhoc spawn
						pendingResults: make(chan *tools.ToolResult, 16),
						concurrencySem: make(chan struct{}, 5),
					}
				}

				// 2. Build Tools slice from registry
				var tlSlice []tools.Tool
				for _, name := range tls.List() {
					if t, ok := tls.Get(name); ok {
						tlSlice = append(tlSlice, t)
					}
				}

				// 3. System Prompt
				systemPrompt := "You are a subagent. Complete the given task independently and report the result.\n" +
					"You have access to tools - use them as needed to complete your task.\n" +
					"After completing the task, provide a clear summary of what was done.\n\n" +
					"Task: " + task

				// 4. Resolve Model
				modelToUse := agent.Model
				if targetAgentID != "" {
					if targetAgent, ok := al.GetRegistry().GetAgent(targetAgentID); ok {
						modelToUse = targetAgent.Model
					}
				}

				// 5. Build SubTurnConfig
				cfg := SubTurnConfig{
					Model:        modelToUse,
					Tools:        tlSlice,
					SystemPrompt: systemPrompt,
				}
				if hasMaxTokens {
					cfg.MaxTokens = maxTokens
				}

				// 6. Spawn SubTurn
				return spawnSubTurn(ctx, al, parentTS, cfg)
			})

			// Clone the parent's tool registry so subagents can use all
			// tools registered so far (file, web, etc.) but NOT spawn/
			// spawn_status which are added below — preventing recursive
			// subagent spawning.
			subagentManager.SetTools(agent.Tools.Clone())
			if spawnEnabled {
				spawnTool := tools.NewSpawnTool(subagentManager)
				spawnTool.SetSpawner(NewSubTurnSpawner(al))
				currentAgentID := agentID
				spawnTool.SetAllowlistChecker(func(targetAgentID string) bool {
					return registry.CanSpawnSubagent(currentAgentID, targetAgentID)
				})

				agent.Tools.Register(spawnTool)

				// Also register the synchronous subagent tool
				subagentTool := tools.NewSubagentTool(subagentManager)
				subagentTool.SetSpawner(NewSubTurnSpawner(al))
				agent.Tools.Register(subagentTool)
			}
			if spawnStatusEnabled {
				agent.Tools.Register(tools.NewSpawnStatusTool(subagentManager))
			}
		} else if (spawnEnabled || spawnStatusEnabled) && !cfg.Tools.IsToolEnabled("subagent") {
			logger.WarnCF("agent", "spawn/spawn_status tools require subagent to be enabled", nil)
		}

		// Task tools — require a task store (available after first NewAgentLoop call).
		if al.taskStore != nil {
			currentAgentID := agentID
			agentCfg := findAgentConfig(cfg, currentAgentID)

			if cfg.Tools.IsToolEnabled("task_list") {
				agent.Tools.Register(tools.NewTaskListTool(al.taskStore))
			}
			if cfg.Tools.IsToolEnabled("task_create") {
				t := tools.NewTaskCreateTool(al.taskStore)
				t.SetDelegateChecker(buildDelegateChecker(agentCfg, cfg.Agents.Defaults))
				agent.Tools.Register(t)
			}
			if cfg.Tools.IsToolEnabled("task_update") {
				t := tools.NewTaskUpdateTool(al.taskStore)
				if al.taskExecutor != nil {
					t.SetOnComplete(al.taskExecutor.onTaskComplete)
				}
				agent.Tools.Register(t)
			}
			agent.Tools.Register(tools.NewTaskDeleteTool(al.taskStore))
			agent.Tools.Register(tools.NewAgentListTool(func() []tools.AgentInfo {
				var infos []tools.AgentInfo
				for _, id := range registry.ListAgentIDs() {
					if a, ok := registry.GetAgent(id); ok {
						infos = append(infos, tools.AgentInfo{ID: a.ID, Name: a.Name, Type: "custom"})
					}
				}
				return infos
			}))
		}

		// Browser automation tools (Wave 4, US-4/US-6/US-7; see wave4-whatsapp-browser-spec.md).
		if cfg.Tools.IsToolEnabled("browser") {
			browserCfg, cfgErr := browser.DefaultConfig()
			if cfgErr != nil {
				logger.ErrorCF("agent", "Browser tools: cannot determine defaults — skipping",
					map[string]any{"error": cfgErr.Error()})
			} else {
				browserCfg.Enabled = true
				// DefaultConfig sets Headless=true; only override if config explicitly sets fields.
				if cfg.Tools.Browser.CDPURL != "" {
					browserCfg.CDPURL = cfg.Tools.Browser.CDPURL
				}
				if cfg.Tools.Browser.PageTimeoutSec > 0 {
					browserCfg.PageTimeout = time.Duration(cfg.Tools.Browser.PageTimeoutSec) * time.Second
				}
				if cfg.Tools.Browser.MaxTabs > 0 {
					browserCfg.MaxTabs = cfg.Tools.Browser.MaxTabs
				}
				if cfg.Tools.Browser.ProfileDir != "" {
					browserCfg.ProfileDir = cfg.Tools.Browser.ProfileDir
				}
				browserCfg.PersistSession = cfg.Tools.Browser.PersistSession

				ssrfChecker := security.NewSSRFChecker(nil)
				mgr, regErr := browser.RegisterTools(agent.Tools, browserCfg, ssrfChecker)
				// browser.evaluate runs arbitrary JS — deny by default (SEC-04/SEC-06).
				// Requires explicit opt-in via tools.browser.evaluate_enabled.
				if !cfg.Tools.Browser.EvaluateEnabled {
					agent.Tools.Unregister("browser.evaluate")
				}
				if regErr != nil {
					logger.ErrorCF("agent", "Failed to register browser tools — "+
						"ensure Chromium/Chrome is installed or set tools.browser.cdp_url",
						map[string]any{"error": regErr.Error()})
				} else {
					al.mu.Lock()
					if al.browserMgr != nil {
						al.browserMgr.Shutdown()
					}
					al.browserMgr = mgr
					al.mu.Unlock()
				}
			}
		}
	}
}

// findAgentConfig returns the AgentConfig for the given agent ID, or nil if not found.
func findAgentConfig(cfg *config.Config, agentID string) *config.AgentConfig {
	for i := range cfg.Agents.List {
		if cfg.Agents.List[i].ID == agentID {
			return &cfg.Agents.List[i]
		}
	}
	return nil
}

// buildDelegateChecker returns a function that checks whether delegation from agentCfg
// to a target agent is allowed.  Supports the "*" wildcard.
func buildDelegateChecker(agentCfg *config.AgentConfig, defaults config.AgentDefaults) func(string) bool {
	var allowList []string
	if agentCfg != nil && len(agentCfg.CanDelegateTo) > 0 {
		allowList = agentCfg.CanDelegateTo
	} else {
		allowList = defaults.CanDelegateTo
	}

	return func(targetAgentID string) bool {
		for _, allowed := range allowList {
			if allowed == "*" || allowed == targetAgentID {
				return true
			}
		}
		return false
	}
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)

	if err := al.ensureHooksInitialized(ctx); err != nil {
		return err
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return err
	}

	idleTicker := time.NewTicker(100 * time.Millisecond)
	defer idleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-idleTicker.C:
			if !al.running.Load() {
				return nil
			}
		case msg, ok := <-al.bus.InboundChan():
			if !ok {
				return nil
			}

			// Start a goroutine that drains the bus while processMessage is
			// running. Only messages that resolve to the active turn scope are
			// redirected into steering; other inbound messages are requeued.
			drainCancel := func() {}
			if activeScope, activeAgentID, ok := al.resolveSteeringTarget(msg); ok {
				drainCtx, cancel := context.WithCancel(ctx)
				drainCancel = cancel
				go al.drainBusToSteering(drainCtx, activeScope, activeAgentID)
			}

			// Process message
			func() {
				defer func() {
					if al.channelManager != nil {
						al.channelManager.InvokeTypingStop(msg.Channel, msg.ChatID)
					}
				}()
				// TODO(media-cleanup): Media file cleanup is disabled. Track in issue backlog.

				drainCanceled := false
				cancelDrain := func() {
					if drainCanceled {
						return
					}
					drainCancel()
					drainCanceled = true
				}
				defer cancelDrain()

				// FR-004: Deferred response guard — guarantees finalResponse is
				// published even when post-turn steering code returns early.
				// published is set to true at each existing publishResponseIfNeeded
				// call site so the defer does not double-publish.
				// publishResponseIfNeeded already checks HasSentInRound() internally,
				// so we do not need to duplicate that check here.
				var finalResponse string
				var activeAgent *AgentInstance
				published := false
				publishChannel := msg.Channel
				publishChatID := msg.ChatID
				defer func() {
					// H2: recover from any panic in the deferred response guard so a
					// programming error here doesn't silently kill the Run goroutine.
					defer func() {
						if r := recover(); r != nil {
							logger.ErrorCF("agent", "panic in deferred response guard", map[string]any{"panic": r})
						}
					}()
					if finalResponse != "" && !published {
						al.publishResponseIfNeeded(ctx, activeAgent, publishChannel, publishChatID, finalResponse)
					}
				}()

				response, activeAgent, err := al.processMessage(ctx, msg)
				if err != nil {
					response = fmt.Sprintf("Error processing message: %v", err)
				}
				finalResponse = response

				target, targetErr := al.buildContinuationTarget(msg)
				if targetErr != nil {
					logger.WarnCF("agent", "Failed to build steering continuation target",
						map[string]any{
							"channel": msg.Channel,
							"error":   targetErr.Error(),
						})
					// defer will publish finalResponse if non-empty
					return
				}
				if target == nil {
					cancelDrain()
					if finalResponse != "" {
						published = true
						al.publishResponseIfNeeded(ctx, activeAgent, msg.Channel, msg.ChatID, finalResponse)
					}
					return
				}

				// Update the defer's publish target to use the resolved continuation target.
				publishChannel = target.Channel
				publishChatID = target.ChatID

				for al.pendingSteeringCountForScope(target.SessionKey) > 0 {
					logger.InfoCF("agent", "Continuing queued steering after turn end",
						map[string]any{
							"channel":     target.Channel,
							"chat_id":     target.ChatID,
							"session_key": target.SessionKey,
							"queue_depth": al.pendingSteeringCountForScope(target.SessionKey),
						})

					continued, continueErr := al.Continue(ctx, target.SessionKey, target.Channel, target.ChatID)
					if continueErr != nil {
						logger.WarnCF("agent", "Failed to continue queued steering",
							map[string]any{
								"channel": target.Channel,
								"chat_id": target.ChatID,
								"error":   continueErr.Error(),
							})
						// defer will publish the last known finalResponse
						return
					}
					if continued == "" {
						// defer will publish the last known finalResponse
						return
					}

					finalResponse = continued
					published = false // Continue returned new content; deferred publish must fire
				}

				cancelDrain()

				for al.pendingSteeringCountForScope(target.SessionKey) > 0 {
					logger.InfoCF("agent", "Draining steering queued during turn shutdown",
						map[string]any{
							"channel":     target.Channel,
							"chat_id":     target.ChatID,
							"session_key": target.SessionKey,
							"queue_depth": al.pendingSteeringCountForScope(target.SessionKey),
						})

					continued, continueErr := al.Continue(ctx, target.SessionKey, target.Channel, target.ChatID)
					if continueErr != nil {
						logger.WarnCF("agent", "Failed to continue queued steering after shutdown drain",
							map[string]any{
								"channel": target.Channel,
								"chat_id": target.ChatID,
								"error":   continueErr.Error(),
							})
						// defer will publish the last known finalResponse
						return
					}
					if continued == "" {
						break
					}

					finalResponse = continued
				}

				if finalResponse != "" {
					published = true
					al.publishResponseIfNeeded(ctx, activeAgent, target.Channel, target.ChatID, finalResponse)
				}
			}()
		}
	}
}

// drainBusToSteering consumes inbound messages and redirects messages from the
// active scope into the steering queue. Messages from other scopes are buffered
// locally and re-published to the inbound bus after the drain loop exits, so
// they are processed normally once the active turn finishes. It drains all
// immediately available messages, blocking for the first one until ctx is done.
func (al *AgentLoop) drainBusToSteering(ctx context.Context, activeScope, activeAgentID string) {
	var deferred []bus.InboundMessage

	// After the loop exits, re-publish buffered out-of-scope messages so the
	// main runAgentLoop can process them once the active turn has finished.
	defer func() {
		for _, m := range deferred {
			if err := al.requeueInboundMessage(m); err != nil {
				// Message loss during shutdown is acceptable: the bus is closing and
				// the message cannot be delivered. The error is logged for observability.
				logger.WarnCF("agent", "Failed to requeue non-steering inbound message", map[string]any{
					"error":     err.Error(),
					"channel":   m.Channel,
					"sender_id": m.SenderID,
				})
			}
		}
	}()

	blocking := true
	for {
		var msg bus.InboundMessage

		if blocking {
			// Block waiting for the first available message or ctx cancellation.
			select {
			case <-ctx.Done():
				return
			case m, ok := <-al.bus.InboundChan():
				if !ok {
					return
				}
				msg = m
			}
		} else {
			// Non-blocking: drain any remaining queued messages, return when empty.
			select {
			case m, ok := <-al.bus.InboundChan():
				if !ok {
					return
				}
				msg = m
			default:
				return
			}
		}
		blocking = false

		msgScope, _, scopeOK := al.resolveSteeringTarget(msg)
		if !scopeOK || msgScope != activeScope {
			// Buffer for re-publishing after the drain exits, so runAgentLoop
			// picks them up without the drain goroutine looping on them.
			deferred = append(deferred, msg)
			continue
		}

		// Transcribe audio if needed before steering, so the agent sees text.
		msg, _ = al.transcribeAudioInMessage(ctx, msg) // errors handled internally with logging

		logger.InfoCF("agent", "Redirecting inbound message to steering queue",
			map[string]any{
				"channel":     msg.Channel,
				"sender_id":   msg.SenderID,
				"content_len": len(msg.Content),
				"scope":       activeScope,
			})

		if err := al.enqueueSteeringMessage(activeScope, activeAgentID, providers.Message{
			Role:    "user",
			Content: msg.Content,
			Media:   append([]string(nil), msg.Media...),
		}); err != nil {
			logger.WarnCF("agent", "Failed to steer message, will be lost",
				map[string]any{
					"error":   err.Error(),
					"channel": msg.Channel,
				})
			// Notify the user that their message could not be queued.
			errCtx, errCancel := context.WithTimeout(ctx, 3*time.Second)
			if pubErr := al.bus.PublishOutbound(errCtx, bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: "Your message could not be queued because the agent is busy. Please try again.",
			}); pubErr != nil {
				logger.WarnCF("agent", "Failed to send steering-queue-full error to user",
					map[string]any{"channel": msg.Channel, "error": pubErr.Error()})
			}
			errCancel()
		}
	}
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
}

func (al *AgentLoop) publishResponseIfNeeded(ctx context.Context, ag *AgentInstance, channel, chatID, response string) {
	if response == "" {
		return
	}

	alreadySent := false
	if ag == nil {
		ag = al.GetRegistry().GetDefaultAgent()
	}
	if ag != nil {
		if tool, ok := ag.Tools.Get("message"); ok {
			if mt, ok := tool.(*tools.MessageTool); ok {
				alreadySent = mt.HasSentInRound()
			}
		}
	}

	if alreadySent {
		logger.DebugCF(
			"agent",
			"Skipped outbound (message tool already sent)",
			map[string]any{"channel": channel},
		)
		return
	}

	if err := al.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: response,
	}); err != nil {
		logger.ErrorCF("agent", "Failed to publish outbound response",
			map[string]any{"channel": channel, "chat_id": chatID, "error": err.Error()})
		return
	}
	logger.InfoCF("agent", "Published outbound response",
		map[string]any{
			"channel":     channel,
			"chat_id":     chatID,
			"content_len": len(response),
		})
}

func (al *AgentLoop) buildContinuationTarget(msg bus.InboundMessage) (*continuationTarget, error) {
	if msg.Channel == "system" {
		return nil, nil
	}

	route, _, err := al.resolveMessageRoute(msg)
	if err != nil {
		return nil, err
	}

	return &continuationTarget{
		SessionKey: resolveScopeKey(route, msg.SessionKey),
		Channel:    msg.Channel,
		ChatID:     msg.ChatID,
	}, nil
}

// WaitForActiveRequests blocks until all in-flight LLM calls tracked by
// activeRequests have completed. Used by the graceful shutdown sequence to
// ensure active turns finish before the process exits.
func (al *AgentLoop) WaitForActiveRequests() {
	al.activeRequests.Wait()
}

// Close releases resources held by agent session stores. Call after Stop.
func (al *AgentLoop) Close() {
	// Shutdown the browser manager (if any) to kill the Chromium subprocess.
	al.mu.Lock()
	if al.browserMgr != nil {
		al.browserMgr.Shutdown()
		al.browserMgr = nil
	}
	al.mu.Unlock()

	mcpManager := al.mcp.takeManager()

	if mcpManager != nil {
		if err := mcpManager.Close(); err != nil {
			logger.ErrorCF("agent", "Failed to close MCP manager",
				map[string]any{
					"error": err.Error(),
				})
		}
	}

	al.GetRegistry().Close()
	if al.hooks != nil {
		al.hooks.Close()
	}
	if al.eventBus != nil {
		al.eventBus.Close()
	}

	// SEC-28: Stop the exec SSRF proxy (idle auto-stop may have already
	// stopped it, but Stop() is idempotent and safe to call either way).
	if al.execProxy != nil {
		al.execProxy.Stop()
	}

	// SEC-26: Persist the accumulated daily cost so the next startup can
	// restore it via LoadIntoRegistry, preventing double-counting on restarts.
	// A save failure here means the cap will under-count after the next
	// restart — worth an Error-level log plus the daily total so operators
	// can reconcile manually.
	if al.costTracker != nil && al.rateLimiter != nil {
		if err := al.costTracker.SaveFromRegistry(al.rateLimiter); err != nil {
			logger.ErrorCF(
				"agent",
				"SEC-26: failed to persist daily cost on shutdown — cap may under-count after restart",
				map[string]any{
					"error":          err.Error(),
					"daily_cost_usd": al.rateLimiter.GetDailyCost(),
				},
			)
		}
	}

	// SEC-15: Log shutdown event and close audit logger.
	if al.auditLogger != nil {
		if err := al.auditLogger.Log(&audit.Entry{
			Event:    audit.EventShutdown,
			Decision: "allow",
		}); err != nil {
			logger.WarnCF("agent", "Failed to write audit shutdown entry",
				map[string]any{"error": err.Error()})
		}
		if err := al.auditLogger.Close(); err != nil {
			logger.ErrorCF("agent", "Failed to close audit logger",
				map[string]any{"error": err.Error()})
		}
	}
}

// MountHook registers an in-process hook on the agent loop.
func (al *AgentLoop) MountHook(reg HookRegistration) error {
	if al == nil || al.hooks == nil {
		return fmt.Errorf("hook manager is not initialized")
	}
	return al.hooks.Mount(reg)
}

// UnmountHook removes a previously registered in-process hook.
func (al *AgentLoop) UnmountHook(name string) {
	if al == nil || al.hooks == nil {
		return
	}
	al.hooks.Unmount(name)
}

// SubscribeEvents registers a subscriber for agent-loop events.
func (al *AgentLoop) SubscribeEvents(buffer int) EventSubscription {
	if al == nil || al.eventBus == nil {
		ch := make(chan Event)
		close(ch)
		return EventSubscription{C: ch}
	}
	return al.eventBus.Subscribe(buffer)
}

// UnsubscribeEvents removes a previously registered event subscriber.
func (al *AgentLoop) UnsubscribeEvents(id uint64) {
	if al == nil || al.eventBus == nil {
		return
	}
	al.eventBus.Unsubscribe(id)
}

// EventDrops returns the number of dropped events for the given kind.
func (al *AgentLoop) EventDrops(kind EventKind) int64 {
	if al == nil || al.eventBus == nil {
		return 0
	}
	return al.eventBus.Dropped(kind)
}

type turnEventScope struct {
	agentID    string
	sessionKey string
	turnID     string
}

func (al *AgentLoop) newTurnEventScope(agentID, sessionKey string) turnEventScope {
	seq := al.turnSeq.Add(1)
	return turnEventScope{
		agentID:    agentID,
		sessionKey: sessionKey,
		turnID:     fmt.Sprintf("%s-turn-%d", agentID, seq),
	}
}

func (ts turnEventScope) meta(iteration int, source, tracePath string) EventMeta {
	return EventMeta{
		AgentID:    ts.agentID,
		TurnID:     ts.turnID,
		SessionKey: ts.sessionKey,
		Iteration:  iteration,
		Source:     source,
		TracePath:  tracePath,
	}
}

func (al *AgentLoop) emitEvent(kind EventKind, meta EventMeta, payload any) {
	evt := Event{
		Kind:    kind,
		Meta:    meta,
		Payload: payload,
	}

	if al == nil || al.eventBus == nil {
		return
	}

	al.logEvent(evt)

	al.eventBus.Emit(evt)
}

func cloneEventArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}

	cloned := make(map[string]any, len(args))
	for k, v := range args {
		cloned[k] = v
	}
	return cloned
}

func (al *AgentLoop) hookAbortError(ts *turnState, stage string, decision HookDecision) error {
	reason := decision.Reason
	if reason == "" {
		reason = "hook requested turn abort"
	}

	err := fmt.Errorf("hook aborted turn during %s: %s", stage, reason)
	al.emitEvent(
		EventKindError,
		ts.eventMeta("hooks", "turn.error"),
		ErrorPayload{
			Stage:   "hook." + stage,
			Message: err.Error(),
		},
	)
	return err
}

func hookDeniedToolContent(prefix, reason string) string {
	if reason == "" {
		return prefix
	}
	return prefix + ": " + reason
}

func (al *AgentLoop) logEvent(evt Event) {
	fields := map[string]any{
		"event_kind":  evt.Kind.String(),
		"agent_id":    evt.Meta.AgentID,
		"turn_id":     evt.Meta.TurnID,
		"session_key": evt.Meta.SessionKey,
		"iteration":   evt.Meta.Iteration,
	}

	if evt.Meta.TracePath != "" {
		fields["trace"] = evt.Meta.TracePath
	}
	if evt.Meta.Source != "" {
		fields["source"] = evt.Meta.Source
	}

	switch payload := evt.Payload.(type) {
	case TurnStartPayload:
		fields["channel"] = payload.Channel
		fields["chat_id"] = payload.ChatID
		fields["user_len"] = len(payload.UserMessage)
		fields["media_count"] = payload.MediaCount
	case TurnEndPayload:
		fields["status"] = payload.Status
		fields["iterations_total"] = payload.Iterations
		fields["duration_ms"] = payload.Duration.Milliseconds()
		fields["final_len"] = payload.FinalContentLen
	case LLMRequestPayload:
		fields["model"] = payload.Model
		fields["messages"] = payload.MessagesCount
		fields["tools"] = payload.ToolsCount
		fields["max_tokens"] = payload.MaxTokens
	case LLMDeltaPayload:
		fields["content_delta_len"] = payload.ContentDeltaLen
		fields["reasoning_delta_len"] = payload.ReasoningDeltaLen
	case LLMResponsePayload:
		fields["content_len"] = payload.ContentLen
		fields["tool_calls"] = payload.ToolCalls
		fields["has_reasoning"] = payload.HasReasoning
	case LLMRetryPayload:
		fields["attempt"] = payload.Attempt
		fields["max_retries"] = payload.MaxRetries
		fields["reason"] = payload.Reason
		fields["error"] = payload.Error
		fields["backoff_ms"] = payload.Backoff.Milliseconds()
	case ContextCompressPayload:
		fields["reason"] = payload.Reason
		fields["dropped_messages"] = payload.DroppedMessages
		fields["remaining_messages"] = payload.RemainingMessages
	case SessionSummarizePayload:
		fields["summarized_messages"] = payload.SummarizedMessages
		fields["kept_messages"] = payload.KeptMessages
		fields["summary_len"] = payload.SummaryLen
		fields["omitted_oversized"] = payload.OmittedOversized
	case ToolExecStartPayload:
		fields["tool"] = payload.Tool
		fields["args_count"] = len(payload.Arguments)
	case ToolExecEndPayload:
		fields["tool"] = payload.Tool
		fields["duration_ms"] = payload.Duration.Milliseconds()
		fields["for_llm_len"] = payload.ForLLMLen
		fields["for_user_len"] = payload.ForUserLen
		fields["is_error"] = payload.IsError
		fields["async"] = payload.Async
	case ToolExecSkippedPayload:
		fields["tool"] = payload.Tool
		fields["reason"] = payload.Reason
	case SteeringInjectedPayload:
		fields["count"] = payload.Count
		fields["total_content_len"] = payload.TotalContentLen
	case FollowUpQueuedPayload:
		fields["source_tool"] = payload.SourceTool
		fields["channel"] = payload.Channel
		fields["chat_id"] = payload.ChatID
		fields["content_len"] = payload.ContentLen
	case InterruptReceivedPayload:
		fields["interrupt_kind"] = payload.Kind
		fields["role"] = payload.Role
		fields["content_len"] = payload.ContentLen
		fields["queue_depth"] = payload.QueueDepth
		fields["hint_len"] = payload.HintLen
	case SubTurnSpawnPayload:
		fields["child_agent_id"] = payload.AgentID
		fields["label"] = payload.Label
	case SubTurnEndPayload:
		fields["child_agent_id"] = payload.AgentID
		fields["status"] = payload.Status
	case SubTurnResultDeliveredPayload:
		fields["target_channel"] = payload.TargetChannel
		fields["target_chat_id"] = payload.TargetChatID
		fields["content_len"] = payload.ContentLen
	case ErrorPayload:
		fields["stage"] = payload.Stage
		fields["error"] = payload.Message
	}

	logger.InfoCF("eventbus", fmt.Sprintf("Agent event: %s", evt.Kind.String()), fields)
}

func (al *AgentLoop) RegisterTool(tool tools.Tool) {
	registry := al.GetRegistry()
	for _, agentID := range registry.ListAgentIDs() {
		if agent, ok := registry.GetAgent(agentID); ok {
			agent.Tools.Register(tool)
		}
	}
}

func (al *AgentLoop) SetChannelManager(cm *channels.Manager) {
	al.channelManager = cm
}

// ReloadProviderAndConfig atomically swaps the provider and config with proper synchronization.
// It uses a context to allow timeout control from the caller.
// Returns an error if the reload fails or context is canceled.
func (al *AgentLoop) ReloadProviderAndConfig(
	ctx context.Context,
	provider providers.LLMProvider,
	cfg *config.Config,
) error {
	// Validate inputs
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}
	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Create new registry with updated config and provider
	// Wrap in defer/recover to handle any panics gracefully
	var registry *AgentRegistry
	var panicErr error
	done := make(chan struct{}, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicErr = fmt.Errorf("panic during registry creation: %v", r)
				logger.ErrorCF("agent", "Panic during registry creation",
					map[string]any{"panic": r})
			}
			close(done)
		}()

		registry = NewAgentRegistry(cfg, provider)
	}()

	// Wait for completion or context cancellation
	select {
	case <-done:
		if registry == nil {
			if panicErr != nil {
				return fmt.Errorf("registry creation failed: %w", panicErr)
			}
			return fmt.Errorf("registry creation failed (nil result)")
		}
	case <-ctx.Done():
		return fmt.Errorf("context canceled during registry creation: %w", ctx.Err())
	}

	// Check context again before proceeding
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context canceled after registry creation: %w", err)
	}

	// Ensure shared tools are re-registered on the new registry
	registerSharedTools(al, cfg, al.bus, registry, provider)

	// Re-wire Ava's agent CRUD tools on the new registry.
	if al.avaDeps != nil {
		if err := al.WireAvaAgentTools(al.avaDeps, registry); err != nil {
			logger.WarnCF("agent", "hot-reload: failed to re-wire Ava agent tools", map[string]any{"error": err.Error()})
		}
	}

	// Atomically swap the config and registry under write lock
	// This ensures readers see a consistent pair
	al.mu.Lock()
	oldRegistry := al.registry

	// Store new values
	al.cfg = cfg
	al.registry = registry

	// Also update fallback chain with new config
	al.fallback = providers.NewFallbackChain(providers.NewCooldownTracker())

	al.mu.Unlock()

	al.hookRuntime.reset(al)
	configureHookManagerFromConfig(al.hooks, cfg)

	// Close old provider after releasing the lock
	// This prevents blocking readers while closing
	if oldProvider, ok := extractProvider(oldRegistry); ok {
		if stateful, ok := oldProvider.(providers.StatefulProvider); ok {
			// Give in-flight requests a moment to complete
			// Use a reasonable timeout that balances cleanup vs resource usage
			select {
			case <-time.After(100 * time.Millisecond):
				stateful.Close()
			case <-ctx.Done():
				// Context canceled, close immediately but log warning
				logger.WarnCF("agent", "Context canceled during provider cleanup, forcing close",
					map[string]any{"error": ctx.Err()})
				stateful.Close()
			}
		}
	}

	// Note: oldRegistry is intentionally NOT closed here. Closing it would
	// terminate session stores that may still be in use by in-flight turns.
	// The old registry's resources (session file handles) will be GC'd when
	// no more references exist. This trades a brief fd leak during reload
	// for crash safety.

	logger.InfoCF("agent", "Provider and config reloaded successfully",
		map[string]any{
			"model": cfg.Agents.Defaults.GetModelName(),
		})

	return nil
}

// GetRegistry returns the current registry (thread-safe)
func (al *AgentLoop) GetRegistry() *AgentRegistry {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.registry
}

// GetConfig returns the current config (thread-safe)
func (al *AgentLoop) GetConfig() *config.Config {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return al.cfg
}

// SwapConfig atomically replaces the in-memory config with the supplied,
// fully-initialized *config.Config (credentials resolved, sensitive values
// registered). Callers are responsible for calling credentials.ResolveBundle
// and cfg.RegisterSensitiveValues before SwapConfig — this method only does
// the atomic pointer swap.
func (al *AgentLoop) SwapConfig(newCfg *config.Config) {
	al.mu.Lock()
	al.cfg = newCfg
	al.mu.Unlock()
}

// MutateConfig acquires the agent loop write lock and calls fn with the
// live *config.Config pointer. This serializes sysagent mutations with all
// REST readers that go through GetConfig (which holds RLock). fn must not
// call GetConfig or SwapConfig — deadlock would result.
//
// The caller (typically Deps.WithConfig) is responsible for snapshotting and
// rolling back cfg fields if fn or the subsequent SaveConfig fails.
func (al *AgentLoop) MutateConfig(fn func(*config.Config) error) error {
	al.mu.Lock()
	defer al.mu.Unlock()
	if al.cfg == nil {
		return fmt.Errorf("agent loop config is nil")
	}
	return fn(al.cfg)
}

// GetTaskStore returns the shared TaskStore (may be nil in tests).
func GetTaskStore(al *AgentLoop) *taskstore.TaskStore {
	return al.taskStore
}

// GetTaskExecutor returns the shared TaskExecutor (may be nil in tests).
func GetTaskExecutor(al *AgentLoop) *TaskExecutor {
	return al.taskExecutor
}

// GetAgentStore returns the UnifiedStore for a given agent, or nil if not found
// or if the agent's session store is not a UnifiedStore.
func (al *AgentLoop) GetAgentStore(agentID string) *session.UnifiedStore {
	agent, ok := al.GetRegistry().GetAgent(agentID)
	if !ok {
		return nil
	}
	us, ok := agent.Sessions.(*session.UnifiedStore)
	if !ok {
		logger.WarnCF("agent", "GetAgentStore: session store is not UnifiedStore",
			map[string]any{"agent_id": agentID})
		return nil
	}
	return us
}

// ResolveSessionStore finds which agent's UnifiedStore owns the given sessionID.
// Checks the main agent first (most common case), then falls back to scanning all agents.
// Returns nil if the session cannot be found across any agent.
func (al *AgentLoop) ResolveSessionStore(sessionID string) *session.UnifiedStore {
	// Fast path: main agent owns most sessions.
	if store := al.GetAgentStore(DefaultAgentID); store != nil {
		if _, err := store.GetMeta(sessionID); err == nil {
			return store
		}
	}
	// Slow path: scan all agents.
	for _, id := range al.GetRegistry().ListAgentIDs() {
		if id == DefaultAgentID {
			continue
		}
		store := al.GetAgentStore(id)
		if store == nil {
			continue
		}
		if _, err := store.GetMeta(sessionID); err == nil {
			return store
		}
	}
	return nil
}

// ListAllSessions returns sessions from all agent stores merged and sorted by UpdatedAt descending.
// The second return value collects per-agent errors so callers can distinguish
// "no sessions" from "all agents failed". Callers should surface partial errors
// as warnings rather than treating the entire response as a failure.
func (al *AgentLoop) ListAllSessions() ([]*session.UnifiedMeta, []error) {
	var all []*session.UnifiedMeta
	var errs []error
	for _, id := range al.GetRegistry().ListAgentIDs() {
		store := al.GetAgentStore(id)
		if store == nil {
			continue
		}
		sessions, err := store.ListSessions()
		if err != nil {
			logger.WarnCF("agent", "ListAllSessions: could not list sessions for agent",
				map[string]any{"agent_id": id, "error": err.Error()})
			errs = append(errs, fmt.Errorf("agent=%s: %w", id, err))
			continue
		}
		all = append(all, sessions...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].UpdatedAt.After(all[j].UpdatedAt)
	})
	return all, errs
}

// processTaskDirect runs the agent loop for a task, dispatching to the given agent.
// taskChatID identifies the WebSocket chat for event forwarding (defaults to "task:" + sessionKey).
// Channel is "webchat" for streaming; tool context is "system" so exec/cron tools are permitted.
func (al *AgentLoop) processTaskDirect(
	ctx context.Context,
	agentID, prompt, sessionKey, taskChatID string,
) (string, error) {
	if err := al.ensureHooksInitialized(ctx); err != nil {
		return "", fmt.Errorf("processTaskDirect: hooks: %w", err)
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return "", fmt.Errorf("processTaskDirect: mcp: %w", err)
	}

	registry := al.GetRegistry()
	ag, ok := registry.GetAgent(agentID)
	if !ok {
		logger.WarnCF(
			"agent",
			"processTaskDirect: agent not found, using default",
			map[string]any{"requested": agentID},
		)
		ag = registry.GetDefaultAgent()
	}
	if ag == nil {
		return "", fmt.Errorf("processTaskDirect: no agent %q", agentID)
	}

	// Tool context uses "system" channel so exec/cron tools are permitted.
	taskCtx := tools.WithAgentID(ctx, agentID)
	taskCtx = tools.WithToolContext(taskCtx, "system", "")

	if taskChatID == "" {
		taskChatID = "task:" + sessionKey
	}

	return al.runAgentLoop(taskCtx, ag, processOptions{
		SessionKey:          sessionKey,
		Channel:             "webchat",
		ChatID:              taskChatID,
		SenderID:            "task-executor",
		UserMessage:         prompt,
		DefaultResponse:     defaultResponse,
		EnableSummary:       false,
		SendResponse:        false,
		TranscriptSessionID: taskChatID,
		TranscriptStore:     al.GetAgentStore(agentID),
	})
}

// SetMediaStore injects a MediaStore for media lifecycle management.
func (al *AgentLoop) SetMediaStore(s media.MediaStore) {
	al.mediaStore = s

	// Propagate store to all registered tools that can emit media.
	registry := al.GetRegistry()
	for _, agentID := range registry.ListAgentIDs() {
		if agent, ok := registry.GetAgent(agentID); ok {
			agent.Tools.SetMediaStore(s)
		}
	}
}

// SetTranscriber injects a voice transcriber for agent-level audio transcription.
func (al *AgentLoop) SetTranscriber(t voice.Transcriber) {
	al.transcriber = t
}

// SetReloadFunc sets the callback function for triggering config reload.
func (al *AgentLoop) SetReloadFunc(fn func() error) {
	al.reloadFunc = fn
}

// TriggerReload triggers a config reload so the in-memory config picks up
// changes written to disk by safeUpdateConfigJSON. Called by REST handlers
// after persisting config changes (agent create/update, token rotate, etc.).
//
// Concurrency: the underlying reloadFunc (set in gateway.go) is guarded by
// an atomic CompareAndSwap that serializes concurrent calls — only one reload
// can be in flight at a time. A second concurrent call returns an error
// ("reload already in progress") rather than queuing a second reload.
func (al *AgentLoop) TriggerReload() error {
	if al.reloadFunc == nil {
		return fmt.Errorf("reload not configured")
	}
	return al.reloadFunc()
}

var audioAnnotationRe = regexp.MustCompile(`\[(voice|audio)(?::[^\]]*)?\]`)

// transcribeAudioInMessage resolves audio media refs, transcribes them, and
// replaces audio annotations in msg.Content with the transcribed text.
// Returns the (possibly modified) message and true if audio was transcribed.
func (al *AgentLoop) transcribeAudioInMessage(ctx context.Context, msg bus.InboundMessage) (bus.InboundMessage, bool) {
	if al.transcriber == nil || al.mediaStore == nil || len(msg.Media) == 0 {
		return msg, false
	}

	// Transcribe each audio media ref in order.
	var transcriptions []string
	for _, ref := range msg.Media {
		path, meta, err := al.mediaStore.ResolveWithMeta(ref)
		if err != nil {
			logger.WarnCF("voice", "Failed to resolve media ref", map[string]any{"ref": ref, "error": err})
			continue
		}
		if !utils.IsAudioFile(meta.Filename, meta.ContentType) {
			continue
		}
		result, err := al.transcriber.Transcribe(ctx, path)
		if err != nil {
			logger.WarnCF("voice", "Transcription failed", map[string]any{"ref": ref, "error": err})
			transcriptions = append(transcriptions, "")
			continue
		}
		transcriptions = append(transcriptions, result.Text)
	}

	if len(transcriptions) == 0 {
		return msg, false
	}

	al.sendTranscriptionFeedback(ctx, msg.Channel, msg.ChatID, msg.MessageID, transcriptions)

	// Replace audio annotations sequentially with transcriptions.
	idx := 0
	newContent := audioAnnotationRe.ReplaceAllStringFunc(msg.Content, func(match string) string {
		if idx >= len(transcriptions) {
			return match
		}
		text := transcriptions[idx]
		idx++
		return "[voice: " + text + "]"
	})

	// Append any remaining transcriptions not matched by an annotation.
	for ; idx < len(transcriptions); idx++ {
		newContent += "\n[voice: " + transcriptions[idx] + "]"
	}

	msg.Content = newContent
	return msg, true
}

// sendTranscriptionFeedback sends feedback to the user with the result of
// audio transcription if the option is enabled. It uses Manager.SendMessage
// which executes synchronously (rate limiting, splitting, retry) so that
// ordering with the subsequent placeholder is guaranteed.
func (al *AgentLoop) sendTranscriptionFeedback(
	ctx context.Context,
	channel, chatID, messageID string,
	validTexts []string,
) {
	if !al.cfg.Voice.EchoTranscription {
		return
	}
	if al.channelManager == nil {
		return
	}

	var nonEmpty []string
	for _, t := range validTexts {
		if t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}

	var feedbackMsg string
	if len(nonEmpty) > 0 {
		feedbackMsg = "Transcript: " + strings.Join(nonEmpty, "\n")
	} else {
		feedbackMsg = "No voice detected in the audio"
	}

	err := al.channelManager.SendMessage(ctx, bus.OutboundMessage{
		Channel:          channel,
		ChatID:           chatID,
		Content:          feedbackMsg,
		ReplyToMessageID: messageID,
	})
	if err != nil {
		logger.WarnCF("voice", "Failed to send transcription feedback", map[string]any{"error": err.Error()})
	}
}

// inferMediaType determines the media type ("image", "audio", "video", "file")
// from a filename and MIME content type.
func inferMediaType(filename, contentType string) string {
	ct := strings.ToLower(contentType)
	fn := strings.ToLower(filename)

	if strings.HasPrefix(ct, "image/") {
		return "image"
	}
	if strings.HasPrefix(ct, "audio/") || ct == "application/ogg" {
		return "audio"
	}
	if strings.HasPrefix(ct, "video/") {
		return "video"
	}

	// Fallback: infer from extension
	ext := filepath.Ext(fn)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp3", ".wav", ".ogg", ".m4a", ".flac", ".aac", ".wma", ".opus":
		return "audio"
	case ".mp4", ".avi", ".mov", ".webm", ".mkv":
		return "video"
	}

	return "file"
}

// RecordLastChannel records the last active channel for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChannel(channel string) error {
	if al.state == nil {
		return nil
	}
	return al.state.SetLastChannel(channel)
}

// RecordLastChatID records the last active chat ID for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChatID(chatID string) error {
	if al.state == nil {
		return nil
	}
	return al.state.SetLastChatID(chatID)
}

func (al *AgentLoop) ProcessDirect(
	ctx context.Context,
	content, sessionKey string,
) (string, error) {
	return al.ProcessDirectWithChannel(ctx, content, sessionKey, "cli", "direct")
}

func (al *AgentLoop) ProcessDirectWithChannel(
	ctx context.Context,
	content, sessionKey, channel, chatID string,
) (string, error) {
	if err := al.ensureHooksInitialized(ctx); err != nil {
		return "", err
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return "", err
	}

	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "cron",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
	}

	resp, _, err := al.processMessage(ctx, msg)
	return resp, err
}

// ProcessHeartbeat processes a heartbeat request without session history.
// Each heartbeat is independent and doesn't accumulate context.
func (al *AgentLoop) ProcessHeartbeat(
	ctx context.Context,
	content, channel, chatID string,
) (string, error) {
	if err := al.ensureHooksInitialized(ctx); err != nil {
		return "", err
	}
	if err := al.ensureMCPInitialized(ctx); err != nil {
		return "", err
	}

	agent := al.GetRegistry().GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for heartbeat")
	}
	return al.runAgentLoop(ctx, agent, processOptions{
		SessionKey:           "heartbeat",
		Channel:              channel,
		ChatID:               chatID,
		UserMessage:          content,
		DefaultResponse:      defaultResponse,
		EnableSummary:        false,
		SendResponse:         false,
		SuppressToolFeedback: true,
		NoHistory:            true, // Don't load session history for heartbeat
	})
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, *AgentInstance, error) {
	// Add message preview to log (show full content for error messages)
	var logContent string
	if strings.Contains(msg.Content, "Error:") || strings.Contains(msg.Content, "error") {
		logContent = msg.Content // Full content for errors
	} else {
		logContent = utils.Truncate(msg.Content, 80)
	}
	logger.InfoCF(
		"agent",
		fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, logContent),
		map[string]any{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		},
	)

	var hadAudio bool
	msg, hadAudio = al.transcribeAudioInMessage(ctx, msg)

	// For audio messages the placeholder was deferred by the channel.
	// Now that transcription (and optional feedback) is done, send it.
	if hadAudio && al.channelManager != nil {
		al.channelManager.SendPlaceholder(ctx, msg.Channel, msg.ChatID)
	}

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		resp, err := al.processSystemMessage(ctx, msg)
		return resp, nil, err
	}

	route, agent, routeErr := al.resolveMessageRoute(msg)
	if routeErr != nil {
		return "", nil, routeErr
	}

	// Reset message-tool state for this round so we don't skip publishing due to a previous round.
	if tool, ok := agent.Tools.Get("message"); ok {
		if resetter, ok := tool.(interface{ ResetSentInRound() }); ok {
			resetter.ResetSentInRound()
		}
	}

	// Resolve session key from route, while preserving explicit agent-scoped keys.
	scopeKey := resolveScopeKey(route, msg.SessionKey)
	sessionKey := scopeKey

	logger.InfoCF("agent", "Routed message",
		map[string]any{
			"agent_id":      agent.ID,
			"scope_key":     scopeKey,
			"session_key":   sessionKey,
			"matched_by":    route.MatchedBy,
			"route_agent":   route.AgentID,
			"route_channel": route.Channel,
		})

	// Resolve transcript store for tool call recording (forwarded via message metadata).
	var transcriptSessionID string
	var transcriptStore *session.UnifiedStore
	if tsid := inboundMetadata(msg, "transcript_session_id"); tsid != "" {
		transcriptSessionID = tsid
		transcriptStore = al.ResolveSessionStore(tsid)
		if transcriptStore == nil {
			logger.WarnCF(
				"agent",
				"transcript_session_id present but store not found — tool calls will not be recorded",
				map[string]any{"transcript_session_id": tsid},
			)
		}
	}

	opts := processOptions{
		SessionKey:          sessionKey,
		Channel:             msg.Channel,
		ChatID:              msg.ChatID,
		SenderID:            msg.SenderID,
		SenderDisplayName:   msg.Sender.DisplayName,
		UserMessage:         msg.Content,
		Media:               msg.Media,
		DefaultResponse:     defaultResponse,
		EnableSummary:       true,
		SendResponse:        false,
		TranscriptSessionID: transcriptSessionID,
		TranscriptStore:     transcriptStore,
	}

	// context-dependent commands check their own Runtime fields and report
	// "unavailable" when the required capability is nil.
	if response, handled := al.handleCommand(ctx, msg, agent, &opts); handled {
		return response, agent, nil
	}

	if pending := al.takePendingSkills(opts.SessionKey); len(pending) > 0 {
		opts.ForcedSkills = append(opts.ForcedSkills, pending...)
		logger.InfoCF("agent", "Applying pending skill override",
			map[string]any{
				"session_key": opts.SessionKey,
				"skills":      strings.Join(pending, ","),
			})
	}

	resp, err := al.runAgentLoop(ctx, agent, opts)
	return resp, agent, err
}

func (al *AgentLoop) resolveMessageRoute(msg bus.InboundMessage) (routing.ResolvedRoute, *AgentInstance, error) {
	registry := al.GetRegistry()

	// If the message carries an explicit agent_id (e.g., webchat agent selector),
	// use it directly instead of going through routing rules.
	if explicitID := inboundMetadata(msg, "agent_id"); explicitID != "" {
		agent, ok := registry.GetAgent(explicitID)
		if ok {
			logger.InfoCF("agent", "Routed to explicit agent", map[string]any{
				"agent_id":  explicitID,
				"workspace": agent.Workspace,
			})
			return routing.ResolvedRoute{AgentID: explicitID}, agent, nil
		}
		// H12: An explicit agent_id was provided but no matching agent is registered.
		// Return a hard error rather than silently falling through to default routing,
		// which would confuse the caller about which agent is actually handling the message.
		// Log internal details (including registered IDs) at Error level for operators,
		// but return a sanitized error to the caller to avoid leaking registry state.
		logger.ErrorCF("agent", "explicit agent_id not found in registry", map[string]any{
			"agent_id":       explicitID,
			"registered_ids": registry.ListAgentIDs(),
		})
		return routing.ResolvedRoute{}, nil, fmt.Errorf("the requested agent is not available")
	}

	route := registry.ResolveRoute(routing.RouteInput{
		Channel:    msg.Channel,
		AccountID:  inboundMetadata(msg, metadataKeyAccountID),
		Peer:       extractPeer(msg),
		ParentPeer: extractParentPeer(msg),
		GuildID:    inboundMetadata(msg, metadataKeyGuildID),
		TeamID:     inboundMetadata(msg, metadataKeyTeamID),
	})

	agent, ok := registry.GetAgent(route.AgentID)
	if !ok {
		agent = registry.GetDefaultAgent()
	}
	if agent == nil {
		// FR-015: log unroutable message with structured context before rejecting.
		logger.WarnCF("agent", "Unroutable message rejected — no matching agent and no default",
			map[string]any{
				"channel":        msg.Channel,
				"sender_id":      msg.SenderID,
				"chat_id":        msg.ChatID,
				"resolved_agent": route.AgentID,
			})
		return routing.ResolvedRoute{}, nil, fmt.Errorf("no agent available for route (agent_id=%s)", route.AgentID)
	}

	return route, agent, nil
}

func resolveScopeKey(route routing.ResolvedRoute, msgSessionKey string) string {
	if msgSessionKey != "" && strings.HasPrefix(msgSessionKey, sessionKeyAgentPrefix) {
		return msgSessionKey
	}
	return route.SessionKey
}

func (al *AgentLoop) resolveSteeringTarget(msg bus.InboundMessage) (string, string, bool) {
	if msg.Channel == "system" {
		return "", "", false
	}

	route, agent, err := al.resolveMessageRoute(msg)
	if err != nil || agent == nil {
		return "", "", false
	}

	return resolveScopeKey(route, msg.SessionKey), agent.ID, true
}

func (al *AgentLoop) requeueInboundMessage(msg bus.InboundMessage) error {
	if al.bus == nil {
		return nil
	}
	pubCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return al.bus.PublishInbound(pubCtx, msg)
}

func (al *AgentLoop) processSystemMessage(
	ctx context.Context,
	msg bus.InboundMessage,
) (string, error) {
	if msg.Channel != "system" {
		return "", fmt.Errorf(
			"processSystemMessage called with non-system message channel: %s",
			msg.Channel,
		)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]any{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin channel from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Extract subagent result from message content
	// Format: "Task 'label' completed.\n\nResult:\n<actual content>"
	content := msg.Content
	if idx := strings.Index(content, "Result:\n"); idx >= 0 {
		content = content[idx+8:] // Extract just the result part
	}

	// Skip internal channels - only log, don't send to user
	if constants.IsInternalChannel(originChannel) {
		logger.InfoCF("agent", "Subagent completed (internal channel)",
			map[string]any{
				"sender_id":   msg.SenderID,
				"content_len": len(content),
				"channel":     originChannel,
			})
		return "", nil
	}

	// Use default agent for system messages
	agent := al.GetRegistry().GetDefaultAgent()
	if agent == nil {
		return "", fmt.Errorf("no default agent for system message")
	}

	// Use the origin session for context
	sessionKey := routing.BuildAgentMainSessionKey(agent.ID)

	return al.runAgentLoop(ctx, agent, processOptions{
		SessionKey:      sessionKey,
		Channel:         originChannel,
		ChatID:          originChatID,
		UserMessage:     fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content),
		DefaultResponse: "Background task completed.",
		EnableSummary:   false,
		SendResponse:    true,
	})
}

// runAgentLoop remains the top-level shell that starts a turn and publishes
// any post-turn work. runTurn owns the full turn lifecycle.
func (al *AgentLoop) runAgentLoop(
	ctx context.Context,
	agent *AgentInstance,
	opts processOptions,
) (string, error) {
	// Record last channel for heartbeat notifications (skip internal channels and cli)
	if opts.Channel != "" && opts.ChatID != "" && !constants.IsInternalChannel(opts.Channel) {
		channelKey := fmt.Sprintf("%s:%s", opts.Channel, opts.ChatID)
		if err := al.RecordLastChannel(channelKey); err != nil {
			logger.WarnCF(
				"agent",
				"Failed to record last channel",
				map[string]any{"error": err.Error()},
			)
		}
	}

	ts := newTurnState(agent, opts, al.newTurnEventScope(agent.ID, opts.SessionKey))
	result, err := al.runTurn(ctx, ts)
	if err != nil {
		return "", err
	}
	if result.status == TurnEndStatusAborted {
		return "", nil
	}

	for _, followUp := range result.followUps {
		if pubErr := al.bus.PublishInbound(ctx, followUp); pubErr != nil {
			logger.WarnCF("agent", "Failed to publish follow-up after turn",
				map[string]any{
					"turn_id": ts.turnID,
					"error":   pubErr.Error(),
				})
		}
	}

	if opts.SendResponse && result.finalContent != "" {
		if err := al.bus.PublishOutbound(ctx, bus.OutboundMessage{
			Channel: opts.Channel,
			ChatID:  opts.ChatID,
			Content: result.finalContent,
		}); err != nil {
			logger.ErrorCF("agent", "Failed to publish outbound response after turn",
				map[string]any{"channel": opts.Channel, "chat_id": opts.ChatID, "error": err.Error()})
		}
	}

	if result.finalContent != "" {
		responsePreview := utils.Truncate(result.finalContent, 120)
		logger.InfoCF("agent", fmt.Sprintf("Response: %s", responsePreview),
			map[string]any{
				"agent_id":     agent.ID,
				"session_key":  opts.SessionKey,
				"iterations":   ts.currentIteration(),
				"final_length": len(result.finalContent),
			})
	}

	return result.finalContent, nil
}

func (al *AgentLoop) targetReasoningChannelID(channelName string) (chatID string) {
	if al.channelManager == nil {
		return ""
	}
	if ch, ok := al.channelManager.GetChannel(channelName); ok {
		return ch.ReasoningChannelID()
	}
	return ""
}

func (al *AgentLoop) handleReasoning(
	ctx context.Context,
	reasoningContent, channelName, channelID string,
) {
	if reasoningContent == "" || channelName == "" || channelID == "" {
		return
	}

	// Check context cancellation before attempting to publish,
	// since PublishOutbound's select may race between send and ctx.Done().
	if ctx.Err() != nil {
		return
	}

	// Use a short timeout so the goroutine does not block indefinitely when
	// the outbound bus is full.  Reasoning output is best-effort; dropping it
	// is acceptable to avoid goroutine accumulation.
	pubCtx, pubCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pubCancel()

	if err := al.bus.PublishOutbound(pubCtx, bus.OutboundMessage{
		Channel: channelName,
		ChatID:  channelID,
		Content: reasoningContent,
	}); err != nil {
		// Treat context.DeadlineExceeded / context.Canceled as expected
		// (bus full under load, or parent canceled).  Check the error
		// itself rather than ctx.Err(), because pubCtx may time out
		// (5 s) while the parent ctx is still active.
		// Also treat ErrBusClosed as expected — it occurs during normal
		// shutdown when the bus is closed before all goroutines finish.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
			errors.Is(err, bus.ErrBusClosed) {
			logger.DebugCF("agent", "Reasoning publish skipped (timeout/cancel)", map[string]any{
				"channel": channelName,
				"error":   err.Error(),
			})
		} else {
			logger.WarnCF("agent", "Failed to publish reasoning (best-effort)", map[string]any{
				"channel": channelName,
				"error":   err.Error(),
			})
		}
	}
}

func (al *AgentLoop) runTurn(ctx context.Context, ts *turnState) (turnResult, error) {
	// H1: guard against an already-canceled or timed-out context before doing any work.
	if ctx.Err() != nil {
		return turnResult{}, fmt.Errorf("turn not started: %w", ctx.Err())
	}

	var turnCtx context.Context
	var turnCancel context.CancelFunc
	turnTimeout := time.Duration(ts.agent.TimeoutSeconds) * time.Second
	if turnTimeout > 0 {
		turnCtx, turnCancel = context.WithTimeout(ctx, turnTimeout)
	} else {
		turnCtx, turnCancel = context.WithCancel(ctx)
	}
	defer turnCancel()
	ts.setTurnCancel(turnCancel)

	// Finalize the streamer when the turn ends (regardless of how it exits).
	// This sends the "done" frame exactly once, at turn completion, rather than
	// after each intermediate LLM call that may be followed by tool execution.
	defer ts.finalizeStreamer(ctx)

	// Inject turnState and AgentLoop into context so tools (e.g. spawn) can retrieve them.
	turnCtx = withTurnState(turnCtx, ts)
	turnCtx = WithAgentLoop(turnCtx, al)
	// SEC-15: Inject agent ID so audit entries carry the agent identity.
	turnCtx = tools.WithAgentID(turnCtx, ts.agent.ID)

	al.registerActiveTurn(ts)
	defer al.clearActiveTurn(ts)

	turnStatus := TurnEndStatusCompleted
	defer func() {
		al.emitEvent(
			EventKindTurnEnd,
			ts.eventMeta("runTurn", "turn.end"),
			TurnEndPayload{
				Status:          turnStatus,
				Iterations:      ts.currentIteration(),
				Duration:        time.Since(ts.startedAt),
				FinalContentLen: ts.finalContentLen(),
			},
		)
	}()

	al.emitEvent(
		EventKindTurnStart,
		ts.eventMeta("runTurn", "turn.start"),
		TurnStartPayload{
			Channel:     ts.channel,
			ChatID:      ts.chatID,
			UserMessage: ts.userMessage,
			MediaCount:  len(ts.media),
		},
	)

	var history []providers.Message
	var summary string
	if !ts.opts.NoHistory {
		history = ts.agent.Sessions.GetHistory(ts.sessionKey)
		summary = ts.agent.Sessions.GetSummary(ts.sessionKey)
	}
	ts.captureRestorePoint(history, summary)

	messages := ts.agent.ContextBuilder.BuildMessages(
		history,
		summary,
		ts.userMessage,
		ts.media,
		ts.channel,
		ts.chatID,
		ts.opts.SenderID,
		ts.opts.SenderDisplayName,
		activeSkillNames(ts.agent, ts.opts)...,
	)

	cfg := al.GetConfig()
	maxMediaSize := cfg.Agents.Defaults.GetMaxMediaSize()
	messages = resolveMediaRefs(messages, al.mediaStore, maxMediaSize)

	if !ts.opts.NoHistory {
		toolDefs := ts.agent.Tools.ToProviderDefs()
		if isOverContextBudget(ts.agent.ContextWindow, messages, toolDefs, ts.agent.MaxTokens) {
			logger.WarnCF("agent", "Proactive compression: context budget exceeded before LLM call",
				map[string]any{"session_key": ts.sessionKey})
			if compression, ok := al.forceCompression(ts.agent, ts.sessionKey); ok {
				al.emitEvent(
					EventKindContextCompress,
					ts.eventMeta("runTurn", "turn.context.compress"),
					ContextCompressPayload{
						Reason:            ContextCompressReasonProactive,
						DroppedMessages:   compression.DroppedMessages,
						RemainingMessages: compression.RemainingMessages,
					},
				)
				ts.refreshRestorePointFromSession(ts.agent)
			}
			newHistory := ts.agent.Sessions.GetHistory(ts.sessionKey)
			newSummary := ts.agent.Sessions.GetSummary(ts.sessionKey)
			messages = ts.agent.ContextBuilder.BuildMessages(
				newHistory, newSummary, ts.userMessage,
				ts.media, ts.channel, ts.chatID,
				ts.opts.SenderID, ts.opts.SenderDisplayName,
				activeSkillNames(ts.agent, ts.opts)...,
			)
			messages = resolveMediaRefs(messages, al.mediaStore, maxMediaSize)
		}
	}

	// Save user message to session
	if !ts.opts.NoHistory && (strings.TrimSpace(ts.userMessage) != "" || len(ts.media) > 0) {
		rootMsg := providers.Message{
			Role:    "user",
			Content: ts.userMessage,
			Media:   append([]string(nil), ts.media...),
		}
		if len(rootMsg.Media) > 0 {
			ts.agent.Sessions.AddFullMessage(ts.sessionKey, rootMsg)
		} else {
			ts.agent.Sessions.AddMessage(ts.sessionKey, rootMsg.Role, rootMsg.Content)
		}
		ts.recordPersistedMessage(rootMsg)
	}

	ts.agent.mu.RLock()
	activeCandidates, activeModel, usedLight := al.selectCandidates(ts.agent, ts.userMessage, messages)
	activeProvider := ts.agent.Provider
	if usedLight && ts.agent.LightProvider != nil {
		activeProvider = ts.agent.LightProvider
	}
	ts.agent.mu.RUnlock()
	pendingMessages := append([]providers.Message(nil), ts.opts.InitialSteeringMessages...)
	var finalContent string
	emptyResponseRetries := 0
	const maxEmptyResponseRetries = 1

turnLoop:
	for ts.currentIteration() < ts.agent.MaxIterations || len(pendingMessages) > 0 || func() bool {
		graceful, _ := ts.gracefulInterruptRequested()
		return graceful
	}() {
		if ts.hardAbortRequested() {
			turnStatus = TurnEndStatusAborted
			return al.abortTurn(ts)
		}

		iteration := ts.currentIteration() + 1
		ts.setIteration(iteration)

		// Hard ceiling: never exceed 2x MaxIterations regardless of pending messages or
		// graceful-interrupt state. This prevents an unbounded loop when the agent keeps
		// producing follow-up messages or the interrupt flag is never cleared.
		if hardCeiling := 2 * ts.agent.MaxIterations; iteration > hardCeiling {
			logger.WarnCF("agent", "Turn exceeded hard iteration ceiling, breaking unconditionally",
				map[string]any{
					"agent_id":     ts.agentID,
					"turn_id":      ts.turnID,
					"iteration":    iteration,
					"max_iter":     ts.agent.MaxIterations,
					"hard_ceiling": hardCeiling,
				})
			break turnLoop
		}

		ts.setPhase(TurnPhaseRunning)

		// SEC-26: Per-agent LLM call rate limit check. Runs once per turn
		// iteration, before the actual LLM call. The system agent is exempt.
		if al.rateLimiter != nil && cfg.Sandbox.RateLimits.MaxAgentLLMCallsPerHour > 0 &&
			!security.IsSystemAgent(ts.agent.ID) {
			window := al.rateLimiter.GetOrCreate(
				"agent:"+ts.agent.ID+":llm_call",
				cfg.Sandbox.RateLimits.MaxAgentLLMCallsPerHour,
				time.Hour,
				security.ScopeAgent,
				ts.agent.ID,
				"llm_call",
			)
			if result := window.Allow(); !result.Allowed {
				al.recordRateLimitDenial(
					ts,
					"agent_llm_calls_per_hour",
					RateLimitPayload{
						Scope:             string(security.ScopeAgent),
						Resource:          "llm_call",
						PolicyRule:        result.PolicyRule,
						RetryAfterSeconds: result.RetryAfterSeconds,
						AgentID:           ts.agent.ID,
						ChatID:            ts.chatID,
					},
					map[string]any{"retry_after_seconds": result.RetryAfterSeconds},
				)
				turnStatus = TurnEndStatusError
				return turnResult{}, fmt.Errorf("rate limit: %s (retry after %.0fs)",
					result.PolicyRule, result.RetryAfterSeconds)
			}
		}

		// SEC-26: Global daily cost cap pre-check. Deny if the accumulated cost
		// for today already meets or exceeds the cap. The system agent is exempt.
		if al.rateLimiter != nil && cfg.Sandbox.RateLimits.DailyCostCapUSD > 0 &&
			!security.IsSystemAgent(ts.agent.ID) {
			if currentCost := al.rateLimiter.GetDailyCost(); currentCost >= cfg.Sandbox.RateLimits.DailyCostCapUSD {
				capRule := fmt.Sprintf("global daily cost cap exceeded ($%.2f)", cfg.Sandbox.RateLimits.DailyCostCapUSD)
				al.recordRateLimitDenial(
					ts,
					"daily_cost_cap_usd",
					RateLimitPayload{
						Scope:      string(security.ScopeGlobal),
						Resource:   "daily_cost_usd",
						PolicyRule: capRule,
						AgentID:    ts.agent.ID,
						ChatID:     ts.chatID,
					},
					map[string]any{
						"daily_cost_usd": currentCost,
						"daily_cost_cap": cfg.Sandbox.RateLimits.DailyCostCapUSD,
					},
				)
				turnStatus = TurnEndStatusError
				return turnResult{}, fmt.Errorf("rate limit: %s", capRule)
			}
		}

		if iteration > 1 {
			if steerMsgs := al.dequeueSteeringMessagesForScope(ts.sessionKey); len(steerMsgs) > 0 {
				pendingMessages = append(pendingMessages, steerMsgs...)
			}
		} else if !ts.opts.SkipInitialSteeringPoll {
			if steerMsgs := al.dequeueSteeringMessagesForScopeWithFallback(ts.sessionKey); len(steerMsgs) > 0 {
				pendingMessages = append(pendingMessages, steerMsgs...)
			}
		}

		// Check if parent turn has ended (SubTurn support)
		if ts.parentTurnState != nil && ts.IsParentEnded() {
			if !ts.critical {
				logger.InfoCF("agent", "Parent turn ended, non-critical SubTurn exiting gracefully", map[string]any{
					"agent_id":  ts.agentID,
					"iteration": iteration,
					"turn_id":   ts.turnID,
				})
				break
			}
			logger.InfoCF("agent", "Parent turn ended, critical SubTurn continues running", map[string]any{
				"agent_id":  ts.agentID,
				"iteration": iteration,
				"turn_id":   ts.turnID,
			})
		}

		// Poll for pending SubTurn results
		if ts.pendingResults != nil {
			select {
			case result, ok := <-ts.pendingResults:
				if ok && result != nil && result.ForLLM != "" {
					content := al.cfg.FilterSensitiveData(result.ForLLM)
					msg := providers.Message{Role: "user", Content: fmt.Sprintf("[SubTurn Result] %s", content)}
					pendingMessages = append(pendingMessages, msg)
				}
			default:
				// No results available
			}
		}

		// Inject pending steering messages
		if len(pendingMessages) > 0 {
			resolvedPending := resolveMediaRefs(pendingMessages, al.mediaStore, maxMediaSize)
			totalContentLen := 0
			for i, pm := range pendingMessages {
				messages = append(messages, resolvedPending[i])
				totalContentLen += len(pm.Content)
				if !ts.opts.NoHistory {
					// Persist the original (unresolved) message to session history to preserve
					// compact media refs; resolved (base64) form is only used for the LLM request.
					ts.agent.Sessions.AddFullMessage(ts.sessionKey, pm)
					ts.recordPersistedMessage(pm)
				}
				logger.InfoCF("agent", "Injected steering message into context",
					map[string]any{
						"agent_id":    ts.agent.ID,
						"iteration":   iteration,
						"content_len": len(pm.Content),
						"media_count": len(pm.Media),
					})
			}
			al.emitEvent(
				EventKindSteeringInjected,
				ts.eventMeta("runTurn", "turn.steering.injected"),
				SteeringInjectedPayload{
					Count:           len(pendingMessages),
					TotalContentLen: totalContentLen,
				},
			)
			pendingMessages = nil
		}

		logger.DebugCF("agent", "LLM iteration",
			map[string]any{
				"agent_id":  ts.agent.ID,
				"iteration": iteration,
				"max":       ts.agent.MaxIterations,
			})

		gracefulTerminal, _ := ts.gracefulInterruptRequested()
		providerToolDefs := ts.agent.Tools.ToProviderDefs()

		// Native web search support
		_, hasWebSearch := ts.agent.Tools.Get("web_search")
		useNativeSearch := al.cfg.Tools.Web.PreferNative &&
			hasWebSearch &&
			func() bool {
				// Check if provider supports native search
				if ns, ok := activeProvider.(interface{ SupportsNativeSearch() bool }); ok {
					return ns.SupportsNativeSearch()
				}
				return false
			}()

		if useNativeSearch {
			// Filter out client-side web_search tool
			filtered := make([]providers.ToolDefinition, 0, len(providerToolDefs))
			for _, td := range providerToolDefs {
				if td.Function.Name != "web_search" {
					filtered = append(filtered, td)
				}
			}
			providerToolDefs = filtered
		}

		callMessages := messages
		if gracefulTerminal {
			callMessages = append(append([]providers.Message(nil), messages...), ts.interruptHintMessage())
			providerToolDefs = nil
			ts.markGracefulTerminalUsed()
		}

		llmOpts := map[string]any{
			"max_tokens":       ts.agent.MaxTokens,
			"temperature":      ts.agent.Temperature,
			"prompt_cache_key": ts.agent.ID,
		}
		if useNativeSearch {
			llmOpts["native_search"] = true
		}
		ts.agent.mu.RLock()
		agentThinkingLevel := ts.agent.ThinkingLevel
		ts.agent.mu.RUnlock()
		if agentThinkingLevel != ThinkingOff {
			if tc, ok := activeProvider.(providers.ThinkingCapable); ok && tc.SupportsThinking() {
				llmOpts["thinking_level"] = string(agentThinkingLevel)
			} else {
				logger.WarnCF("agent", "thinking_level is set but current provider does not support it, ignoring",
					map[string]any{"agent_id": ts.agent.ID, "thinking_level": string(agentThinkingLevel)})
			}
		}

		llmModel := activeModel
		if al.hooks != nil {
			llmReq, decision := al.hooks.BeforeLLM(turnCtx, &LLMHookRequest{
				Meta:             ts.eventMeta("runTurn", "turn.llm.request"),
				Model:            llmModel,
				Messages:         callMessages,
				Tools:            providerToolDefs,
				Options:          llmOpts,
				Channel:          ts.channel,
				ChatID:           ts.chatID,
				GracefulTerminal: gracefulTerminal,
			})
			switch decision.normalizedAction() {
			case HookActionContinue, HookActionModify:
				if llmReq != nil {
					llmModel = llmReq.Model
					callMessages = llmReq.Messages
					providerToolDefs = llmReq.Tools
					llmOpts = llmReq.Options
				}
			case HookActionAbortTurn:
				turnStatus = TurnEndStatusError
				return turnResult{}, al.hookAbortError(ts, "before_llm", decision)
			case HookActionHardAbort:
				_ = ts.requestHardAbort()
				turnStatus = TurnEndStatusAborted
				return al.abortTurn(ts)
			}
		}

		al.emitEvent(
			EventKindLLMRequest,
			ts.eventMeta("runTurn", "turn.llm.request"),
			LLMRequestPayload{
				Model:         llmModel,
				MessagesCount: len(callMessages),
				ToolsCount:    len(providerToolDefs),
				MaxTokens:     ts.agent.MaxTokens,
				Temperature:   ts.agent.Temperature,
			},
		)

		systemPromptLen := 0
		if len(callMessages) > 0 {
			systemPromptLen = len(callMessages[0].Content)
		}
		logger.DebugCF("agent", "LLM request",
			map[string]any{
				"agent_id":          ts.agent.ID,
				"iteration":         iteration,
				"model":             llmModel,
				"messages_count":    len(callMessages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        ts.agent.MaxTokens,
				"temperature":       ts.agent.Temperature,
				"system_prompt_len": systemPromptLen,
			})
		logger.DebugCF("agent", "Full LLM request",
			map[string]any{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(callMessages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		callLLM := func(messagesForCall []providers.Message, toolDefsForCall []providers.ToolDefinition) (*providers.LLMResponse, error) {
			providerCtx, providerCancel := context.WithCancel(turnCtx)
			ts.setProviderCancel(providerCancel)
			defer func() {
				providerCancel()
				ts.clearProviderCancel(providerCancel)
			}()

			al.activeRequests.Add(1)
			defer al.activeRequests.Done()

			if len(activeCandidates) > 1 && al.fallback != nil {
				fbResult, fbErr := al.fallback.Execute(
					providerCtx,
					activeCandidates,
					func(ctx context.Context, provider, model string) (*providers.LLMResponse, error) {
						return activeProvider.Chat(ctx, messagesForCall, toolDefsForCall, model, llmOpts)
					},
				)
				if fbErr != nil {
					return nil, fbErr
				}
				if fbResult.Provider != "" && len(fbResult.Attempts) > 0 {
					logger.InfoCF(
						"agent",
						fmt.Sprintf("Fallback: succeeded with %s/%s after %d attempts",
							fbResult.Provider, fbResult.Model, len(fbResult.Attempts)+1),
						map[string]any{"agent_id": ts.agent.ID, "iteration": iteration},
					)
				}
				return fbResult.Response, nil
			}
			// Use streaming if the provider supports it and we have a streamer for this channel.
			if sp, ok := activeProvider.(providers.StreamingProvider); ok && al.bus != nil {
				logger.DebugCF("agent", "Provider supports streaming, checking for streamer", map[string]any{"channel": ts.channel, "chat_id": ts.chatID})
				if streamer, hasStreamer := al.bus.GetStreamer(providerCtx, ts.channel, ts.chatID); hasStreamer {
					logger.InfoCF("agent", "Using streaming for response", map[string]any{"channel": ts.channel, "chat_id": ts.chatID})
					var lastChunk string
					resp, streamErr := sp.ChatStream(providerCtx, messagesForCall, toolDefsForCall, llmModel, llmOpts, func(accumulated string) {
						// Send only the new delta (accumulated minus what we already sent)
						delta := accumulated[len(lastChunk):]
						lastChunk = accumulated
						if delta != "" {
							if err := streamer.Update(providerCtx, delta); err != nil {
								logger.DebugCF("agent", "Streaming update error (client may have disconnected)", map[string]any{"error": err.Error()})
							}
						}
					})
					// Do NOT finalize here — the turn may continue with tool calls.
					// Store the streamer so the turn-level code can finalize once,
					// after the last LLM call, preventing premature "done" frames
					// that tell the frontend the response is complete mid-turn.
					ts.setLastStreamer(streamer)
					return resp, streamErr
				}
			}
			return activeProvider.Chat(providerCtx, messagesForCall, toolDefsForCall, llmModel, llmOpts)
		}

		var response *providers.LLMResponse
		var err error
		maxRetries := 2
		compactionAttemptedOnTimeout := false
		contextCompressionFailed := false // C3: tracks that compression was tried but returned ok=false
		for retry := 0; retry <= maxRetries; retry++ {
			response, err = callLLM(callMessages, providerToolDefs)
			if err == nil {
				break
			}
			if ts.hardAbortRequested() && errors.Is(err, context.Canceled) {
				turnStatus = TurnEndStatusAborted
				return al.abortTurn(ts)
			}

			// I3: if the FallbackChain already exhausted all candidates, don't retry
			// in the outer loop — the chain already tried everything. Break immediately
			// so the error surfaces to the caller without redundant delay.
			var exhaustedErr *providers.FallbackExhaustedError
			if errors.As(err, &exhaustedErr) {
				break
			}

			// Use ClassifyError to distinguish turn-level errors from provider errors.
			// Provider-transient errors (429, 5xx, auth) are handled by the FallbackChain;
			// break here and let the error propagate to the caller.
			//
			// C1: pass the provider name (not the model name) as the second argument.
			// The provider name comes from the first active candidate; fall back to the
			// agent's configured provider field when no candidates are resolved.
			activeProviderName := ""
			if len(activeCandidates) > 0 {
				activeProviderName = activeCandidates[0].Provider
			}
			failErr := providers.ClassifyError(err, activeProviderName, llmModel)

			var isTimeoutError bool
			var isContextError bool
			if failErr != nil {
				isTimeoutError = failErr.Reason == providers.FailoverTimeout
				isContextError = failErr.Reason == providers.FailoverContextOverflow
				// Retriable provider errors (rate limit, auth, overloaded) are handled
				// by the FallbackChain. Don't retry inline — break so the error surfaces.
				if failErr.IsRetriable() && !isTimeoutError {
					break
				}
				// Non-retriable, non-timeout, non-context errors: break immediately.
				if !isTimeoutError && !isContextError {
					break
				}
			} else {
				// ClassifyError returned nil: unknown error. Don't retry.
				break
			}

			if isTimeoutError && retry < maxRetries {
				// I1: emit EventKindTurnTimeout when a timeout error is detected.
				al.emitEvent(
					EventKindTurnTimeout,
					ts.eventMeta("runTurn", "turn.timeout"),
					TurnTimeoutPayload{
						TimeoutSeconds: ts.agent.TimeoutSeconds,
						Compacted:      compactionAttemptedOnTimeout,
						Retried:        retry > 0,
					},
				)
				// Timeout recovery: compact context if it's heavily loaded, then retry once.
				if !compactionAttemptedOnTimeout && ts.agent.SummarizeTokenPercent > 0 && !ts.opts.NoHistory {
					toolDefs := ts.agent.Tools.ToProviderDefs()
					if isOverContextBudget(
						ts.agent.ContextWindow*ts.agent.SummarizeTokenPercent/100,
						callMessages, toolDefs, ts.agent.MaxTokens,
					) {
						compactionAttemptedOnTimeout = true
						if compression, ok := al.forceCompression(ts.agent, ts.sessionKey); ok {
							al.emitEvent(
								EventKindContextCompress,
								ts.eventMeta("runTurn", "turn.context.compress"),
								ContextCompressPayload{
									Reason:            ContextCompressReasonRetry,
									DroppedMessages:   compression.DroppedMessages,
									RemainingMessages: compression.RemainingMessages,
								},
							)
							// I1: emit EventKindCompactionRetry when compaction is triggered
							// during timeout recovery (separate from the general compress event).
							al.emitEvent(
								EventKindCompactionRetry,
								ts.eventMeta("runTurn", "turn.compaction_retry"),
								CompactionRetryPayload{
									DroppedMessages:   compression.DroppedMessages,
									RemainingMessages: compression.RemainingMessages,
								},
							)
							ts.refreshRestorePointFromSession(ts.agent)
							newHistory := ts.agent.Sessions.GetHistory(ts.sessionKey)
							newSummary := ts.agent.Sessions.GetSummary(ts.sessionKey)
							messages = ts.agent.ContextBuilder.BuildMessages(
								newHistory, newSummary, "",
								nil, ts.channel, ts.chatID, ts.opts.SenderID, ts.opts.SenderDisplayName,
								activeSkillNames(ts.agent, ts.opts)...,
							)
							callMessages = messages
							if gracefulTerminal {
								callMessages = append(append([]providers.Message(nil), messages...), ts.interruptHintMessage())
							}
						} else {
							// Compaction failed: return partial content + timeout message.
							logger.WarnCF("agent", "Compaction failed during timeout recovery; returning partial response",
								map[string]any{"agent_id": ts.agent.ID, "iteration": iteration})
							break
						}
					}
				}

				// Exponential backoff with full jitter (base 2s, max 30s).
				base := 2 * time.Second
				calculated := base * (1 << uint(retry)) // 2^retry * base
				if calculated > 30*time.Second {
					calculated = 30 * time.Second
				}
				jitter := time.Duration(rand.Int64N(int64(calculated) + 1))
				// M3: enforce a minimum backoff floor of 500ms so jitter can never produce
				// a zero or near-zero delay (rand.Int64N(1) == 0 when calculated == 0).
				backoff := jitter
				if backoff < 500*time.Millisecond {
					backoff = 500 * time.Millisecond
				}
				al.emitEvent(
					EventKindLLMRetry,
					ts.eventMeta("runTurn", "turn.llm.retry"),
					LLMRetryPayload{
						Attempt:    retry + 1,
						MaxRetries: maxRetries,
						Reason:     "timeout",
						Error:      err.Error(),
						Backoff:    backoff,
					},
				)
				if retry == 0 && !constants.IsInternalChannel(ts.channel) {
					if notifyErr := al.bus.PublishOutbound(turnCtx, bus.OutboundMessage{
						Channel: ts.channel,
						ChatID:  ts.chatID,
						Content: "Retrying — please wait...",
					}); notifyErr != nil {
						logger.WarnCF("agent", "Failed to send retry indicator",
							map[string]any{"channel": ts.channel, "error": notifyErr.Error()})
					}
				}
				logger.WarnCF("agent", "Timeout error, retrying after backoff", map[string]any{
					"error":   err.Error(),
					"retry":   retry,
					"backoff": backoff.String(),
				})
				if sleepErr := sleepWithContext(turnCtx, backoff); sleepErr != nil {
					if ts.hardAbortRequested() {
						turnStatus = TurnEndStatusAborted
						return al.abortTurn(ts)
					}
					err = sleepErr
					break
				}
				continue
			}

			if isContextError && retry < maxRetries && !ts.opts.NoHistory {
				// C3: if a previous compression attempt returned ok=false and we're
				// still getting context errors, retrying with identical data won't help.
				// Break to surface the error rather than burning the remaining budget.
				if contextCompressionFailed {
					logger.WarnCF("agent", "Context overflow persists after failed compression; aborting retry",
						map[string]any{"agent_id": ts.agent.ID, "iteration": iteration, "retry": retry})
					break
				}
				al.emitEvent(
					EventKindLLMRetry,
					ts.eventMeta("runTurn", "turn.llm.retry"),
					LLMRetryPayload{
						Attempt:    retry + 1,
						MaxRetries: maxRetries,
						Reason:     "context_limit",
						Error:      err.Error(),
					},
				)
				logger.WarnCF(
					"agent",
					"Context window error detected, attempting compression",
					map[string]any{
						"error": err.Error(),
						"retry": retry,
					},
				)

				if retry == 0 && !constants.IsInternalChannel(ts.channel) {
					if notifyErr := al.bus.PublishOutbound(turnCtx, bus.OutboundMessage{
						Channel: ts.channel,
						ChatID:  ts.chatID,
						Content: "Context window exceeded. Compressing history and retrying...",
					}); notifyErr != nil {
						logger.WarnCF("agent", "Failed to notify user of context compression",
							map[string]any{"channel": ts.channel, "error": notifyErr.Error()})
					}
				}

				if compression, ok := al.forceCompression(ts.agent, ts.sessionKey); ok {
					al.emitEvent(
						EventKindContextCompress,
						ts.eventMeta("runTurn", "turn.context.compress"),
						ContextCompressPayload{
							Reason:            ContextCompressReasonRetry,
							DroppedMessages:   compression.DroppedMessages,
							RemainingMessages: compression.RemainingMessages,
						},
					)
					ts.refreshRestorePointFromSession(ts.agent)
				} else {
					// C3: compaction returned ok=false (nothing to compress). Mark the
					// flag so the NEXT retry attempt will break rather than burning more
					// budget on identical data. We still allow this single retry through
					// because the provider might succeed without context reduction.
					contextCompressionFailed = true
					logger.WarnCF("agent", "Compaction failed during context overflow recovery; will not retry further",
						map[string]any{"agent_id": ts.agent.ID, "iteration": iteration})
				}

				newHistory := ts.agent.Sessions.GetHistory(ts.sessionKey)
				newSummary := ts.agent.Sessions.GetSummary(ts.sessionKey)
				messages = ts.agent.ContextBuilder.BuildMessages(
					newHistory, newSummary, "",
					nil, ts.channel, ts.chatID, ts.opts.SenderID, ts.opts.SenderDisplayName,
					activeSkillNames(ts.agent, ts.opts)...,
				)
				callMessages = messages
				if gracefulTerminal {
					callMessages = append(append([]providers.Message(nil), messages...), ts.interruptHintMessage())
				}
				continue
			}
			break
		}

		if err != nil {
			// C2: check for context cancellation/timeout before reporting a generic
			// "LLM call failed" error — these are user/system actions, not LLM failures.
			if errors.Is(err, context.Canceled) {
				return turnResult{}, fmt.Errorf("turn canceled")
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return turnResult{}, fmt.Errorf("turn timed out")
			}
			turnStatus = TurnEndStatusError
			al.emitEvent(
				EventKindError,
				ts.eventMeta("runTurn", "turn.error"),
				ErrorPayload{
					Stage:   "llm",
					Message: err.Error(),
				},
			)
			logger.ErrorCF("agent", "LLM call failed",
				map[string]any{
					"agent_id":  ts.agent.ID,
					"iteration": iteration,
					"model":     llmModel,
					"error":     err.Error(),
				})
			return turnResult{}, fmt.Errorf("LLM call failed after retries: %w", err)
		}

		if al.hooks != nil {
			llmResp, decision := al.hooks.AfterLLM(turnCtx, &LLMHookResponse{
				Meta:     ts.eventMeta("runTurn", "turn.llm.response"),
				Model:    llmModel,
				Response: response,
				Channel:  ts.channel,
				ChatID:   ts.chatID,
			})
			switch decision.normalizedAction() {
			case HookActionContinue, HookActionModify:
				if llmResp != nil && llmResp.Response != nil {
					response = llmResp.Response
				}
			case HookActionAbortTurn:
				turnStatus = TurnEndStatusError
				return turnResult{}, al.hookAbortError(ts, "after_llm", decision)
			case HookActionHardAbort:
				_ = ts.requestHardAbort()
				turnStatus = TurnEndStatusAborted
				return al.abortTurn(ts)
			}
		}

		// Save finishReason to turnState for SubTurn truncation detection.
		// H5: use turnCtx (the per-turn context that carries the turnState value),
		// not the outer ctx which may not have the turnState attached.
		if innerTS := turnStateFromContext(turnCtx); innerTS != nil {
			innerTS.SetLastFinishReason(response.FinishReason)
			// Save usage for token budget tracking
			if response.Usage != nil {
				innerTS.SetLastUsage(response.Usage)
			}
		}

		reasoningContent := response.Reasoning
		if reasoningContent == "" {
			reasoningContent = response.ReasoningContent
		}
		go al.handleReasoning(
			turnCtx,
			reasoningContent,
			ts.channel,
			al.targetReasoningChannelID(ts.channel),
		)
		al.emitEvent(
			EventKindLLMResponse,
			ts.eventMeta("runTurn", "turn.llm.response"),
			LLMResponsePayload{
				ContentLen:   len(response.Content),
				ToolCalls:    len(response.ToolCalls),
				HasReasoning: response.Reasoning != "" || response.ReasoningContent != "",
			},
		)

		llmResponseFields := map[string]any{
			"agent_id":       ts.agent.ID,
			"iteration":      iteration,
			"content_chars":  len(response.Content),
			"tool_calls":     len(response.ToolCalls),
			"reasoning":      response.Reasoning,
			"target_channel": al.targetReasoningChannelID(ts.channel),
			"channel":        ts.channel,
		}
		if response.Usage != nil {
			llmResponseFields["prompt_tokens"] = response.Usage.PromptTokens
			llmResponseFields["completion_tokens"] = response.Usage.CompletionTokens
			llmResponseFields["total_tokens"] = response.Usage.TotalTokens
		}
		logger.DebugCF("agent", "LLM response", llmResponseFields)

		// SEC-26: Record the cost of this completed LLM call in the daily
		// accumulator. We MUST use RecordSpend (not CheckGlobalCostCap) here:
		// the call already happened, so the spend must be recorded even if it
		// pushes the total past the cap — the next turn's pre-check will deny
		// further calls. CheckGlobalCostCap silently skipped the increment on
		// denials, which caused the accumulator to stick below the cap and let
		// every subsequent call sneak through.
		if al.rateLimiter != nil && response != nil && response.Usage != nil {
			callCost := estimateLLMCallCost(llmModel, response.Usage)
			al.rateLimiter.RecordSpend(callCost, ts.agent.ID)
			// Accumulate turn-level stats so the "done" WS frame can surface
			// real token counts and cost to the chat UI (issue #12).
			ts.AddTurnStats(int64(response.Usage.TotalTokens), callCost)
			if al.costTracker != nil {
				if saveErr := al.costTracker.SaveFromRegistry(al.rateLimiter); saveErr != nil {
					logger.ErrorCF("agent", "SEC-26: failed to persist daily cost after LLM call — cap may under-count on restart",
						map[string]any{
							"error":          saveErr.Error(),
							"agent_id":       ts.agent.ID,
							"call_cost_usd":  callCost,
							"daily_cost_usd": al.rateLimiter.GetDailyCost(),
							"model":          llmModel,
						})
				}
			}
		}

		if len(response.ToolCalls) == 0 || gracefulTerminal {
			responseContent := response.Content
			if responseContent == "" && response.ReasoningContent != "" {
				responseContent = response.ReasoningContent
			}
			if steerMsgs := al.dequeueSteeringMessagesForScope(ts.sessionKey); len(steerMsgs) > 0 {
				logger.InfoCF("agent", "Steering arrived after direct LLM response; continuing turn",
					map[string]any{
						"agent_id":       ts.agent.ID,
						"iteration":      iteration,
						"steering_count": len(steerMsgs),
					})
				pendingMessages = append(pendingMessages, steerMsgs...)
				continue
			}
			// Empty response recovery (FR-006): if LLM returned empty content with no
			// reasoning and no tool calls, retry once before surfacing a fallback message.
			//
			// H3: perform the retry in an inner loop that calls callLLM directly, so we
			// do NOT increment the outer iteration counter (which would consume the agent's
			// MaxIterations budget for what is purely a provider-level retry).
			for strings.TrimSpace(responseContent) == "" && emptyResponseRetries < maxEmptyResponseRetries {
				emptyResponseRetries++
				logger.WarnCF("agent", "Empty response from LLM, retrying", map[string]any{
					"agent_id":  ts.agent.ID,
					"iteration": iteration,
					"attempt":   emptyResponseRetries,
				})
				al.emitEvent(
					EventKindLLMRetry,
					ts.eventMeta("runTurn", "turn.llm.retry"),
					LLMRetryPayload{
						Attempt:    emptyResponseRetries,
						MaxRetries: maxEmptyResponseRetries,
						Reason:     "empty_response",
					},
				)
				// I1: also emit the dedicated EventKindEmptyResponseRetry for subscribers
				// that specifically track empty-response retry behavior.
				al.emitEvent(
					EventKindEmptyResponseRetry,
					ts.eventMeta("runTurn", "turn.empty_response_retry"),
					EmptyResponseRetryPayload{
						Attempt:    emptyResponseRetries,
						MaxRetries: maxEmptyResponseRetries,
					},
				)
				// Re-call the LLM directly without advancing the outer turn iteration.
				retryResp, retryErr := callLLM(callMessages, providerToolDefs)
				if retryErr != nil {
					// Propagate the error back to the outer error-handling block by
					// overwriting response/err and breaking out of both loops.
					response = nil
					err = retryErr
					break
				}
				response = retryResp
				responseContent = response.Content
				if responseContent == "" && response.ReasoningContent != "" {
					responseContent = response.ReasoningContent
				}
			}
			// If the inner retry loop set an error, surface it via the outer error path.
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return turnResult{}, fmt.Errorf("turn canceled")
				}
				if errors.Is(err, context.DeadlineExceeded) {
					return turnResult{}, fmt.Errorf("turn timed out")
				}
				turnStatus = TurnEndStatusError
				al.emitEvent(
					EventKindError,
					ts.eventMeta("runTurn", "turn.error"),
					ErrorPayload{Stage: "llm_empty_retry", Message: err.Error()},
				)
				return turnResult{}, fmt.Errorf("LLM call failed during empty-response retry: %w", err)
			}
			if strings.TrimSpace(responseContent) == "" {
				responseContent = defaultResponse
				logger.WarnCF("agent", "LLM returned empty response after retry; using fallback message",
					map[string]any{"agent_id": ts.agent.ID, "iteration": iteration})
			}
			finalContent = responseContent
			logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
				map[string]any{
					"agent_id":      ts.agent.ID,
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		normalizedToolCalls := make([]providers.ToolCall, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			normalizedToolCalls = append(normalizedToolCalls, providers.NormalizeToolCall(tc))
		}

		toolNames := make([]string, 0, len(normalizedToolCalls))
		for _, tc := range normalizedToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("agent", "LLM requested tool calls",
			map[string]any{
				"agent_id":  ts.agent.ID,
				"tools":     toolNames,
				"count":     len(normalizedToolCalls),
				"iteration": iteration,
			})

		allResponsesHandled := len(normalizedToolCalls) > 0
		assistantMsg := providers.Message{
			Role:             "assistant",
			Content:          response.Content,
			ReasoningContent: response.ReasoningContent,
		}
		for _, tc := range normalizedToolCalls {
			argumentsJSON, marshalErr := json.Marshal(tc.Arguments)
			if marshalErr != nil {
				logger.WarnCF("agent", "failed to marshal tool call arguments", map[string]any{"tool": tc.Name, "error": marshalErr.Error()})
				argumentsJSON = []byte("{}")
			}
			extraContent := tc.ExtraContent
			thoughtSignature := ""
			if tc.Function != nil {
				thoughtSignature = tc.Function.ThoughtSignature
			}
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Name: tc.Name,
				Function: &providers.FunctionCall{
					Name:             tc.Name,
					Arguments:        string(argumentsJSON),
					ThoughtSignature: thoughtSignature,
				},
				ExtraContent:     extraContent,
				ThoughtSignature: thoughtSignature,
			})
		}
		messages = append(messages, assistantMsg)
		if !ts.opts.NoHistory {
			ts.agent.Sessions.AddFullMessage(ts.sessionKey, assistantMsg)
			ts.recordPersistedMessage(assistantMsg)
		}

		ts.setPhase(TurnPhaseTools)
		for i, tc := range normalizedToolCalls {
			if ts.hardAbortRequested() {
				turnStatus = TurnEndStatusAborted
				return al.abortTurn(ts)
			}

			// Unsanitize tool name from LLM — dots were replaced with underscores
			// for Anthropic/Azure API compatibility (e.g., "browser_navigate" → "browser.navigate").
			toolName := ts.agent.Tools.UnsanitizeToolName(tc.Name)
			toolArgs := cloneStringAnyMap(tc.Arguments)

			if al.hooks != nil {
				toolReq, decision := al.hooks.BeforeTool(turnCtx, &ToolCallHookRequest{
					Meta:      ts.eventMeta("runTurn", "turn.tool.before"),
					Tool:      toolName,
					Arguments: toolArgs,
					Channel:   ts.channel,
					ChatID:    ts.chatID,
				})
				switch decision.normalizedAction() {
				case HookActionContinue, HookActionModify:
					if toolReq != nil {
						toolName = toolReq.Tool
						toolArgs = toolReq.Arguments
					}
				case HookActionDenyTool:
					allResponsesHandled = false
					denyContent := hookDeniedToolContent("Tool execution denied by hook", decision.Reason)
					al.emitEvent(
						EventKindToolExecSkipped,
						ts.eventMeta("runTurn", "turn.tool.skipped"),
						ToolExecSkippedPayload{
							Tool:   toolName,
							Reason: denyContent,
						},
					)
					deniedMsg := providers.Message{
						Role:       "tool",
						Content:    denyContent,
						ToolCallID: tc.ID,
					}
					messages = append(messages, deniedMsg)
					if !ts.opts.NoHistory {
						ts.agent.Sessions.AddFullMessage(ts.sessionKey, deniedMsg)
						ts.recordPersistedMessage(deniedMsg)
					}
					continue
				case HookActionAbortTurn:
					turnStatus = TurnEndStatusError
					return turnResult{}, al.hookAbortError(ts, "before_tool", decision)
				case HookActionHardAbort:
					_ = ts.requestHardAbort()
					turnStatus = TurnEndStatusAborted
					return al.abortTurn(ts)
				}
			}

			if al.hooks != nil {
				approval := al.hooks.ApproveTool(turnCtx, &ToolApprovalRequest{
					Meta:      ts.eventMeta("runTurn", "turn.tool.approve"),
					Tool:      toolName,
					Arguments: toolArgs,
					Channel:   ts.channel,
					ChatID:    ts.chatID,
				})
				if !approval.IsApproved() {
					allResponsesHandled = false
					denyContent := hookDeniedToolContent("Tool execution denied by approval hook", approval.Reason)
					al.emitEvent(
						EventKindToolExecSkipped,
						ts.eventMeta("runTurn", "turn.tool.skipped"),
						ToolExecSkippedPayload{
							Tool:   toolName,
							Reason: denyContent,
						},
					)
					deniedMsg := providers.Message{
						Role:       "tool",
						Content:    denyContent,
						ToolCallID: tc.ID,
					}
					messages = append(messages, deniedMsg)
					if !ts.opts.NoHistory {
						ts.agent.Sessions.AddFullMessage(ts.sessionKey, deniedMsg)
						ts.recordPersistedMessage(deniedMsg)
					}
					continue
				}
			}

			argsJSON, marshalErr := json.Marshal(toolArgs)
			if marshalErr != nil {
				logger.WarnCF("agent", "failed to marshal tool args for preview", map[string]any{"tool": toolName, "error": marshalErr.Error()})
				argsJSON = []byte("{}")
			}
			argsPreview := utils.Truncate(string(argsJSON), 200)
			logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", toolName, argsPreview),
				map[string]any{
					"agent_id":  ts.agent.ID,
					"tool":      toolName,
					"iteration": iteration,
				})
			al.emitEvent(
				EventKindToolExecStart,
				ts.eventMeta("runTurn", "turn.tool.start"),
				ToolExecStartPayload{
					ToolCallID: tc.ID,
					ChatID:     ts.chatID,
					Tool:       toolName,
					Arguments:  cloneEventArguments(toolArgs),
				},
			)

			// Send tool feedback to chat channel if enabled
			if al.cfg.Agents.Defaults.IsToolFeedbackEnabled() &&
				ts.channel != "" &&
				!ts.opts.SuppressToolFeedback {
				feedbackPreview := utils.Truncate(
					string(argsJSON),
					al.cfg.Agents.Defaults.GetToolFeedbackMaxArgsLength(),
				)
				feedbackMsg := fmt.Sprintf("\U0001f527 `%s`\n```\n%s\n```", tc.Name, feedbackPreview)
				fbCtx, fbCancel := context.WithTimeout(turnCtx, 3*time.Second)
				if fbErr := al.bus.PublishOutbound(fbCtx, bus.OutboundMessage{
					Channel: ts.channel,
					ChatID:  ts.chatID,
					Content: feedbackMsg,
				}); fbErr != nil {
					logger.WarnCF("agent", "Failed to publish tool feedback",
						map[string]any{"tool": tc.Name, "channel": ts.channel, "error": fbErr.Error()})
				}
				fbCancel()
			}

			toolCallID := tc.ID
			toolIteration := iteration
			asyncToolName := toolName
			asyncCallback := func(_ context.Context, result *tools.ToolResult) {
				// Send ForUser content directly to the user (immediate feedback),
				// mirroring the synchronous tool execution path.
				if !result.Silent && result.ForUser != "" {
					outCtx, outCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer outCancel()
					// M1: capture and log publish errors instead of silently discarding them.
					if pubErr := al.bus.PublishOutbound(outCtx, bus.OutboundMessage{
						Channel: ts.channel,
						ChatID:  ts.chatID,
						Content: result.ForUser,
					}); pubErr != nil {
						logger.WarnCF("agent", "Async tool ForUser content failed to publish",
							map[string]any{
								"tool":    asyncToolName,
								"channel": ts.channel,
								"error":   pubErr.Error(),
							})
					}
				}

				// Determine content for the agent loop (ForLLM or error).
				content := result.ContentForLLM()
				if content == "" {
					return
				}

				// Filter sensitive data before publishing
				content = al.cfg.FilterSensitiveData(content)

				logger.InfoCF("agent", "Async tool completed, publishing result",
					map[string]any{
						"tool":        asyncToolName,
						"content_len": len(content),
						"channel":     ts.channel,
					})
				al.emitEvent(
					EventKindFollowUpQueued,
					ts.scope.meta(toolIteration, "runTurn", "turn.follow_up.queued"),
					FollowUpQueuedPayload{
						SourceTool: asyncToolName,
						Channel:    ts.channel,
						ChatID:     ts.chatID,
						ContentLen: len(content),
					},
				)

				pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer pubCancel()
				if pubErr := al.bus.PublishInbound(pubCtx, bus.InboundMessage{
					Channel:  "system",
					SenderID: fmt.Sprintf("async:%s", asyncToolName),
					ChatID:   fmt.Sprintf("%s:%s", ts.channel, ts.chatID),
					Content:  content,
				}); pubErr != nil {
					logger.ErrorCF("agent", "Failed to publish async tool result; result permanently lost",
						map[string]any{"tool": asyncToolName, "channel": ts.channel, "error": pubErr.Error()})
				}
			}

			// SEC-26: Per-agent tool call rate limit check. The system agent is exempt.
			if al.rateLimiter != nil && cfg.Sandbox.RateLimits.MaxAgentToolCallsPerMinute > 0 &&
				!security.IsSystemAgent(ts.agent.ID) {
				toolWindow := al.rateLimiter.GetOrCreate(
					"agent:"+ts.agent.ID+":tool_call",
					cfg.Sandbox.RateLimits.MaxAgentToolCallsPerMinute,
					time.Minute,
					security.ScopeAgent,
					ts.agent.ID,
					"tool_call",
				)
				if toolRLResult := toolWindow.Allow(); !toolRLResult.Allowed {
					al.recordRateLimitDenial(
						ts,
						"agent_tool_calls_per_minute",
						RateLimitPayload{
							Scope:             string(security.ScopeAgent),
							Resource:          "tool_call",
							PolicyRule:        toolRLResult.PolicyRule,
							RetryAfterSeconds: toolRLResult.RetryAfterSeconds,
							AgentID:           ts.agent.ID,
							ChatID:            ts.chatID,
							Tool:              toolName,
						},
						map[string]any{"retry_after_seconds": toolRLResult.RetryAfterSeconds},
					)
					// Soft denial: the tool call is rejected (fail closed — the tool
					// does not execute) but the denial is surfaced as a tool-result
					// error rather than aborting the turn, so the LLM can react
					// (e.g. inform the user, back off). Contrast with the LLM-call
					// rate limit above, which aborts the turn entirely.
					errMsg := fmt.Sprintf("Rate limited: %s (retry after %.0fs)",
						toolRLResult.PolicyRule, toolRLResult.RetryAfterSeconds)
					deniedMsg := providers.Message{
						Role:       "tool",
						Content:    errMsg,
						ToolCallID: tc.ID,
					}
					messages = append(messages, deniedMsg)
					if !ts.opts.NoHistory {
						ts.agent.Sessions.AddFullMessage(ts.sessionKey, deniedMsg)
						ts.recordPersistedMessage(deniedMsg)
					}
					allResponsesHandled = false
					al.emitEvent(
						EventKindToolExecSkipped,
						ts.eventMeta("runTurn", "turn.tool.skipped"),
						ToolExecSkippedPayload{
							Tool:   toolName,
							Reason: errMsg,
						},
					)
					continue
				}
			}

			toolStart := time.Now()
			toolResult := ts.agent.Tools.ExecuteWithContext(
				turnCtx,
				toolName,
				toolArgs,
				ts.channel,
				ts.chatID,
				asyncCallback,
			)
			toolDuration := time.Since(toolStart)

			if ts.hardAbortRequested() {
				turnStatus = TurnEndStatusAborted
				return al.abortTurn(ts)
			}

			if al.hooks != nil {
				toolResp, decision := al.hooks.AfterTool(turnCtx, &ToolResultHookResponse{
					Meta:      ts.eventMeta("runTurn", "turn.tool.after"),
					Tool:      toolName,
					Arguments: toolArgs,
					Result:    toolResult,
					Duration:  toolDuration,
					Channel:   ts.channel,
					ChatID:    ts.chatID,
				})
				switch decision.normalizedAction() {
				case HookActionContinue, HookActionModify:
					if toolResp != nil {
						if toolResp.Tool != "" {
							toolName = toolResp.Tool
						}
						if toolResp.Result != nil {
							toolResult = toolResp.Result
						}
					}
				case HookActionAbortTurn:
					turnStatus = TurnEndStatusError
					return turnResult{}, al.hookAbortError(ts, "after_tool", decision)
				case HookActionHardAbort:
					_ = ts.requestHardAbort()
					turnStatus = TurnEndStatusAborted
					return al.abortTurn(ts)
				}
			}

			if toolResult == nil {
				toolResult = tools.ErrorResult("hook returned nil tool result")
			}
			if len(toolResult.Media) > 0 && toolResult.ResponseHandled {
				parts := make([]bus.MediaPart, 0, len(toolResult.Media))
				for _, ref := range toolResult.Media {
					part := bus.MediaPart{Ref: ref}
					if al.mediaStore != nil {
						if _, meta, err := al.mediaStore.ResolveWithMeta(ref); err == nil {
							part.Filename = meta.Filename
							part.ContentType = meta.ContentType
							part.Type = inferMediaType(meta.Filename, meta.ContentType)
						}
					}
					parts = append(parts, part)
				}
				outboundMedia := bus.OutboundMediaMessage{
					Channel: ts.channel,
					ChatID:  ts.chatID,
					Parts:   parts,
				}
				if al.channelManager != nil && ts.channel != "" && !constants.IsInternalChannel(ts.channel) {
					if err := al.channelManager.SendMedia(ctx, outboundMedia); err != nil {
						logger.WarnCF("agent", "Failed to deliver handled tool media",
							map[string]any{
								"agent_id": ts.agent.ID,
								"tool":     toolName,
								"channel":  ts.channel,
								"chat_id":  ts.chatID,
								"error":    err.Error(),
							})
						toolResult = tools.ErrorResult(fmt.Sprintf("failed to deliver attachment: %v", err)).WithError(err)
					}
				} else if al.bus != nil {
					al.bus.PublishOutboundMedia(ctx, outboundMedia)
					// Queuing media is only best-effort; it has not been delivered yet.
					toolResult.ResponseHandled = false
				}
			}

			if len(toolResult.Media) > 0 && !toolResult.ResponseHandled {
				toolResult.ArtifactTags = buildArtifactTags(al.mediaStore, toolResult.Media)
			}

			if !toolResult.ResponseHandled {
				allResponsesHandled = false
			}

			if !toolResult.Silent && toolResult.ForUser != "" && ts.opts.SendResponse {
				if pubErr := al.bus.PublishOutbound(ctx, bus.OutboundMessage{
					Channel: ts.channel,
					ChatID:  ts.chatID,
					Content: toolResult.ForUser,
				}); pubErr != nil {
					logger.WarnCF("agent", "PublishOutbound failed for tool result",
						map[string]any{
							"tool":  toolName,
							"error": pubErr.Error(),
						})
				} else {
					logger.DebugCF("agent", "Sent tool result to user",
						map[string]any{
							"tool":        toolName,
							"content_len": len(toolResult.ForUser),
						})
				}
			}

			contentForLLM := toolResult.ContentForLLM()

			// SEC-25: Sanitize tool results from untrusted sources (web fetch,
			// web search, browser output, read_file) before they enter the
			// LLM's context. Trusted tools (exec, spawn, message, task_*,
			// file writes, etc.) are NEVER sanitized because their output is
			// either user-authored or produced by a peer agent inside the
			// same trust boundary.
			//
			// Order of operations: prompt guard FIRST, sensitive-data filter
			// SECOND. Reversing the order would let an injection payload
			// that mentions a secret pattern be partially redacted, leaving
			// the injection prefix intact and feeding it to the LLM.
			if al.promptGuard != nil && isUntrustedToolResult(toolName) {
				original := contentForLLM
				contentForLLM = al.promptGuard.Sanitize(contentForLLM, false)
				// Log every actual mutation to the operator stream AND to the
				// audit log (when enabled). Mutation is the signal the security
				// team cares about; logging no-op passes would drown real
				// events. The operator-stream log is unconditional so that
				// disabling audit logging does NOT hide prompt-guard rewrites.
				if contentForLLM != original {
					details := map[string]any{
						"action":          "prompt_guard_sanitize",
						"strictness":      string(al.promptGuard.Strictness()),
						"original_bytes":  len(original),
						"sanitized_bytes": len(contentForLLM),
						"tool":            toolName,
						"agent_id":        ts.agent.ID,
					}
					logger.InfoCF("agent", "prompt guard sanitized tool result", details)
					if al.auditLogger != nil {
						if err := al.auditLogger.Log(&audit.Entry{
							Event:    audit.EventPolicyEval,
							Decision: audit.DecisionAllow,
							AgentID:  ts.agent.ID,
							Tool:     toolName,
							Details:  details,
						}); err != nil {
							logger.WarnCF("agent", "failed to write prompt_guard audit entry",
								map[string]any{"error": err.Error(), "tool": toolName})
						}
					}
				}
			}

			// Filter sensitive data (API keys, tokens, secrets) before sending to LLM
			if al.cfg.Tools.IsFilterSensitiveDataEnabled() {
				contentForLLM = al.cfg.FilterSensitiveData(contentForLLM)
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: toolCallID,
			}
			al.emitEvent(
				EventKindToolExecEnd,
				ts.eventMeta("runTurn", "turn.tool.end"),
				ToolExecEndPayload{
					ToolCallID: toolCallID,
					ChatID:     ts.chatID,
					Tool:       toolName,
					Duration:   toolDuration,
					ForLLMLen:  len(contentForLLM),
					ForUserLen: len(toolResult.ForUser),
					IsError:    toolResult.IsError,
					Async:      toolResult.Async,
					Result:     contentForLLM,
				},
			)
			tcStatus := "success"
			if toolResult.IsError {
				tcStatus = "error"
			}
			ts.appendToolCallTranscript(session.ToolCall{
				ID:         toolCallID,
				Tool:       toolName,
				Status:     tcStatus,
				DurationMS: toolDuration.Milliseconds(),
				Parameters: cloneEventArguments(toolArgs),
			})
			messages = append(messages, toolResultMsg)
			if !ts.opts.NoHistory {
				ts.agent.Sessions.AddFullMessage(ts.sessionKey, toolResultMsg)
				ts.recordPersistedMessage(toolResultMsg)
			}

			if steerMsgs := al.dequeueSteeringMessagesForScope(ts.sessionKey); len(steerMsgs) > 0 {
				pendingMessages = append(pendingMessages, steerMsgs...)
			}

			skipReason := ""
			skipMessage := ""
			if len(pendingMessages) > 0 {
				skipReason = "queued user steering message"
				skipMessage = "Skipped due to queued user message."
			} else if gracefulPending, _ := ts.gracefulInterruptRequested(); gracefulPending {
				skipReason = "graceful interrupt requested"
				skipMessage = "Skipped due to graceful interrupt."
			}

			if skipReason != "" {
				remaining := len(normalizedToolCalls) - i - 1
				if remaining > 0 {
					logger.InfoCF("agent", "Turn checkpoint: skipping remaining tools",
						map[string]any{
							"agent_id":  ts.agent.ID,
							"completed": i + 1,
							"skipped":   remaining,
							"reason":    skipReason,
						})
					for j := i + 1; j < len(normalizedToolCalls); j++ {
						skippedTC := normalizedToolCalls[j]
						al.emitEvent(
							EventKindToolExecSkipped,
							ts.eventMeta("runTurn", "turn.tool.skipped"),
							ToolExecSkippedPayload{
								Tool:   skippedTC.Name,
								Reason: skipReason,
							},
						)
						skippedMsg := providers.Message{
							Role:       "tool",
							Content:    skipMessage,
							ToolCallID: skippedTC.ID,
						}
						messages = append(messages, skippedMsg)
						if !ts.opts.NoHistory {
							ts.agent.Sessions.AddFullMessage(ts.sessionKey, skippedMsg)
							ts.recordPersistedMessage(skippedMsg)
						}
					}
				}
				break
			}

			// Also poll for any SubTurn results that arrived during tool execution.
			if ts.pendingResults != nil {
				select {
				case result, ok := <-ts.pendingResults:
					if ok && result != nil && result.ForLLM != "" {
						content := al.cfg.FilterSensitiveData(result.ForLLM)
						msg := providers.Message{Role: "user", Content: fmt.Sprintf("[SubTurn Result] %s", content)}
						messages = append(messages, msg)
						if !ts.opts.NoHistory {
							ts.agent.Sessions.AddFullMessage(ts.sessionKey, msg)
						}
					}
				default:
					// No results available
				}
			}
		}

		if allResponsesHandled {
			if len(pendingMessages) > 0 {
				logger.InfoCF("agent", "Pending steering exists after handled tool delivery; continuing turn before finalizing",
					map[string]any{
						"agent_id":       ts.agent.ID,
						"steering_count": len(pendingMessages),
						"session_key":    ts.sessionKey,
					})
				finalContent = ""
				// I2: guard against bypassing the hard iteration ceiling via goto.
				if ts.currentIteration() >= 2*ts.agent.MaxIterations {
					break
				}
				goto turnLoop
			}

			if steerMsgs := al.dequeueSteeringMessagesForScope(ts.sessionKey); len(steerMsgs) > 0 {
				logger.InfoCF("agent", "Steering arrived after handled tool delivery; continuing turn before finalizing",
					map[string]any{
						"agent_id":       ts.agent.ID,
						"steering_count": len(steerMsgs),
						"session_key":    ts.sessionKey,
					})
				pendingMessages = append(pendingMessages, steerMsgs...)
				finalContent = ""
				// I2: guard against bypassing the hard iteration ceiling via goto.
				if ts.currentIteration() >= 2*ts.agent.MaxIterations {
					break
				}
				goto turnLoop
			}

			summaryMsg := providers.Message{
				Role:    "assistant",
				Content: handledToolResponseSummary,
			}

			if !ts.opts.NoHistory {
				ts.agent.Sessions.AddMessage(ts.sessionKey, summaryMsg.Role, summaryMsg.Content)
				ts.recordPersistedMessage(summaryMsg)
				if err := ts.agent.Sessions.Save(ts.sessionKey); err != nil {
					turnStatus = TurnEndStatusError
					al.emitEvent(
						EventKindError,
						ts.eventMeta("runTurn", "turn.error"),
						ErrorPayload{
							Stage:   "session_save",
							Message: err.Error(),
						},
					)
					return turnResult{}, err
				}
			}
			if ts.opts.EnableSummary {
				al.maybeSummarize(ts.agent, ts.sessionKey, ts.scope)
			}

			ts.setPhase(TurnPhaseCompleted)
			ts.setFinalContent("")
			logger.InfoCF("agent", "Tool output satisfied delivery; ending turn without follow-up LLM",
				map[string]any{
					"agent_id":   ts.agent.ID,
					"iteration":  iteration,
					"tool_count": len(normalizedToolCalls),
				})
			return turnResult{
				finalContent: "",
				status:       turnStatus,
				followUps:    append([]bus.InboundMessage(nil), ts.followUps...),
			}, nil
		}

		ts.agent.Tools.TickTTL()
		logger.DebugCF("agent", "TTL tick after tool execution", map[string]any{
			"agent_id": ts.agent.ID, "iteration": iteration,
		})
	}

	if steerMsgs := al.dequeueSteeringMessagesForScope(ts.sessionKey); len(steerMsgs) > 0 {
		logger.InfoCF("agent", "Steering arrived after turn completion; continuing turn before finalizing",
			map[string]any{
				"agent_id":       ts.agent.ID,
				"steering_count": len(steerMsgs),
				"session_key":    ts.sessionKey,
			})
		pendingMessages = append(pendingMessages, steerMsgs...)
		finalContent = ""
		// I2: guard against bypassing the hard iteration ceiling via goto.
		// If the ceiling is exceeded, fall through to finalization rather than
		// re-entering turnLoop, which would be invalid at this point anyway.
		if ts.currentIteration() < 2*ts.agent.MaxIterations {
			goto turnLoop
		}
	}

	if ts.hardAbortRequested() {
		turnStatus = TurnEndStatusAborted
		return al.abortTurn(ts)
	}

	if finalContent == "" {
		if ts.currentIteration() >= ts.agent.MaxIterations && ts.agent.MaxIterations > 0 {
			finalContent = toolLimitResponse
		} else {
			finalContent = ts.opts.DefaultResponse
		}
	}

	ts.setPhase(TurnPhaseFinalizing)
	ts.setFinalContent(finalContent)
	if !ts.opts.NoHistory {
		finalMsg := providers.Message{Role: "assistant", Content: finalContent}
		ts.agent.Sessions.AddMessage(ts.sessionKey, finalMsg.Role, finalMsg.Content)
		ts.recordPersistedMessage(finalMsg)
		if err := ts.agent.Sessions.Save(ts.sessionKey); err != nil {
			turnStatus = TurnEndStatusError
			al.emitEvent(
				EventKindError,
				ts.eventMeta("runTurn", "turn.error"),
				ErrorPayload{
					Stage:   "session_save",
					Message: err.Error(),
				},
			)
			return turnResult{}, err
		}
	}

	if ts.opts.EnableSummary {
		al.maybeSummarize(ts.agent, ts.sessionKey, ts.scope)
	}

	ts.setPhase(TurnPhaseCompleted)
	return turnResult{
		finalContent: finalContent,
		status:       turnStatus,
		followUps:    append([]bus.InboundMessage(nil), ts.followUps...),
	}, nil
}

func (al *AgentLoop) abortTurn(ts *turnState) (turnResult, error) {
	ts.setPhase(TurnPhaseAborted)
	if !ts.opts.NoHistory {
		if err := ts.restoreSession(ts.agent); err != nil {
			al.emitEvent(
				EventKindError,
				ts.eventMeta("abortTurn", "turn.error"),
				ErrorPayload{
					Stage:   "session_restore",
					Message: err.Error(),
				},
			)
			return turnResult{}, err
		}
	}
	return turnResult{status: TurnEndStatusAborted}, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// selectCandidates returns the model candidates and resolved model name to use
// for a conversation turn. When model routing is configured and the incoming
// message scores below the complexity threshold, it returns the light model
// candidates instead of the primary ones.
//
// The returned (candidates, model) pair is used for all LLM calls within one
// turn — tool follow-up iterations use the same tier as the initial call so
// that a multi-step tool chain doesn't switch models mid-way.
func (al *AgentLoop) selectCandidates(
	agent *AgentInstance,
	userMsg string,
	history []providers.Message,
) (candidates []providers.FallbackCandidate, model string, usedLight bool) {
	if agent.Router == nil || len(agent.LightCandidates) == 0 {
		return agent.Candidates, resolvedCandidateModel(agent.Candidates, agent.Model), false
	}

	_, usedLight, score := agent.Router.SelectModel(userMsg, history, agent.Model)
	if !usedLight {
		logger.DebugCF("agent", "Model routing: primary model selected",
			map[string]any{
				"agent_id":  agent.ID,
				"score":     score,
				"threshold": agent.Router.Threshold(),
			})
		return agent.Candidates, resolvedCandidateModel(agent.Candidates, agent.Model), false
	}

	logger.InfoCF("agent", "Model routing: light model selected",
		map[string]any{
			"agent_id":    agent.ID,
			"light_model": agent.Router.LightModel(),
			"score":       score,
			"threshold":   agent.Router.Threshold(),
		})
	return agent.LightCandidates, resolvedCandidateModel(agent.LightCandidates, agent.Router.LightModel()), true
}

// maybeSummarize triggers summarization if the session history exceeds thresholds.
func (al *AgentLoop) maybeSummarize(agent *AgentInstance, sessionKey string, turnScope turnEventScope) {
	newHistory := agent.Sessions.GetHistory(sessionKey)
	tokenEstimate := al.estimateTokens(newHistory)
	threshold := agent.ContextWindow * agent.SummarizeTokenPercent / 100

	if len(newHistory) > agent.SummarizeMessageThreshold || tokenEstimate > threshold {
		summarizeKey := agent.ID + ":" + sessionKey
		if _, loading := al.summarizing.LoadOrStore(summarizeKey, true); !loading {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.ErrorCF("agent", "Panic during summarization", map[string]any{"panic": r})
					}
					al.summarizing.Delete(summarizeKey)
				}()
				logger.Debug("Memory threshold reached. Optimizing conversation history...")
				al.summarizeSession(agent, sessionKey, turnScope)
			}()
		}
	}
}

type compressionResult struct {
	DroppedMessages   int
	RemainingMessages int
}

// forceCompression aggressively reduces context when the limit is hit.
// It drops the oldest ~50% of Turns (a Turn is a complete user→LLM→response
// cycle, as defined in #1316), so tool-call sequences are never split.
//
// If the history is a single Turn with no safe split point, the function
// falls back to keeping only the most recent user message. This breaks
// Turn atomicity as a last resort to avoid a context-exceeded loop.
//
// Session history contains only user/assistant/tool messages — the system
// prompt is built dynamically by BuildMessages and is NOT stored here.
// The compression note is recorded in the session summary so that
// BuildMessages can include it in the next system prompt.
func (al *AgentLoop) forceCompression(agent *AgentInstance, sessionKey string) (compressionResult, bool) {
	history := agent.Sessions.GetHistory(sessionKey)
	if len(history) <= 2 {
		return compressionResult{}, false
	}

	// Split at a Turn boundary so no tool-call sequence is torn apart.
	// parseTurnBoundaries gives us the start of each Turn; we drop the
	// oldest half of Turns and keep the most recent ones.
	turns := parseTurnBoundaries(history)
	var mid int
	if len(turns) >= 2 {
		mid = turns[len(turns)/2]
	} else {
		// Fewer than 2 Turns — fall back to message-level midpoint
		// aligned to the nearest Turn boundary.
		mid = findSafeBoundary(history, len(history)/2)
	}
	var keptHistory []providers.Message
	if mid <= 0 {
		// No safe Turn boundary — the entire history is a single Turn
		// (e.g. one user message followed by a massive tool response).
		// Keeping everything would leave the agent stuck in a context-
		// exceeded loop, so fall back to keeping only the most recent
		// user message. This breaks Turn atomicity as a last resort.
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "user" {
				keptHistory = []providers.Message{history[i]}
				break
			}
		}
	} else {
		keptHistory = history[mid:]
	}

	droppedCount := len(history) - len(keptHistory)

	// Record compression in the session summary so BuildMessages includes it
	// in the system prompt. We do not modify history messages themselves.
	existingSummary := agent.Sessions.GetSummary(sessionKey)
	compressionNote := fmt.Sprintf(
		"[Emergency compression dropped %d oldest messages due to context limit]",
		droppedCount,
	)
	if existingSummary != "" {
		compressionNote = existingSummary + "\n\n" + compressionNote
	}
	agent.Sessions.SetSummary(sessionKey, compressionNote)

	agent.Sessions.SetHistory(sessionKey, keptHistory)
	if saveErr := agent.Sessions.Save(sessionKey); saveErr != nil {
		logger.ErrorCF("agent", "forceCompression: failed to persist compressed session",
			map[string]any{"session_key": sessionKey, "error": saveErr.Error()})
	}

	logger.WarnCF("agent", "Forced compression executed", map[string]any{
		"session_key":  sessionKey,
		"dropped_msgs": droppedCount,
		"new_count":    len(keptHistory),
	})

	return compressionResult{
		DroppedMessages:   droppedCount,
		RemainingMessages: len(keptHistory),
	}, true
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) GetStartupInfo() map[string]any {
	info := make(map[string]any)

	registry := al.GetRegistry()
	agent := registry.GetDefaultAgent()
	if agent == nil {
		return info
	}

	// Tools info
	toolsList := agent.Tools.List()
	info["tools"] = map[string]any{
		"count": len(toolsList),
		"names": toolsList,
	}

	// Skills info
	info["skills"] = agent.ContextBuilder.GetSkillsInfo()

	// Agents info
	info["agents"] = map[string]any{
		"count": len(registry.ListAgentIDs()),
		"ids":   registry.ListAgentIDs(),
	}

	return info
}

// formatMessagesForLog formats messages for logging
func formatMessagesForLog(messages []providers.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	var sb strings.Builder
	sb.WriteString("[\n")
	for i, msg := range messages {
		fmt.Fprintf(&sb, "  [%d] Role: %s\n", i, msg.Role)
		if len(msg.ToolCalls) > 0 {
			sb.WriteString("  ToolCalls:\n")
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&sb, "    - ID: %s, Type: %s, Name: %s\n", tc.ID, tc.Type, tc.Name)
				if tc.Function != nil {
					fmt.Fprintf(
						&sb,
						"      Arguments: %s\n",
						utils.Truncate(tc.Function.Arguments, 200),
					)
				}
			}
		}
		if msg.Content != "" {
			content := utils.Truncate(msg.Content, 200)
			fmt.Fprintf(&sb, "  Content: %s\n", content)
		}
		if msg.ToolCallID != "" {
			fmt.Fprintf(&sb, "  ToolCallID: %s\n", msg.ToolCallID)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("]")
	return sb.String()
}

// formatToolsForLog formats tool definitions for logging
func formatToolsForLog(toolDefs []providers.ToolDefinition) string {
	if len(toolDefs) == 0 {
		return "[]"
	}

	var sb strings.Builder
	sb.WriteString("[\n")
	for i, tool := range toolDefs {
		fmt.Fprintf(&sb, "  [%d] Type: %s, Name: %s\n", i, tool.Type, tool.Function.Name)
		fmt.Fprintf(&sb, "      Description: %s\n", tool.Function.Description)
		if len(tool.Function.Parameters) > 0 {
			fmt.Fprintf(
				&sb,
				"      Parameters: %s\n",
				utils.Truncate(fmt.Sprintf("%v", tool.Function.Parameters), 200),
			)
		}
	}
	sb.WriteString("]")
	return sb.String()
}

// summarizeSession summarizes the conversation history for a session.
func (al *AgentLoop) summarizeSession(agent *AgentInstance, sessionKey string, turnScope turnEventScope) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	history := agent.Sessions.GetHistory(sessionKey)
	summary := agent.Sessions.GetSummary(sessionKey)

	// Keep the most recent Turns for continuity, aligned to a Turn boundary
	// so that no tool-call sequence is split.
	if len(history) <= 4 {
		return
	}

	safeCut := findSafeBoundary(history, len(history)-4)
	if safeCut <= 0 {
		return
	}
	keepCount := len(history) - safeCut
	toSummarize := history[:safeCut]

	// Oversized Message Guard
	maxMessageTokens := agent.ContextWindow / 2
	validMessages := make([]providers.Message, 0)
	omitted := false

	for _, m := range toSummarize {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		msgTokens := len(m.Content) / 2
		if msgTokens > maxMessageTokens {
			omitted = true
			continue
		}
		validMessages = append(validMessages, m)
	}

	if len(validMessages) == 0 {
		return
	}

	const (
		maxSummarizationMessages = 10
		llmMaxRetries            = 3
		llmTemperature           = 0.3
		fallbackMaxContentLength = 200
	)

	// Multi-Part Summarization
	var finalSummary string
	if len(validMessages) > maxSummarizationMessages {
		mid := len(validMessages) / 2

		mid = al.findNearestUserMessage(validMessages, mid)

		part1 := validMessages[:mid]
		part2 := validMessages[mid:]

		s1, _ := al.summarizeBatch(ctx, agent, part1, "")
		s2, _ := al.summarizeBatch(ctx, agent, part2, "")

		mergePrompt := fmt.Sprintf(
			"Merge these two conversation summaries into one cohesive summary:\n\n1: %s\n\n2: %s",
			s1,
			s2,
		)

		resp, err := al.retryLLMCall(ctx, agent, mergePrompt, llmMaxRetries)
		if err == nil && resp.Content != "" {
			finalSummary = resp.Content
		} else {
			finalSummary = s1 + " " + s2
		}
	} else {
		finalSummary, _ = al.summarizeBatch(ctx, agent, validMessages, summary)
	}

	if omitted && finalSummary != "" {
		finalSummary += "\n[Note: Some oversized messages were omitted from this summary for efficiency.]"
	}

	if finalSummary != "" {
		agent.Sessions.SetSummary(sessionKey, finalSummary)
		agent.Sessions.TruncateHistory(sessionKey, keepCount)
		if saveErr := agent.Sessions.Save(sessionKey); saveErr != nil {
			logger.ErrorCF("agent", "summarizeSession: failed to persist summarized session",
				map[string]any{"session_key": sessionKey, "error": saveErr.Error()})
		}
		al.emitEvent(
			EventKindSessionSummarize,
			turnScope.meta(0, "summarizeSession", "turn.session.summarize"),
			SessionSummarizePayload{
				SummarizedMessages: len(validMessages),
				KeptMessages:       keepCount,
				SummaryLen:         len(finalSummary),
				OmittedOversized:   omitted,
			},
		)
	}
}

// findNearestUserMessage finds the nearest user message to the given index.
// It searches backward first, then forward if no user message is found.
func (al *AgentLoop) findNearestUserMessage(messages []providers.Message, mid int) int {
	originalMid := mid

	for mid > 0 && messages[mid].Role != "user" {
		mid--
	}

	if messages[mid].Role == "user" {
		return mid
	}

	mid = originalMid
	for mid < len(messages) && messages[mid].Role != "user" {
		mid++
	}

	if mid < len(messages) {
		return mid
	}

	return originalMid
}

// retryLLMCall calls the LLM with retry logic.
func (al *AgentLoop) retryLLMCall(
	ctx context.Context,
	agent *AgentInstance,
	prompt string,
	maxRetries int,
) (*providers.LLMResponse, error) {
	const (
		llmTemperature = 0.3
	)

	var resp *providers.LLMResponse
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		al.activeRequests.Add(1)
		resp, err = func() (*providers.LLMResponse, error) {
			defer al.activeRequests.Done()
			return agent.Provider.Chat(
				ctx,
				[]providers.Message{{Role: "user", Content: prompt}},
				nil,
				agent.Model,
				map[string]any{
					"max_tokens":       agent.MaxTokens,
					"temperature":      llmTemperature,
					"prompt_cache_key": agent.ID,
				},
			)
		}()

		if err == nil && resp != nil && resp.Content != "" {
			return resp, nil
		}
		if attempt < maxRetries-1 {
			if sleepErr := sleepWithContext(ctx, time.Duration(attempt+1)*100*time.Millisecond); sleepErr != nil {
				return resp, sleepErr
			}
		}
	}

	return resp, err
}

// summarizeBatch summarizes a batch of messages.
func (al *AgentLoop) summarizeBatch(
	ctx context.Context,
	agent *AgentInstance,
	batch []providers.Message,
	existingSummary string,
) (string, error) {
	const (
		llmMaxRetries             = 3
		llmTemperature            = 0.3
		fallbackMinContentLength  = 200
		fallbackMaxContentPercent = 10
	)

	var sb strings.Builder
	sb.WriteString(
		"Provide a concise summary of this conversation segment, preserving core context and key points.\n",
	)
	if existingSummary != "" {
		sb.WriteString("Existing context: ")
		sb.WriteString(existingSummary)
		sb.WriteString("\n")
	}
	sb.WriteString("\nCONVERSATION:\n")
	for _, m := range batch {
		fmt.Fprintf(&sb, "%s: %s\n", m.Role, m.Content)
	}
	prompt := sb.String()

	response, err := al.retryLLMCall(ctx, agent, prompt, llmMaxRetries)
	if err == nil && response.Content != "" {
		return strings.TrimSpace(response.Content), nil
	}

	var fallback strings.Builder
	fallback.WriteString("Conversation summary: ")
	for i, m := range batch {
		if i > 0 {
			fallback.WriteString(" | ")
		}
		content := strings.TrimSpace(m.Content)
		runes := []rune(content)
		if len(runes) == 0 {
			fallback.WriteString(fmt.Sprintf("%s: ", m.Role))
			continue
		}

		keepLength := len(runes) * fallbackMaxContentPercent / 100
		if keepLength < fallbackMinContentLength {
			keepLength = fallbackMinContentLength
		}

		if keepLength > len(runes) {
			keepLength = len(runes)
		}

		content = string(runes[:keepLength])
		if keepLength < len(runes) {
			content += "..."
		}
		fallback.WriteString(fmt.Sprintf("%s: %s", m.Role, content))
	}
	return fallback.String(), nil
}

// estimateTokens estimates the number of tokens in a message list.
// Counts Content, ToolCalls arguments, and ToolCallID metadata so that
// tool-heavy conversations are not systematically undercounted.
func (al *AgentLoop) estimateTokens(messages []providers.Message) int {
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	return total
}

func (al *AgentLoop) handleCommand(
	ctx context.Context,
	msg bus.InboundMessage,
	agent *AgentInstance,
	opts *processOptions,
) (string, bool) {
	if !commands.HasCommandPrefix(msg.Content) {
		return "", false
	}

	if matched, handled, reply := al.applyExplicitSkillCommand(msg.Content, agent, opts); matched {
		return reply, handled
	}

	if al.cmdRegistry == nil {
		return "", false
	}

	rt := al.buildCommandsRuntime(agent, opts)
	executor := commands.NewExecutor(al.cmdRegistry, rt)

	var commandReply string
	result := executor.Execute(ctx, commands.Request{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		SenderID: msg.SenderID,
		Text:     msg.Content,
		Reply: func(text string) error {
			commandReply = text
			return nil
		},
	})

	switch result.Outcome {
	case commands.OutcomeHandled:
		if result.Err != nil {
			return mapCommandError(result), true
		}
		if commandReply != "" {
			return commandReply, true
		}
		return "", true
	default: // OutcomePassthrough — let the message fall through to LLM
		return "", false
	}
}

func activeSkillNames(agent *AgentInstance, opts processOptions) []string {
	if agent == nil {
		return nil
	}

	combined := make([]string, 0, len(agent.SkillsFilter)+len(opts.ForcedSkills))
	combined = append(combined, agent.SkillsFilter...)
	combined = append(combined, opts.ForcedSkills...)
	if len(combined) == 0 {
		return nil
	}

	var resolved []string
	seen := make(map[string]struct{}, len(combined))
	for _, name := range combined {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if agent.ContextBuilder != nil {
			if canonical, ok := agent.ContextBuilder.ResolveSkillName(name); ok {
				name = canonical
			}
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		resolved = append(resolved, name)
	}

	return resolved
}

func (al *AgentLoop) applyExplicitSkillCommand(
	raw string,
	agent *AgentInstance,
	opts *processOptions,
) (matched bool, handled bool, reply string) {
	cmdName, ok := commands.CommandName(raw)
	if !ok || cmdName != "use" {
		return false, false, ""
	}

	if agent == nil || agent.ContextBuilder == nil {
		return true, true, commandsUnavailableSkillMessage()
	}

	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) < 2 {
		return true, true, buildUseCommandHelp(agent)
	}

	arg := strings.TrimSpace(parts[1])
	if strings.EqualFold(arg, "clear") || strings.EqualFold(arg, "off") {
		if opts != nil {
			al.clearPendingSkills(opts.SessionKey)
		}
		return true, true, "Cleared pending skill override."
	}

	skillName, ok := agent.ContextBuilder.ResolveSkillName(arg)
	if !ok {
		return true, true, fmt.Sprintf("Unknown skill: %s\nUse /list skills to see installed skills.", arg)
	}

	if len(parts) < 3 {
		if opts == nil || strings.TrimSpace(opts.SessionKey) == "" {
			return true, true, commandsUnavailableSkillMessage()
		}
		al.setPendingSkills(opts.SessionKey, []string{skillName})
		return true, true, fmt.Sprintf(
			"Skill %q is armed for your next message. Send your next prompt normally, or use /use clear to cancel.",
			skillName,
		)
	}

	message := strings.TrimSpace(strings.Join(parts[2:], " "))
	if message == "" {
		return true, true, buildUseCommandHelp(agent)
	}

	if opts != nil {
		opts.ForcedSkills = append(opts.ForcedSkills, skillName)
		opts.UserMessage = message
	}

	return true, false, ""
}

func (al *AgentLoop) buildCommandsRuntime(agent *AgentInstance, opts *processOptions) *commands.Runtime {
	registry := al.GetRegistry()
	cfg := al.GetConfig()
	rt := &commands.Runtime{
		Config:          cfg,
		ListAgentIDs:    registry.ListAgentIDs,
		ListDefinitions: al.cmdRegistry.Definitions,
		GetEnabledChannels: func() []string {
			if al.channelManager == nil {
				return nil
			}
			return al.channelManager.GetEnabledChannels()
		},
		GetActiveTurn: func() any {
			info := al.GetActiveTurn()
			if info == nil {
				return nil
			}
			return info
		},
		SwitchChannel: func(value string) error {
			if al.channelManager == nil {
				return fmt.Errorf("channel manager not initialized")
			}
			if _, exists := al.channelManager.GetChannel(value); !exists && value != "cli" {
				return fmt.Errorf("channel '%s' not found or not enabled", value)
			}
			return nil
		},
	}
	if agent != nil && agent.ContextBuilder != nil {
		rt.ListSkillNames = agent.ContextBuilder.ListSkillNames
	}
	rt.ReloadConfig = func() error {
		if al.reloadFunc == nil {
			return fmt.Errorf("reload not configured")
		}
		return al.reloadFunc()
	}
	if agent != nil {
		if agent.ContextBuilder != nil {
			rt.ListSkillNames = agent.ContextBuilder.ListSkillNames
		}
		rt.GetModelInfo = func() (string, string) {
			agent.mu.RLock()
			m, c := agent.Model, agent.Candidates
			agent.mu.RUnlock()
			return m, resolvedCandidateProvider(c, cfg.Agents.Defaults.Provider)
		}
		rt.SwitchModel = func(value string) (string, error) {
			value = strings.TrimSpace(value)
			modelCfg, err := resolvedModelConfig(cfg, value, agent.Workspace)
			if err != nil {
				return "", err
			}

			nextProvider, _, err := providers.CreateProviderFromConfig(modelCfg)
			if err != nil {
				return "", fmt.Errorf("failed to initialize model %q: %w", value, err)
			}

			nextCandidates := resolveModelCandidates(cfg, cfg.Agents.Defaults.Provider, modelCfg.Model, agent.Fallbacks)
			if len(nextCandidates) == 0 {
				return "", fmt.Errorf("model %q did not resolve to any provider candidates", value)
			}

			agent.mu.Lock()
			oldModel := agent.Model
			oldProvider := agent.Provider
			agent.Model = value
			agent.Provider = nextProvider
			agent.Candidates = nextCandidates
			agent.ThinkingLevel = parseThinkingLevel(modelCfg.ThinkingLevel)
			agent.mu.Unlock()

			if oldProvider != nil && oldProvider != nextProvider {
				if stateful, ok := oldProvider.(providers.StatefulProvider); ok {
					stateful.Close()
				}
			}
			return oldModel, nil
		}

		rt.ClearHistory = func() error {
			if opts == nil {
				return fmt.Errorf("process options not available")
			}
			if agent.Sessions == nil {
				return fmt.Errorf("sessions not initialized for agent")
			}

			agent.Sessions.SetHistory(opts.SessionKey, make([]providers.Message, 0))
			agent.Sessions.SetSummary(opts.SessionKey, "")
			return agent.Sessions.Save(opts.SessionKey)
		}
	}
	return rt
}

func commandsUnavailableSkillMessage() string {
	return "Skill selection is unavailable in the current context."
}

func buildUseCommandHelp(agent *AgentInstance) string {
	if agent == nil || agent.ContextBuilder == nil {
		return "Usage: /use <skill> [message]"
	}

	names := agent.ContextBuilder.ListSkillNames()
	if len(names) == 0 {
		return "Usage: /use <skill> [message]\nNo installed skills found."
	}

	return fmt.Sprintf(
		"Usage: /use <skill> [message]\n\nInstalled Skills:\n- %s\n\nUse /use <skill> to apply a skill to your next message, or /use <skill> <message> to force it immediately.",
		strings.Join(names, "\n- "),
	)
}

func (al *AgentLoop) setPendingSkills(sessionKey string, skillNames []string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" || len(skillNames) == 0 {
		return
	}

	filtered := make([]string, 0, len(skillNames))
	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		return
	}

	al.pendingSkills.Store(sessionKey, filtered)
}

func (al *AgentLoop) takePendingSkills(sessionKey string) []string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return nil
	}

	value, ok := al.pendingSkills.LoadAndDelete(sessionKey)
	if !ok {
		return nil
	}

	skills, ok := value.([]string)
	if !ok {
		return nil
	}

	return append([]string(nil), skills...)
}

func (al *AgentLoop) clearPendingSkills(sessionKey string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	al.pendingSkills.Delete(sessionKey)
}

func mapCommandError(result commands.ExecuteResult) string {
	if result.Command == "" {
		return fmt.Sprintf("Failed to execute command: %v", result.Err)
	}
	return fmt.Sprintf("Failed to execute /%s: %v", result.Command, result.Err)
}

// extractPeer extracts the routing peer from the inbound message's structured Peer field.
func extractPeer(msg bus.InboundMessage) *routing.RoutePeer {
	if msg.Peer.Kind == "" {
		return nil
	}
	peerID := msg.Peer.ID
	if peerID == "" {
		if msg.Peer.Kind == "direct" {
			peerID = msg.SenderID
		} else {
			peerID = msg.ChatID
		}
	}
	return &routing.RoutePeer{Kind: string(msg.Peer.Kind), ID: peerID}
}

func inboundMetadata(msg bus.InboundMessage, key string) string {
	if msg.Metadata == nil {
		return ""
	}
	return msg.Metadata[key]
}

// extractParentPeer extracts the parent peer (reply-to) from inbound message metadata.
func extractParentPeer(msg bus.InboundMessage) *routing.RoutePeer {
	parentKind := inboundMetadata(msg, metadataKeyParentPeerKind)
	parentID := inboundMetadata(msg, metadataKeyParentPeerID)
	if parentKind == "" || parentID == "" {
		return nil
	}
	return &routing.RoutePeer{Kind: parentKind, ID: parentID}
}

// isNativeSearchProvider reports whether the given LLM provider implements
// NativeSearchCapable and returns true for SupportsNativeSearch.
func isNativeSearchProvider(p providers.LLMProvider) bool {
	if ns, ok := p.(providers.NativeSearchCapable); ok {
		return ns.SupportsNativeSearch()
	}
	return false
}

// filterClientWebSearch returns a copy of tools with the client-side
// web_search tool removed. Used when native provider search is preferred.
func filterClientWebSearch(tools []providers.ToolDefinition) []providers.ToolDefinition {
	result := make([]providers.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if strings.EqualFold(t.Function.Name, "web_search") {
			continue
		}
		result = append(result, t)
	}
	return result
}

// Helper to extract provider from registry for cleanup
func extractProvider(registry *AgentRegistry) (providers.LLMProvider, bool) {
	if registry == nil {
		return nil, false
	}
	// Get any agent to access the provider
	defaultAgent := registry.GetDefaultAgent()
	if defaultAgent == nil {
		return nil, false
	}
	return defaultAgent.Provider, true
}

// llmRate holds approximate per-1K-token pricing for a model family.
type llmRate struct {
	inputPer1K, outputPer1K float64
}

// llmRateFallback is used when no prefix in llmRateTable matches. It is
// deliberately conservative so unknown models over-count rather than silently
// escape the cost cap.
var llmRateFallback = llmRate{inputPer1K: 0.003, outputPer1K: 0.015}

// llmRateTable is an ordered prefix lookup — first match wins. Longer/more
// specific prefixes must appear before shorter ones. Rates are approximations
// for budgeting only and will not match provider invoices exactly.
var llmRateTable = []struct {
	prefix string
	rate   llmRate
}{
	// Anthropic Claude 3.x
	{"claude-3-5-haiku", llmRate{0.0008, 0.004}},
	{"claude-3-5-sonnet", llmRate{0.003, 0.015}},
	{"claude-3-haiku", llmRate{0.00025, 0.00125}},
	{"claude-3-sonnet", llmRate{0.003, 0.015}},
	{"claude-3-opus", llmRate{0.015, 0.075}},
	// Anthropic Claude 4.x
	{"claude-opus-4", llmRate{0.015, 0.075}},
	{"claude-sonnet-4", llmRate{0.003, 0.015}},
	{"claude-haiku-4", llmRate{0.0008, 0.004}},
	// OpenAI GPT-4 family
	{"gpt-4o-mini", llmRate{0.00015, 0.0006}},
	{"gpt-4o", llmRate{0.005, 0.015}},
	{"gpt-4-turbo", llmRate{0.01, 0.03}},
	{"gpt-4", llmRate{0.03, 0.06}},
	{"gpt-3.5-turbo", llmRate{0.0005, 0.0015}},
	// Google Gemini
	{"gemini-1.5-flash", llmRate{0.000075, 0.0003}},
	{"gemini-1.5-pro", llmRate{0.00125, 0.005}},
	{"gemini-2.0-flash", llmRate{0.0001, 0.0004}},
	{"gemini-2.5-pro", llmRate{0.00125, 0.01}},
}

// estimateLLMCallCost returns a conservative cost estimate in USD for a single
// LLM call given the model name and token usage (SEC-26). Unknown models fall
// back to llmRateFallback so the cost accumulator never under-counts.
func estimateLLMCallCost(model string, usage *providers.UsageInfo) float64 {
	if usage == nil {
		return 0
	}

	lowerModel := strings.ToLower(model)
	rate := llmRateFallback
	for _, entry := range llmRateTable {
		if strings.HasPrefix(lowerModel, entry.prefix) {
			rate = entry.rate
			break
		}
	}

	inputCost := float64(usage.PromptTokens) / 1000.0 * rate.inputPer1K
	outputCost := float64(usage.CompletionTokens) / 1000.0 * rate.outputPer1K
	return inputCost + outputCost
}

// braveKeys returns a []string for use as BraveAPIKeys. Returns nil if the key is empty.
func braveKeys(key string) []string {
	if key == "" {
		return nil
	}
	return []string{key}
}

// tavilyKeys returns a []string for use as TavilyAPIKeys. Returns nil if the key is empty.
func tavilyKeys(key string) []string {
	if key == "" {
		return nil
	}
	return []string{key}
}

// perplexityKeys returns a []string for use as PerplexityAPIKeys. Returns nil if the key is empty.
func perplexityKeys(key string) []string {
	if key == "" {
		return nil
	}
	return []string{key}
}

// WireSystemTools registers the 35 system.* tools from BuildRegistry into the
// system agent's tool registry (the "main"/"omnipus-system" agent). This must be
// called by the gateway after NewAgentLoop for production use; non-system agents
// do NOT receive system tools.
//
// WireSystemTools is deliberately separate from NewAgentLoop so that configPath
// and credStore (gateway-level concerns) do not leak into the core agent constructor.
// Callers that construct an AgentLoop in tests or non-gateway contexts can skip
// this wiring.
//
// navCb may be nil in headless or test environments — the navigate tool
// tolerates a nil callback and no-ops the navigation side-effect.
//
// WireSystemTools is idempotent: calling it multiple times overwrites any
// previously registered system.* tools with the same names (ToolRegistry.Register
// silently replaces existing entries).
//
// Returns an error if the system agent is not found in the registry — callers
// should treat this as a fatal boot failure.
func (al *AgentLoop) WireSystemTools(deps *systools.Deps, navCb systools.NavigateCallback) error {
	reg := al.GetRegistry()
	// The system agent is "omnipus-system", which the registry stores as "main".
	agentInst, ok := reg.GetAgent(DefaultAgentID)
	if !ok || agentInst == nil {
		return fmt.Errorf(
			"WireSystemTools: system agent %q not found in registry — agent loop may not have been initialized correctly",
			DefaultAgentID,
		)
	}
	sysRegistry := systools.BuildRegistry(deps, navCb)

	// Build the handler that enforces BRD-required guards on every system.* tool call:
	//   1. RBAC check (CheckRBAC) — SEC-19
	//   2. Rate limit check (SystemRateLimiter.Check)
	//   3. Confirmation requirement (RequiresConfirmation) — UI button gate
	//   4. Audit log entry (audit.Logger.Log) — SEC-15
	//
	// In open-source single-user mode we use RoleSingleUser (bypasses RBAC checks)
	// and a nil confirm func (destructive ops return CONFIRMATION_REQUIRED to the LLM,
	// which is the correct headless posture until the WebSocket layer wires a real confirm).
	handler := sysagent.NewSystemToolHandler(sysagent.HandlerConfig{
		Registry: sysRegistry,
		Audit:    al.auditLogger,
		Confirm:  nil, // wired by gateway WebSocket handler when UI is available
	})

	for _, toolName := range sysRegistry.List() {
		t, exists := sysRegistry.Get(toolName)
		if !exists {
			continue
		}
		guarded := sysagent.NewGuardedTool(t, handler, sysagent.RoleSingleUser, "gateway")
		agentInst.Tools.Register(guarded)
	}
	logger.InfoCF("agent", "System tools wired into system agent (guarded)",
		map[string]any{"agent_id": DefaultAgentID, "tool_count": len(sysRegistry.List())})
	return nil
}

// WireAvaAgentTools registers the 3 agent CRUD tools (system.agent.create,
// system.agent.update, system.agent.delete) on the "ava" core agent so she
// can create custom agents through her structured interview flow.
// These are wrapped with GuardedTool for RBAC + rate limiting + audit.
// WireAvaAgentTools registers the 3 agent CRUD tools on Ava.
// If reg is nil, the current registry is used. Pass a specific registry
// during hot-reload when the new registry hasn't been swapped yet.
func (al *AgentLoop) WireAvaAgentTools(deps *systools.Deps, reg ...*AgentRegistry) error {
	al.avaDeps = deps
	var r *AgentRegistry
	if len(reg) > 0 && reg[0] != nil {
		r = reg[0]
	} else {
		r = al.GetRegistry()
	}
	avaInst, ok := r.GetAgent("ava")
	if !ok || avaInst == nil {
		return fmt.Errorf("WireAvaAgentTools: agent 'ava' not found in registry")
	}

	handler := sysagent.NewSystemToolHandler(sysagent.HandlerConfig{
		Registry: systools.BuildRegistry(deps, nil),
		Audit:    al.auditLogger,
		Confirm:  nil,
	})

	// Wire agent CRUD tools + model lookup — Ava doesn't get the other system tools.
	agentToolNames := []string{
		"system.agent.create",
		"system.agent.update",
		"system.agent.delete",
		"system.models.list",
	}

	wired := 0
	var missing []string
	fullRegistry := systools.BuildRegistry(deps, nil)
	for _, name := range agentToolNames {
		t, exists := fullRegistry.Get(name)
		if !exists {
			missing = append(missing, name)
			continue
		}
		guarded := sysagent.NewGuardedTool(t, handler, sysagent.RoleSingleUser, "gateway").
			WithScopeOverride(tools.ScopeCore)
		avaInst.Tools.Register(guarded)
		wired++
	}
	if len(missing) > 0 {
		return fmt.Errorf("WireAvaAgentTools: %d/%d tools not found in registry: %v",
			len(missing), len(agentToolNames), missing)
	}

	// Inject available resources (tools, providers, defaults) into Ava's context
	// so she can recommend tools and models during the agent creation interview.
	avaInst.ContextBuilder.WithResourcesInjector(func() string {
		cfg := deps.GetCfg()
		var sb strings.Builder
		sb.WriteString("# Available Resources\n\n")

		// System defaults.
		sb.WriteString("## System Defaults\n")
		sb.WriteString(fmt.Sprintf("- Default model: `%s`\n", cfg.Agents.Defaults.ModelName))
		if len(cfg.Agents.Defaults.ModelFallbacks) > 0 {
			sb.WriteString(fmt.Sprintf("- Default fallbacks: %s\n", strings.Join(cfg.Agents.Defaults.ModelFallbacks, ", ")))
		}
		sb.WriteString("\n")

		// Connected providers.
		sb.WriteString("## Connected Providers\n")
		providersSeen := map[string]bool{}
		for _, p := range cfg.Providers {
			if p == nil {
				continue
			}
			name := p.Provider
			if name == "" {
				if idx := strings.IndexByte(p.Model, '/'); idx > 0 {
					name = p.Model[:idx]
				}
			}
			if name == "" || providersSeen[name] {
				continue
			}
			providersSeen[name] = true
			sb.WriteString(fmt.Sprintf("- %s\n", name))
		}
		if len(providersSeen) == 0 {
			sb.WriteString("- (none configured)\n")
		}
		sb.WriteString("\nUse `system.models.list` to see all available models from these providers.\n\n")

		// Builtin tools — from the centralized catalog (pkg/tools/catalog.go).
		sb.WriteString(tools.CatalogMarkdown())

		return sb.String()
	})

	logger.InfoCF("agent", "Agent CRUD tools wired into Ava (guarded)",
		map[string]any{"agent_id": "ava", "tool_count": wired})
	return nil
}
