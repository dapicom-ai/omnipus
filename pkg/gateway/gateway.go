//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	runtimedebug "runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/agent"
	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/bus"
	"github.com/dapicom-ai/omnipus/pkg/channels"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/dingtalk"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/discord"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/feishu"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/googlechat"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/irc"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/line"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/maixcam"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/onebot"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/qq"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/slack"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/telegram"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/wecom"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/weixin"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/whatsapp"
	_ "github.com/dapicom-ai/omnipus/pkg/channels/whatsapp_native"
	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/coreagent"
	"github.com/dapicom-ai/omnipus/pkg/credentials"
	"github.com/dapicom-ai/omnipus/pkg/cron"
	"github.com/dapicom-ai/omnipus/pkg/datamodel"
	"github.com/dapicom-ai/omnipus/pkg/devices"
	"github.com/dapicom-ai/omnipus/pkg/gateway/middleware"
	"github.com/dapicom-ai/omnipus/pkg/health"
	"github.com/dapicom-ai/omnipus/pkg/heartbeat"
	"github.com/dapicom-ai/omnipus/pkg/logger"
	"github.com/dapicom-ai/omnipus/pkg/media"
	"github.com/dapicom-ai/omnipus/pkg/onboarding"
	"github.com/dapicom-ai/omnipus/pkg/providers"
	"github.com/dapicom-ai/omnipus/pkg/state"
	systools "github.com/dapicom-ai/omnipus/pkg/sysagent/tools"
	"github.com/dapicom-ai/omnipus/pkg/tools"
	"github.com/dapicom-ai/omnipus/pkg/voice"
)

const (
	serviceShutdownTimeout  = 30 * time.Second
	providerReloadTimeout   = 30 * time.Second
	gracefulShutdownTimeout = 15 * time.Second

	logPath   = "logs"
	panicFile = "gateway_panic.log"
	logFile   = "gateway.log"
)

type services struct {
	CronService      *cron.CronService
	HeartbeatService *heartbeat.HeartbeatService
	MediaStore       media.MediaStore
	// ChannelManager is read-only to HTTP handlers (they access it via the
	// agent loop's GetChannelManager). It is written only during executeReload,
	// which is single-flighted by the reloading atomic.Bool. No handler reads
	// this field directly, so no additional lock is required for read access.
	ChannelManager   *channels.Manager
	DeviceService    *devices.Service
	HealthServer     *health.Server
	manualReloadChan chan struct{}
	reloading        atomic.Bool
	credStore        *credentials.Store
	// sandboxResult is the Apply/Install outcome from boot. Populated by
	// applySandbox before services start (and before any HTTP listener
	// binds). Read-only after initialization — sandbox config has no
	// hot-reload path, so this never changes for the process lifetime.
	sandboxResult *SandboxApplyResult
	// stopNagBanner cancels the permissive / production-off nag goroutine
	// on shutdown. No-op when no banner was armed.
	stopNagBanner func()
	// bundle is read-only to HTTP handlers (channels receive secrets at
	// construction time via SecretBundle; handlers do not access bundle
	// directly). Written only during executeReload under the reloading
	// single-flight guard. No additional lock is required for read access.
	bundle credentials.SecretBundle

	// reloadDegraded is set to true when a config reload fails and the service
	// is running on the last successfully loaded config. Cleared on next
	// successful reload. Protected by reloadMu.
	reloadMu       sync.Mutex
	reloadDegraded bool
	reloadError    error

	// Tier 1/3 preview-tool registries. Created once at boot; closed on shutdown.
	// servedSubdirs is always non-nil after a successful boot with preview enabled.
	// devServers is non-nil on the same condition.
	// egressProxy is non-nil when sandbox.EgressAllowList is non-empty or egress
	// enforcement is enabled.
	servedSubdirs *agent.ServedSubdirs
	devServers    *sandbox.DevServerRegistry
	egressProxy   *sandbox.EgressProxy
}

type startupBlockedProvider struct {
	reason string
}

func (p *startupBlockedProvider) Chat(
	_ context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	return nil, fmt.Errorf("%s", p.reason)
}

func (p *startupBlockedProvider) GetDefaultModel() string {
	return ""
}

// buildEnabledRefMap returns a set of credential ref names that belong to
// channels that are currently enabled. Used by bootCredentials to distinguish
// a missing credential on an enabled channel (fatal) from one on a disabled
// channel (Info + continue). Only channel refs are included — provider
// APIKeyRef misses are already fatal via InjectFromConfig.
func buildEnabledRefMap(cfg *config.Config) map[string]bool {
	m := make(map[string]bool)
	ch := cfg.Channels

	type channelRef struct {
		enabled bool
		refs    []string
	}
	entries := []channelRef{
		{ch.Telegram.Enabled, []string{ch.Telegram.TokenRef}},
		{ch.Discord.Enabled, []string{ch.Discord.TokenRef}},
		{ch.Slack.Enabled, []string{ch.Slack.BotTokenRef, ch.Slack.AppTokenRef}},
		{ch.Feishu.Enabled, []string{ch.Feishu.AppSecretRef, ch.Feishu.EncryptKeyRef, ch.Feishu.VerificationTokenRef}},
		{ch.QQ.Enabled, []string{ch.QQ.AppSecretRef}},
		{ch.DingTalk.Enabled, []string{ch.DingTalk.ClientSecretRef}},
		{ch.Matrix.Enabled, []string{ch.Matrix.AccessTokenRef, ch.Matrix.CryptoPassphraseRef}},
		{ch.LINE.Enabled, []string{ch.LINE.ChannelSecretRef, ch.LINE.ChannelAccessTokenRef}},
		{ch.OneBot.Enabled, []string{ch.OneBot.AccessTokenRef}},
		{ch.WeCom.Enabled, []string{ch.WeCom.SecretRef}},
		{ch.Weixin.Enabled, []string{ch.Weixin.TokenRef}},
		{ch.IRC.Enabled, []string{ch.IRC.PasswordRef, ch.IRC.NickServPasswordRef, ch.IRC.SASLPasswordRef}},
	}
	for _, e := range entries {
		if !e.enabled {
			continue
		}
		for _, ref := range e.refs {
			if ref != "" {
				m[ref] = true
			}
		}
	}
	return m
}

// bootCredentials runs the canonical credential + config boot sequence and
// returns the initialized config, secret bundle, and store.
//
// Sequence (matches ADR-004 §Boot Order Contract):
//  1. NewStore → Unlock (fatal on failure)
//  2. LoadConfigWithStore (fatal on failure)
//  3. InjectFromConfig for provider env-vars (fatal on failure)
//  4. ResolveBundle for channel secrets (NotFoundError for disabled channels is Info, rest Warn)
//  5. cfg.RegisterSensitiveValues with all resolved plaintexts
//
// Both Run and boot_order_test.go call this helper so that a refactor of one
// cannot silently drift from the other.
func bootCredentials(
	homePath, configPath string,
) (*config.Config, credentials.SecretBundle, *credentials.Store, error) {
	credStore := credentials.NewStore(filepath.Join(homePath, "credentials.json"))
	if unlockErr := credentials.Unlock(credStore); unlockErr != nil {
		return nil, nil, nil, fmt.Errorf("credential store: %w", unlockErr)
	}

	cfg, err := config.LoadConfigWithStore(configPath, credStore)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error loading config: %w", err)
	}

	// Inject provider API keys into the process environment so LLM SDK clients
	// can read them via os.Getenv. Channels use SecretBundle instead (no env injection).
	if errs := credentials.InjectFromConfig(cfg, credStore); len(errs) > 0 {
		for _, e := range errs {
			slog.Error("provider credential injection failed", "error", e)
		}
		return nil, nil, nil, fmt.Errorf(
			"fatal: provider credential injection failed — ensure OMNIPUS_MASTER_KEY is set and all referenced credentials exist",
		)
	}

	// Build a ref→enabled map so we can distinguish a missing credential on an
	// enabled channel (fatal) from one on a disabled channel (Info + continue).
	enabledRefs := buildEnabledRefMap(cfg)

	// Resolve all credential refs into a SecretBundle. Channels receive secrets
	// via the bundle — no os.Setenv for channel credentials (B1 fix).
	bundle, bundleErrs := credentials.ResolveBundle(cfg, credStore)
	for _, e := range bundleErrs {
		var notFound *credentials.NotFoundError
		if errors.As(e, &notFound) {
			if enabledRefs[notFound.Name] {
				// Missing credential on an enabled channel is fatal at boot.
				return nil, nil, nil, fmt.Errorf(
					"fatal: enabled channel credential %q not found in store — "+
						"ensure the credential is stored before starting: %w",
					notFound.Name, e,
				)
			}
			slog.Info("channel credential not found (channel is disabled)", "ref", notFound.Name)
			continue
		}
		slog.Warn("credential bundle resolution error", "error", e)
	}

	// Register all resolved plaintext credentials with the config's sensitive-data
	// replacer so they are scrubbed from LLM output and audit logs (A1 fix).
	// Semantics are "replace", so every call installs the complete current set.
	values := make([]string, 0, len(bundle))
	for _, v := range bundle {
		if v != "" {
			values = append(values, v)
		}
	}
	cfg.RegisterSensitiveValues(values)

	return cfg, bundle, credStore, nil
}

// RunOptions carries the inputs for the gateway runtime. Kept as a struct
// so new Sprint-J options (SandboxMode) and future options can be added
// without churning the Run signature. The legacy Run function remains as a
// thin wrapper for callers that have not migrated.
type RunOptions struct {
	Debug             bool
	HomePath          string
	ConfigPath        string
	AllowEmptyStartup bool
	// SandboxMode is the value of the --sandbox CLI flag ("enforce",
	// "permissive", "off"). Empty means "no flag set, use config". See
	// FR-J-006 for CLI > config > default precedence.
	SandboxMode string
}

// SandboxBootError wraps a sandbox Apply/Install failure so the CLI entry
// point can distinguish it from generic boot errors and exit with the
// Sprint-J-specific EX_CONFIG (78) code per FR-J-004.
type SandboxBootError struct {
	Err error
}

// Error makes SandboxBootError satisfy the error interface.
func (e *SandboxBootError) Error() string {
	if e == nil || e.Err == nil {
		return "sandbox boot error"
	}
	return e.Err.Error()
}

// Unwrap exposes the underlying error for errors.Is/As traversal.
func (e *SandboxBootError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Run starts the gateway runtime using the configuration loaded from configPath.
// It installs OS signal handlers (SIGINT, SIGTERM) and blocks until one fires,
// then delegates to RunContext for the actual boot and serve logic.
// Zero behavior change from the caller's perspective — the CLI entry point
// continues to call Run unchanged.
//
// For Sprint-J options (--sandbox), call RunWithOptions instead.
func Run(debug bool, homePath, configPath string, allowEmptyStartup bool) error {
	return RunWithOptions(RunOptions{
		Debug:             debug,
		HomePath:          homePath,
		ConfigPath:        configPath,
		AllowEmptyStartup: allowEmptyStartup,
	})
}

// RunWithOptions is the Sprint-J entry point. Handles the same boot flow as
// Run but accepts the expanded RunOptions struct (including SandboxMode).
// Installs OS signal handlers the same way Run does, then delegates to
// RunContextWithOptions.
func RunWithOptions(opts RunOptions) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	go func() {
		select {
		case <-sigChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	return RunContextWithOptions(ctx, opts)
}

// RunContext is the context-cancellable entry point for the gateway runtime.
// Run is a thin signal-driven wrapper around this function. Tests call RunContext
// directly with a context they control, enabling in-process integration testing
// without signal wiring.
//
// The caller is responsible for canceling ctx when the gateway should shut down.
// RunContext blocks until ctx is canceled or a fatal error occurs, then performs
// the full graceful shutdown sequence (channels → agent loop → background services
// → provider) before returning.
//
// Tests that need to override Sprint-J options (sandbox mode) should call
// RunContextWithOptions instead.
func RunContext(ctx context.Context, debug bool, homePath, configPath string, allowEmptyStartup bool) error {
	return RunContextWithOptions(ctx, RunOptions{
		Debug:             debug,
		HomePath:          homePath,
		ConfigPath:        configPath,
		AllowEmptyStartup: allowEmptyStartup,
	})
}

// RunContextWithOptions is the Sprint-J context-cancellable entry point.
// RunContext is a thin wrapper that builds a legacy RunOptions and calls this.
func RunContextWithOptions(ctx context.Context, opts RunOptions) error {
	debug := opts.Debug
	homePath := opts.HomePath
	configPath := opts.ConfigPath
	allowEmptyStartup := opts.AllowEmptyStartup
	// Bootstrap ~/.omnipus/ directory tree on every start (idempotent, US-1).
	if err := datamodel.Init(homePath); err != nil {
		return fmt.Errorf("directory initialization failed: %w", err)
	}

	panicPath := filepath.Join(homePath, logPath, panicFile)
	panicFunc, err := logger.InitPanic(panicPath)
	if err != nil {
		return fmt.Errorf("error initializing panic log: %w", err)
	}
	defer panicFunc()

	if err = logger.EnableFileLogging(filepath.Join(homePath, logPath, logFile)); err != nil {
		panic(fmt.Errorf("error enabling file logging: %w", err))
	}
	defer logger.DisableFileLogging()

	// Construct and unlock the credential store BEFORE loading config so that
	// v0→v1 migration (MigrateWithStore) can persist legacy plaintext secrets.
	// Implements BRD SEC-22/SEC-23 deny-by-default behavior.
	cfg, bundle, credStore, err := bootCredentials(homePath, configPath)
	if err != nil {
		return err
	}

	logger.SetLevelFromString(cfg.Gateway.LogLevel)

	if debug {
		logger.SetLevel(logger.DEBUG)
		fmt.Println("🔍 Debug mode enabled")
	}

	// Check for a test provider override BEFORE calling createStartupProvider.
	// If one is installed, bypass the real provider creation entirely — the
	// override factory supplies the provider directly. This hook is always nil
	// in production. See pkg/gateway/testhook.go / testhook_stub.go.
	var provider providers.LLMProvider
	var modelID string
	if overridePtr := testProviderOverride.Load(); overridePtr != nil {
		provider = (*overridePtr)()
	} else {
		provider, modelID, err = createStartupProvider(cfg, allowEmptyStartup)
		if err != nil {
			return fmt.Errorf("error creating provider: %w", err)
		}
	}

	// Only override ModelName if it was empty (first boot / migration).
	// Don't overwrite an alias (e.g. "openrouter-auto") with the raw model slug
	// (e.g. "z-ai/glm-5-turbo") — the alias is what GetModelConfig looks up by.
	if modelID != "" && cfg.Agents.Defaults.ModelName == "" {
		cfg.Agents.Defaults.ModelName = modelID
	}

	// Seed core agents into config on first boot. Core agents are stored in
	// cfg.Agents.List with Locked=true so they appear alongside custom agents
	// in the REST API with type "core". SeedConfig is idempotent — it only adds
	// agents that are not already present (checked by ID).
	if coreagent.SeedConfig(cfg) {
		if saveErr := config.SaveConfig(configPath, cfg); saveErr != nil {
			return fmt.Errorf("gateway: failed to persist seeded core agents: %w", saveErr)
		}
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Apply the kernel sandbox to the gateway process BEFORE any HTTP listener
	// binds. Strict ordering:
	//   unlock → config → NewAgentLoop → applySandbox → setupAndStartServices
	// where setupAndStartServices ends in ChannelManager.StartAll which calls
	// ListenAndServe on the shared HTTP server. During the Apply→Install→listen
	// window, external TCP probes receive ECONNREFUSED because the socket does
	// not exist yet.
	sandboxResult, sandboxErr := applySandbox(SandboxApplyOptions{
		CLIMode:  opts.SandboxMode,
		Cfg:      cfg,
		HomePath: homePath,
		Backend:  agentLoop.SandboxBackend(),
	})
	if sandboxErr != nil {
		// FR-J-004: kernel apply failure on a capable kernel is fatal.
		// Never bind the HTTP listener in this state — a half-sandboxed
		// process is worse than failing to boot. Wrapping in
		// SandboxBootError lets cmd/omnipus map this to exit code 78.
		slog.Error("gateway: sandbox apply failed — aborting boot",
			"error", sandboxErr,
			"requested_mode", opts.SandboxMode)
		return &SandboxBootError{Err: sandboxErr}
	}

	// Arm the permissive / production-off nag banner BEFORE the listener
	// binds so operators see the warning even during a fast crash-loop.
	stopNag := StartNagBanner(sandboxResult.NagReason, nil)

	// Wire agent CRUD tools (system.agent.create/update/delete) to Ava so she
	// can create custom agents through her structured interview flow.
	// reloadFuncRef is set after services start; the closure captures the pointer
	// so avaDeps.ReloadFunc is safe to call even if invoked before assignment
	// (returns "reload not yet available" instead of nil panic).
	var reloadFuncRef func() error
	avaDeps := &systools.Deps{
		Home:         homePath,
		ConfigPath:   configPath,
		GetCfg:       agentLoop.GetConfig,
		MutateConfig: agentLoop.MutateConfig,
		SaveConfigLocked: func(cfg *config.Config) error {
			return config.SaveConfig(configPath, cfg)
		},
		CredStore: credStore,
		ReloadFunc: func() error {
			if reloadFuncRef == nil {
				return fmt.Errorf("reload not yet available — gateway still starting")
			}
			return reloadFuncRef()
		},
	}
	if wireErr := agentLoop.WireAvaAgentTools(avaDeps); wireErr != nil {
		slog.Error("gateway: failed to wire Ava agent tools — Ava cannot create agents", "error", wireErr)
	}

	fmt.Println("\n📦 Agent Status:")
	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo, _ := startupInfo["tools"].(map[string]any)
	skillsInfo, _ := startupInfo["skills"].(map[string]any)
	if toolsInfo == nil {
		toolsInfo = map[string]any{"count": 0}
	}
	if skillsInfo == nil {
		skillsInfo = map[string]any{"available": 0, "total": 0}
	}
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n", skillsInfo["available"], skillsInfo["total"])

	logger.InfoCF("agent", "Agent initialized",
		map[string]any{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})

	runningServices, err := setupAndStartServices(cfg, bundle, agentLoop, msgBus, homePath, credStore, sandboxResult)
	if err != nil {
		stopNag() // don't leak the nag goroutine if service setup fails.
		return err
	}
	runningServices.stopNagBanner = stopNag

	// Surface sandbox state on /health via the existing degraded/check
	// infrastructure. Registering a RegisterCheck puts the {mode, backend,
	// applied} triplet into the /health response body.
	if runningServices.HealthServer != nil && sandboxResult != nil {
		registerSandboxHealthCheck(runningServices.HealthServer, sandboxResult)
	}

	// Setup manual reload channel for /reload endpoint
	manualReloadChan := make(chan struct{}, 1)
	runningServices.manualReloadChan = manualReloadChan
	reloadTrigger := func() error {
		if !runningServices.reloading.CompareAndSwap(false, true) {
			return fmt.Errorf("reload already in progress")
		}
		select {
		case manualReloadChan <- struct{}{}:
			return nil
		default:
			// Should not happen, but reset flag if channel is full
			runningServices.reloading.Store(false)
			return fmt.Errorf("reload already queued")
		}
	}
	runningServices.HealthServer.SetReloadFunc(reloadTrigger)
	agentLoop.SetReloadFunc(reloadTrigger)
	// Wire reload trigger into Ava's deps so agent create triggers hot-reload.
	reloadFuncRef = reloadTrigger

	fmt.Printf("✓ Gateway started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop")

	// agentLoopCtx is canceled if the agent loop exits unexpectedly (e.g. panic
	// recovery). The outer select below treats this the same as ctx cancellation.
	agentLoopCtx, agentLoopCancel := context.WithCancel(ctx)
	defer agentLoopCancel()

	// agentLoopDead is set when the agent loop exits (normally or via panic).
	// The /health endpoint returns 503 when this flag is set, signaling to
	// load-balancers and monitors that the gateway is no longer functional.
	var agentLoopDead atomic.Bool

	go func() {
		defer agentLoopCancel()
		defer func() {
			if r := recover(); r != nil {
				stack := runtimedebug.Stack()
				slog.Error("agent loop panicked — gateway is now non-functional",
					"panic", r, "stack", string(stack))
				// Append to the panic log file so ops can find the crash.
				if f, openErr := os.OpenFile(panicPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600); openErr == nil {
					fmt.Fprintf(f, "\n\nagent loop panic: %v\n%s\n", r, stack)
					f.Close()
				}
				agentLoopDead.Store(true)
			}
		}()
		agentLoop.Run(agentLoopCtx)
	}()

	// Launch the nightly retention sweep goroutine. Uses ctx (not agentLoopCtx)
	// so it shuts down on gateway stop regardless of agent-loop liveness.
	// GetSessionStore returns the shared UnifiedStore; when nil (misconfigured
	// home) the goroutine is a no-op — getCfg returning a nil cfg is guarded
	// inside executeSweepTick.
	if sharedStore := agentLoop.GetSessionStore(); sharedStore != nil {
		startRetentionSweepLoop(ctx, sharedStore, agentLoop.GetConfig, 24*time.Hour)
	}

	// Wire a second degraded check: report 503 when the agent loop has died.
	runningServices.HealthServer.SetDegradedFunc(func() (bool, string) {
		if agentLoopDead.Load() {
			return true, "agent loop exited unexpectedly — gateway requires restart"
		}
		runningServices.reloadMu.Lock()
		defer runningServices.reloadMu.Unlock()
		if runningServices.reloadDegraded {
			return true, fmt.Sprintf("config reload failed: %v", runningServices.reloadError)
		}
		return false, ""
	})

	var configReloadChan <-chan *config.Config
	stopWatch := func() {}
	if cfg.Gateway.HotReload {
		configReloadChan, stopWatch = setupConfigWatcherPolling(configPath, debug, credStore)
		logger.Info("Config hot reload enabled")
	}
	defer stopWatch()

	for {
		select {
		case <-agentLoopCtx.Done():
			logger.Info("Shutting down...")
			omnipusGracefulShutdown(runningServices, agentLoop, provider, cfg)
			return nil
		case newCfg := <-configReloadChan:
			if !runningServices.reloading.CompareAndSwap(false, true) {
				logger.Warn("Config reload skipped: another reload is in progress")
				continue
			}
			err := executeReload(ctx, agentLoop, newCfg, &provider, runningServices, msgBus, allowEmptyStartup)
			if err != nil {
				logger.Errorf("Config reload failed: %v", err)
			}
		case <-manualReloadChan:
			logger.Info("Manual reload triggered via /reload endpoint")
			newCfg, err := config.LoadConfigWithStore(configPath, credStore)
			if err != nil {
				logger.Errorf("Error loading config for manual reload: %v", err)
				runningServices.reloading.Store(false)
				continue
			}
			if err = newCfg.ValidateProviders(); err != nil {
				logger.Errorf("Config validation failed: %v", err)
				runningServices.reloading.Store(false)
				continue
			}
			err = executeReload(ctx, agentLoop, newCfg, &provider, runningServices, msgBus, allowEmptyStartup)
			if err != nil {
				logger.Errorf("Manual reload failed: %v", err)
			} else {
				logger.Info("Manual reload completed successfully")
			}
		}
	}
}

// servicesSnapshot captures all fields that restartServices and executeReload
// mutate, so they can be atomically restored on reload failure.
type servicesSnapshot struct {
	bundle           credentials.SecretBundle
	ChannelManager   *channels.Manager
	CronService      *cron.CronService
	HeartbeatService *heartbeat.HeartbeatService
	MediaStore       media.MediaStore
	DeviceService    *devices.Service
}

func snapshotServices(svc *services) servicesSnapshot {
	return servicesSnapshot{
		bundle:           svc.bundle,
		ChannelManager:   svc.ChannelManager,
		CronService:      svc.CronService,
		HeartbeatService: svc.HeartbeatService,
		MediaStore:       svc.MediaStore,
		DeviceService:    svc.DeviceService,
	}
}

func restoreServices(svc *services, snap servicesSnapshot) {
	svc.bundle = snap.bundle
	svc.ChannelManager = snap.ChannelManager
	svc.CronService = snap.CronService
	svc.HeartbeatService = snap.HeartbeatService
	svc.MediaStore = snap.MediaStore
	svc.DeviceService = snap.DeviceService
}

func executeReload(
	ctx context.Context,
	agentLoop *agent.AgentLoop,
	newCfg *config.Config,
	provider *providers.LLMProvider,
	runningServices *services,
	msgBus *bus.MessageBus,
	allowEmptyStartup bool,
) error {
	defer runningServices.reloading.Store(false)

	// Snapshot all service fields that restartServices mutates so they can be
	// restored atomically if the reload fails. bundle and ChannelManager are
	// mutated here in executeReload itself; the rest are mutated in
	// restartServices (CronService, HeartbeatService, MediaStore, DeviceService).
	snap := snapshotServices(runningServices)

	markDegraded := func(err error) {
		slog.Error("config reload failed — rolling back to previous in-memory state", "error", err)
		restoreServices(runningServices, snap)
		runningServices.reloadMu.Lock()
		runningServices.reloadDegraded = true
		runningServices.reloadError = err
		runningServices.reloadMu.Unlock()
	}
	clearDegraded := func() {
		runningServices.reloadMu.Lock()
		runningServices.reloadDegraded = false
		runningServices.reloadError = nil
		runningServices.reloadMu.Unlock()
	}

	// Re-inject provider credentials for the new config so LLM SDK clients
	// receive their secrets. If injection fails, reject the reload.
	if cs := runningServices.credStore; cs != nil {
		if errs := credentials.InjectFromConfig(newCfg, cs); len(errs) > 0 {
			for _, e := range errs {
				slog.Error("reload: provider credential injection failed — rejecting reload", "error", e)
			}
			reloadErr := fmt.Errorf("reload rejected: provider credential injection failed")
			markDegraded(reloadErr)
			return reloadErr
		}

		// Re-resolve the SecretBundle for channels (no os.Setenv for channel creds).
		newBundle, bundleErrs := credentials.ResolveBundle(newCfg, cs)
		for _, e := range bundleErrs {
			var notFound *credentials.NotFoundError
			if errors.As(e, &notFound) {
				slog.Info("reload: channel credential not found (channel may be disabled)", "error", e)
				continue
			}
			slog.Warn("reload: credential bundle resolution error", "error", e)
		}
		runningServices.bundle = newBundle

		// Re-register resolved plaintexts so the scrubber stays current after reload.
		reloadValues := make([]string, 0, len(newBundle))
		for _, v := range newBundle {
			if v != "" {
				reloadValues = append(reloadValues, v)
			}
		}
		if len(reloadValues) > 0 {
			newCfg.RegisterSensitiveValues(reloadValues)
		}
	}
	if err := handleConfigReload(
		ctx,
		agentLoop,
		newCfg,
		provider,
		runningServices,
		msgBus,
		allowEmptyStartup,
	); err != nil {
		markDegraded(err)
		return err
	}
	clearDegraded()
	return nil
}

func createStartupProvider(
	cfg *config.Config,
	allowEmptyStartup bool,
) (providers.LLMProvider, string, error) {
	modelName := cfg.Agents.Defaults.GetModelName()
	if modelName == "" && allowEmptyStartup {
		reason := "no default model configured; gateway started in limited mode"
		fmt.Printf("⚠ Warning: %s\n", reason)
		logger.WarnCF("gateway", "Gateway started without default model", map[string]any{
			"limited_mode": true,
		})
		return &startupBlockedProvider{reason: reason}, "", nil
	}

	return providers.CreateProvider(cfg)
}

func setupAndStartServices(
	cfg *config.Config,
	bundle credentials.SecretBundle,
	agentLoop *agent.AgentLoop,
	msgBus *bus.MessageBus,
	homePath string,
	credStore *credentials.Store,
	sandboxResult *SandboxApplyResult,
) (*services, error) {
	runningServices := &services{credStore: credStore, bundle: bundle, sandboxResult: sandboxResult}

	execTimeout := time.Duration(cfg.Tools.Cron.ExecTimeoutMinutes) * time.Minute
	var err error
	runningServices.CronService, err = setupCronTool(
		agentLoop,
		msgBus,
		cfg.WorkspacePath(),
		cfg.Agents.Defaults.RestrictToWorkspace,
		execTimeout,
		cfg,
	)
	if err != nil {
		return nil, fmt.Errorf("error setting up cron service: %w", err)
	}
	if err = runningServices.CronService.Start(); err != nil {
		return nil, fmt.Errorf("error starting cron service: %w", err)
	}
	fmt.Println("✓ Cron service started")

	runningServices.HeartbeatService = heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	runningServices.HeartbeatService.SetBus(msgBus)
	runningServices.HeartbeatService.SetHandler(createHeartbeatHandler(agentLoop))
	if te := agent.GetTaskExecutor(agentLoop); te != nil {
		runningServices.HeartbeatService.SetTaskChecker(te)
	}
	if err = runningServices.HeartbeatService.Start(); err != nil {
		return nil, fmt.Errorf("error starting heartbeat service: %w", err)
	}
	fmt.Println("✓ Heartbeat service started")

	runningServices.MediaStore = media.NewFileMediaStoreWithCleanup(media.MediaCleanerConfig{
		Enabled:  cfg.Tools.MediaCleanup.Enabled,
		MaxAge:   time.Duration(cfg.Tools.MediaCleanup.MaxAge) * time.Minute,
		Interval: time.Duration(cfg.Tools.MediaCleanup.Interval) * time.Minute,
	})
	if fms, ok := runningServices.MediaStore.(*media.FileMediaStore); ok {
		fms.Start()
	}

	runningServices.ChannelManager, err = channels.NewManager(
		cfg,
		runningServices.bundle,
		msgBus,
		runningServices.MediaStore,
	)
	if err != nil {
		if fms, ok := runningServices.MediaStore.(*media.FileMediaStore); ok {
			fms.Stop()
		}
		return nil, fmt.Errorf("error creating channel manager: %w", err)
	}

	agentLoop.SetChannelManager(runningServices.ChannelManager)
	agentLoop.SetMediaStore(runningServices.MediaStore)

	if transcriber := voice.DetectTranscriber(cfg, runningServices.bundle); transcriber != nil {
		agentLoop.SetTranscriber(transcriber)
		logger.InfoCF("voice", "Transcription enabled (agent-level)", map[string]any{"provider": transcriber.Name()})
	}

	enabledChannels := runningServices.ChannelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	// Validate preview config and apply computed defaults (FR-001..FR-005, FR-027, FR-028).
	// ValidateAndApplyPreviewDefaults mutates cfg.Gateway in-place.
	if err = cfg.Gateway.ValidateAndApplyPreviewDefaults(); err != nil {
		return nil, fmt.Errorf("gateway config: %w", err)
	}
	// Apply warmup timeout default (FR-013 / CR-04).
	cfg.Tools.ApplyWarmupTimeoutDefault()

	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	runningServices.HealthServer = health.NewServer(cfg.Gateway.Host, cfg.Gateway.Port)
	runningServices.ChannelManager.SetupHTTPServer(addr, runningServices.HealthServer)

	// Compute the main gateway origin for CORS and CSP frame-ancestors.
	// Use PublicURL when set (reverse-proxy deployment); otherwise derive from host:port.
	// When host is a wildcard (0.0.0.0, ::), allowedOrigin is empty and the WARN
	// is emitted below (FR-007e / MR-03).
	allowedOrigin := middleware.CanonicalGatewayOrigin(cfg)
	if allowedOrigin == "" {
		// Wildcard bind host and no public_url → frame-ancestors must fall back to *.
		// Log once at WARN so operators know to set gateway.public_url for strict control.
		//
		// NOTE: this WARN is emitted at boot only and is NOT re-evaluated on
		// hot-reload of gateway.public_url. Operators changing the field at
		// runtime must restart the gateway for the WARN to re-fire on the new
		// value and for the new origin to take effect in CSP headers.
		slog.Warn("frame-ancestors fallback to '*' — set gateway.public_url for strict embedding control",
			"host", cfg.Gateway.Host)
	}

	// Set up the preview listener when enabled (FR-001, FR-020).
	previewListenerEnabled := cfg.Gateway.IsPreviewListenerEnabled()
	var gatewayPreviewBaseURL string
	if previewListenerEnabled {
		previewHost := cfg.Gateway.PreviewHost
		previewPort := int(cfg.Gateway.PreviewPort)
		previewAddr := fmt.Sprintf("%s:%d", previewHost, previewPort)
		runningServices.ChannelManager.SetupPreviewServer(previewAddr)
		// Compute the preview base URL for tool result generation.
		if cfg.Gateway.PreviewOrigin != "" {
			gatewayPreviewBaseURL = cfg.Gateway.PreviewOrigin
		} else {
			scheme := "http"
			if cfg.Gateway.PublicURL != "" {
				// Inherit the scheme from PublicURL when preview_origin is not set.
				if len(cfg.Gateway.PublicURL) >= 5 && cfg.Gateway.PublicURL[:5] == "https" {
					scheme = "https"
				}
			}
			gatewayPreviewBaseURL = fmt.Sprintf("%s://%s",
				scheme,
				net.JoinHostPort(previewHost, strconv.Itoa(previewPort)),
			)
		}
	}
	// Construct the Tier 1 (serve_workspace) and Tier 3 (run_in_workspace)
	// shared registries. These are created regardless of previewListenerEnabled so
	// that operators who run on the single-port fallback still get serve_workspace.
	// run_in_workspace requires the DevServerRegistry; the tool itself gates to Linux.
	servedSubdirs := agent.NewServedSubdirs()
	devServers := sandbox.NewDevServerRegistry()
	runningServices.servedSubdirs = servedSubdirs
	runningServices.devServers = devServers

	// F-9: wire audit-set cleanup so evicted tokens don't re-emit serve.served
	// / dev.proxied on the rare cap-reset path. The callbacks are injected here
	// rather than in the registry constructors to avoid an import cycle
	// (gateway → agent/sandbox is fine; agent/sandbox → gateway would cycle).
	servedSubdirs.SetOnEvict(purgeFirstServedTokensBulk)
	devServers.SetOnEvict(purgeFirstServedTokens)

	// Start the egress proxy only when allow-list entries are configured.
	// An empty allow-list means deny-all, which is enforced by the proxy itself;
	// the proxy is still useful for audit logging even with an empty list.
	var egressProxy *sandbox.EgressProxy
	if egressProx, epErr := sandbox.NewEgressProxy(cfg.Sandbox.EgressAllowList, nil); epErr != nil {
		slog.Warn("gateway: egress proxy failed to start; run_in_workspace will run without egress enforcement",
			"error", epErr)
	} else {
		egressProxy = egressProx
		runningServices.egressProxy = egressProxy
	}

	// Build and wire Tier13Deps into every agent via the agent loop.
	tier13 := agent.Tier13Deps{
		ServedSubdirs:         servedSubdirs,
		DevServerRegistry:     devServers,
		EgressProxy:           egressProxy,
		GatewayPreviewBaseURL: gatewayPreviewBaseURL,
	}
	agentLoop.WireTier13Deps(tier13)

	// SSE chat endpoint — kept for backward compatibility; streaming tokens now route through WebSocket.
	sseHandler := newSSEHandler(msgBus, nil, allowedOrigin, func() *config.Config { return cfg })
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/chat", sseHandler)

	// WebSocket chat endpoint — primary transport for bi-directional chat streaming.
	wsHandler := newWSHandler(msgBus, agentLoop, allowedOrigin)
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/chat/ws", wsHandler)
	// Register WebSocket handler as stream fallback so streaming tokens route back for webchat.
	runningServices.ChannelManager.SetStreamFallback(wsHandler)
	// Register webchat as a channel so outbound messages (non-streaming) also route back.
	// The webchatChannel and wsHandler share a reference so streaming can suppress duplicate Send().
	wch := newWebchatChannel(wsHandler)
	wsHandler.webchatCh = wch
	runningServices.ChannelManager.RegisterChannel("webchat", wch)

	// Build the in-process tool-approval registry (FR-016, FR-070).
	// maxPending <= 0 uses the spec default (64); timeout <= 0 uses the default (300 s).
	approvalMaxPending := cfg.Gateway.ToolApprovalMaxPending
	if approvalMaxPending < 0 {
		return nil, fmt.Errorf("gateway: tool_approval_max_pending must not be negative (got %d)", approvalMaxPending)
	}
	if approvalMaxPending == 0 {
		slog.Warn("gateway: tool_approval_max_pending=0 — spec default (64) will be used")
	}
	approvalTimeout := cfg.Gateway.ToolApprovalTimeout
	var approvalTimeoutDur time.Duration
	if approvalTimeout > 0 {
		approvalTimeoutDur = time.Duration(approvalTimeout) * time.Second
	} else {
		approvalTimeoutDur = 300 * time.Second
	}
	approvalReg := newApprovalRegistryV2(approvalMaxPending, approvalTimeoutDur)
	wsHandler.approvalRegV2 = approvalReg

	// REST API endpoints for frontend data.
	onboardingMgr := onboarding.NewManager(homePath)
	tStore := agent.GetTaskStore(agentLoop)
	tExecutor := agent.GetTaskExecutor(agentLoop)
	api := &restAPI{
		agentLoop:     agentLoop,
		allowedOrigin: allowedOrigin,
		onboardingMgr: onboardingMgr,
		homePath:      homePath,
		taskStore:     tStore,
		taskExecutor:  tExecutor,
		credStore:     credStore,
		mediaStore:    runningServices.MediaStore,
		ssrfChecker:   agent.GetSSRFChecker(agentLoop), // SEC-24: nil when SSRF disabled
		sandboxResult: sandboxResult,                   // immutable post-boot snapshot
		appliedConfig: mustDeepCopyConfig(cfg),         // boot-time snapshot for pending-restart diff
		servedSubdirs: runningServices.servedSubdirs,   // serve_workspace token registry
		devServers:    runningServices.devServers,       // run_in_workspace process registry
		approvalReg:   approvalReg,                     // in-process tool-approval registry (FR-016)
	}
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/sessions", api.withAuth(api.HandleSessions))
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/sessions/", api.withAuth(api.HandleSessions))
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/agents", api.withAuth(api.HandleAgents))
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/agents/", api.withAuth(api.HandleAgents))
	runningServices.ChannelManager.RegisterHTTPHandler(
		"/api/v1/config",
		api.withAuth(withRateLimit(configLimiter, api.HandleConfig)),
	)
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/skills", api.withAuth(api.HandleSkills))
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/skills/", api.withAuth(api.HandleSkills))
	runningServices.ChannelManager.RegisterHTTPHandler("/api/v1/doctor", api.withAuth(api.HandleDoctor))

	// Register additional endpoints for frontend features.
	// These return proper JSON responses instead of letting the SPA catch-all
	// serve HTML (which causes "Unexpected token '<'" JSON parse errors).
	api.registerAdditionalEndpoints(runningServices.ChannelManager)

	// Register /serve/ and /dev/ on the preview mux (FR-005, FR-006).
	// These paths are intentionally absent from the main mux — any hit on
	// <main_host>:<port>/serve/... returns 404 from the catch-all handler.
	// The preview mux has no auth middleware: the URL path token is the credential (FR-023).
	// Handler implementations belong to Track B (rest_serve.go / rest_dev.go).
	if previewListenerEnabled {
		api.registerPreviewEndpoints(runningServices.ChannelManager)
	}

	// Catch-all for any /api/ path not registered — returns JSON 404 instead of SPA HTML.
	// Do not echo r.URL.Path in the response; that leaks internal routing details.
	runningServices.ChannelManager.RegisterHTTPHandler(
		"/api/",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
		}),
	)

	// Serve the embedded SPA (Sovereign Deep UI) as the default handler.
	// API routes registered above take priority; anything else serves the SPA.
	// If no SPA was embedded at build time, skip registration (UI not available).
	if spaHandler := newSPAHandler(); spaHandler != nil {
		runningServices.ChannelManager.RegisterHTTPHandler("/", spaHandler)
	} else {
		fmt.Println("Note: No embedded SPA (run 'pnpm build' in web/frontend to enable UI)")
	}

	// Wrap the HTTP server handler with config snapshot middleware so all
	// request handlers see a consistent config even during hot-reload.
	if err = runningServices.ChannelManager.WrapHTTPHandler(api.configSnapshotMiddleware); err != nil {
		return nil, fmt.Errorf("wrapping HTTP handler: %w", err)
	}
	// F-13: wrap the preview server with the same middleware. Without this,
	// handlers on the preview mux (HandleServeWorkspace, HandleDevProxy) call
	// configFromContext(r.Context()) which returns nil and falls back to a
	// live read of the config pointer — a torn read during hot-reload.
	// WrapPreviewHandler is a no-op when the preview listener is disabled.
	if previewListenerEnabled {
		if err = runningServices.ChannelManager.WrapPreviewHandler(api.configSnapshotMiddleware); err != nil {
			return nil, fmt.Errorf("wrapping preview handler: %w", err)
		}
	}

	// Wrap with CSRF double-submit-cookie middleware (SEC / issue #97).
	//
	// WrapHTTPHandler semantics: "wrap N times" stacks outermost-last, so the
	// execution order on a request is:
	//   CSRF check → configSnapshot injection → mux dispatch → auth check in handler
	//
	// The sprint plan (temporal-puzzling-melody.md §1) calls for
	// "auth → RBAC → CSRF → handler". We place CSRF BEFORE the per-handler
	// auth gate because (a) auth is currently inlined in withAuth / withOptionalAuth
	// wrappers rather than a separate middleware, and splitting it would be
	// substantial collateral damage for this PR; (b) failing fast on a bad
	// cookie avoids wasting a bcrypt compare on obvious cross-origin forgeries.
	// The net effect — state-changing requests without a valid cookie+header
	// get rejected — is identical.
	csrfMW := middleware.CSRFMiddleware(
		middleware.WithClientIPFunc(clientIP),
		middleware.WithReporter(func(r *http.Request, sourceIP, route string) {
			// Best-effort audit log of CSRF mismatches (SEC-15). Never blocks
			// or crashes the request path — the middleware already returns 403.
			logger := api.agentLoop.AuditLogger()
			if logger == nil {
				slog.Warn("csrf: token mismatch (no audit logger)",
					"source_ip", sourceIP, "route", route, "method", r.Method)
				return
			}
			// Named logErr to avoid shadowing the outer err declared in
			// setupServices (govet shadow). The two errors have unrelated
			// lifetimes — this one is scoped entirely to the Reporter closure.
			if logErr := logger.Log(&audit.Entry{
				Event:    "csrf_mismatch",
				Decision: audit.DecisionDeny,
				Details: map[string]any{
					"source_ip": sourceIP,
					"route":     route,
					"method":    r.Method,
				},
				PolicyRule: "csrf: cookie/header mismatch on state-changing request",
			}); logErr != nil {
				slog.Warn("csrf: audit log write failed", "error", logErr)
			}
		}),
	)
	if err = runningServices.ChannelManager.WrapHTTPHandler(csrfMW); err != nil {
		return nil, fmt.Errorf("wrapping HTTP handler with CSRF: %w", err)
	}

	if err = runningServices.ChannelManager.StartAll(context.Background()); err != nil {
		return nil, fmt.Errorf("error starting channels: %w", err)
	}

	// Boot logging (FR-020): main listener first, then preview (or disabled message).
	mainAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	slog.Info("gateway listening on " + mainAddr)
	if previewListenerEnabled {
		previewAddr := fmt.Sprintf("%s:%d", cfg.Gateway.PreviewHost, int(cfg.Gateway.PreviewPort))
		slog.Info("preview listening on " + previewAddr)
	} else {
		slog.Info("preview listener disabled by config")
	}

	fmt.Printf(
		"✓ Health endpoints available at http://%s:%d/health, /ready and /reload (POST)\n",
		cfg.Gateway.Host,
		cfg.Gateway.Port,
	)

	stateManager := state.NewManager(cfg.WorkspacePath())
	runningServices.DeviceService = devices.NewService(devices.Config{
		Enabled:    cfg.Devices.Enabled,
		MonitorUSB: cfg.Devices.MonitorUSB,
	}, stateManager)
	runningServices.DeviceService.SetBus(msgBus)
	// Invariant: when cfg.Devices.Enabled==true, a Start failure is fatal and
	// propagated to the caller (Run returns the error). When disabled, Start
	// failures are only warnings. A unit test for this path is not included
	// because devices.Service is a concrete struct (not an interface) and
	// mocking it would require invasive refactoring; the behavior is exercised
	// by integration tests that configure a real USB monitor on supported hosts.
	if err = runningServices.DeviceService.Start(context.Background()); err != nil {
		if cfg.Devices.Enabled {
			return nil, fmt.Errorf("device service: %w", err)
		}
		logger.WarnCF(
			"device",
			"device service start failed (devices disabled, continuing)",
			map[string]any{"error": err.Error()},
		)
	} else if cfg.Devices.Enabled {
		fmt.Println("✓ Device event service started")
	}

	return runningServices, nil
}

func stopAndCleanupServices(runningServices *services, shutdownTimeout time.Duration, isReload bool) {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// reload should not stop channel manager
	if !isReload && runningServices.ChannelManager != nil {
		runningServices.ChannelManager.StopAll(shutdownCtx)
	}
	if runningServices.DeviceService != nil {
		runningServices.DeviceService.Stop()
	}
	if runningServices.HeartbeatService != nil {
		runningServices.HeartbeatService.Stop()
	}
	if runningServices.CronService != nil {
		runningServices.CronService.Stop()
	}
	if runningServices.MediaStore != nil {
		if fms, ok := runningServices.MediaStore.(*media.FileMediaStore); ok {
			fms.Stop()
		}
	}
}

func handleConfigReload(
	ctx context.Context,
	al *agent.AgentLoop,
	newCfg *config.Config,
	providerRef *providers.LLMProvider,
	runningServices *services,
	msgBus *bus.MessageBus,
	allowEmptyStartup bool,
) error {
	logger.Info("🔄 Config file changed, reloading...")

	newModel := newCfg.Agents.Defaults.ModelName

	logger.Infof(" New model is '%s', recreating provider...", newModel)

	logger.Info("  Stopping all services...")
	stopAndCleanupServices(runningServices, serviceShutdownTimeout, true)

	newProvider, newModelID, err := createStartupProvider(newCfg, allowEmptyStartup)
	if err != nil {
		logger.Errorf("  ⚠ Error creating new provider: %v", err)
		logger.Warn("  Attempting to restart services with old provider and config...")
		if restartErr := restartServices(al, runningServices, msgBus); restartErr != nil {
			logger.Errorf("  ⚠ Failed to restart services: %v", restartErr)
		}
		return fmt.Errorf("error creating new provider: %w", err)
	}

	if newModelID != "" && newCfg.Agents.Defaults.ModelName == "" {
		newCfg.Agents.Defaults.ModelName = newModelID
	}

	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), providerReloadTimeout)
	defer reloadCancel()

	if err := al.ReloadProviderAndConfig(reloadCtx, newProvider, newCfg); err != nil {
		logger.Errorf("  ⚠ Error reloading agent loop: %v", err)
		if cp, ok := newProvider.(providers.StatefulProvider); ok {
			cp.Close()
		}
		logger.Warn("  Attempting to restart services with old provider and config...")
		if restartErr := restartServices(al, runningServices, msgBus); restartErr != nil {
			logger.Errorf("  ⚠ Failed to restart services: %v", restartErr)
		}
		return fmt.Errorf("error reloading agent loop: %w", err)
	}

	*providerRef = newProvider

	logger.Info("  Restarting all services with new configuration...")
	if err := restartServices(al, runningServices, msgBus); err != nil {
		logger.Errorf("  ⚠ Error restarting services: %v", err)
		return fmt.Errorf("error restarting services: %w", err)
	}

	logger.Info("  ✓ Provider, configuration, and services reloaded successfully (thread-safe)")
	return nil
}

func restartServices(
	al *agent.AgentLoop,
	runningServices *services,
	msgBus *bus.MessageBus,
) error {
	cfg := al.GetConfig()

	execTimeout := time.Duration(cfg.Tools.Cron.ExecTimeoutMinutes) * time.Minute
	var err error
	runningServices.CronService, err = setupCronTool(
		al,
		msgBus,
		cfg.WorkspacePath(),
		cfg.Agents.Defaults.RestrictToWorkspace,
		execTimeout,
		cfg,
	)
	if err != nil {
		return fmt.Errorf("error restarting cron service: %w", err)
	}
	if err = runningServices.CronService.Start(); err != nil {
		return fmt.Errorf("error restarting cron service: %w", err)
	}
	fmt.Println("  ✓ Cron service restarted")

	runningServices.HeartbeatService = heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	runningServices.HeartbeatService.SetBus(msgBus)
	runningServices.HeartbeatService.SetHandler(createHeartbeatHandler(al))
	if te := agent.GetTaskExecutor(al); te != nil {
		runningServices.HeartbeatService.SetTaskChecker(te)
	}
	if err = runningServices.HeartbeatService.Start(); err != nil {
		return fmt.Errorf("error restarting heartbeat service: %w", err)
	}
	fmt.Println("  ✓ Heartbeat service restarted")

	runningServices.MediaStore = media.NewFileMediaStoreWithCleanup(media.MediaCleanerConfig{
		Enabled:  cfg.Tools.MediaCleanup.Enabled,
		MaxAge:   time.Duration(cfg.Tools.MediaCleanup.MaxAge) * time.Minute,
		Interval: time.Duration(cfg.Tools.MediaCleanup.Interval) * time.Minute,
	})
	if fms, ok := runningServices.MediaStore.(*media.FileMediaStore); ok {
		fms.Start()
	}
	al.SetMediaStore(runningServices.MediaStore)

	al.SetChannelManager(runningServices.ChannelManager)

	if err = runningServices.ChannelManager.Reload(context.Background(), cfg, runningServices.bundle); err != nil {
		return fmt.Errorf("error reload channels: %w", err)
	}
	fmt.Println("  ✓ Channels restarted.")

	enabledChannels := runningServices.ChannelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("  ✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("  ⚠ Warning: No channels enabled")
	}

	// Stop the previous DeviceService before replacing it to avoid goroutine
	// leaks: the old service's goroutine would keep running with a dangling
	// pointer if we only overwrite the field.
	if oldDS := runningServices.DeviceService; oldDS != nil {
		oldDS.Stop()
	}
	stateManager := state.NewManager(cfg.WorkspacePath())
	runningServices.DeviceService = devices.NewService(devices.Config{
		Enabled:    cfg.Devices.Enabled,
		MonitorUSB: cfg.Devices.MonitorUSB,
	}, stateManager)
	runningServices.DeviceService.SetBus(msgBus)
	if err := runningServices.DeviceService.Start(context.Background()); err != nil {
		if cfg.Devices.Enabled {
			return fmt.Errorf("device service: %w", err)
		}
		logger.WarnCF(
			"device",
			"device service start failed (devices disabled, continuing)",
			map[string]any{"error": err.Error()},
		)
	} else if cfg.Devices.Enabled {
		fmt.Println("  ✓ Device event service restarted")
	}

	transcriber := voice.DetectTranscriber(cfg, runningServices.bundle)
	al.SetTranscriber(transcriber)
	if transcriber != nil {
		logger.InfoCF("voice", "Transcription re-enabled (agent-level)", map[string]any{"provider": transcriber.Name()})
	} else {
		logger.InfoCF("voice", "Transcription disabled", nil)
	}

	return nil
}

func setupConfigWatcherPolling(
	configPath string,
	debug bool,
	credStore *credentials.Store,
) (chan *config.Config, func()) {
	configChan := make(chan *config.Config, 1)
	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		lastModTime := getFileModTime(configPath)
		lastSize := getFileSize(configPath)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				currentModTime := getFileModTime(configPath)
				currentSize := getFileSize(configPath)

				if currentModTime.After(lastModTime) || currentSize != lastSize {
					if debug {
						logger.Debugf("🔍 Config file change detected")
					}

					time.Sleep(500 * time.Millisecond)

					lastModTime = currentModTime
					lastSize = currentSize

					newCfg, err := config.LoadConfigWithStore(configPath, credStore)
					if err != nil {
						logger.Errorf("⚠ Error loading new config: %v", err)
						logger.Warn("  Using previous valid config")
						continue
					}

					if err := newCfg.ValidateProviders(); err != nil {
						logger.Errorf("  ⚠ New config validation failed: %v", err)
						logger.Warn("  Using previous valid config")
						continue
					}

					logger.Info("✓ Config file validated and loaded")

					select {
					case configChan <- newCfg:
					default:
						logger.Warn("⚠ Previous config reload still in progress, skipping")
					}
				}
			case <-stop:
				return
			}
		}
	}()

	stopFunc := func() {
		close(stop)
		wg.Wait()
	}

	return configChan, stopFunc
}

func getFileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		slog.Debug("gateway: could not stat file for mod time", "path", path, "error", err)
		return time.Time{}
	}
	return info.ModTime()
}

func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		slog.Debug("gateway: could not stat file for size", "path", path, "error", err)
		return 0
	}
	return info.Size()
}

func setupCronTool(
	agentLoop *agent.AgentLoop,
	msgBus *bus.MessageBus,
	workspace string,
	restrict bool,
	execTimeout time.Duration,
	cfg *config.Config,
) (*cron.CronService, error) {
	cronStorePath := filepath.Join(workspace, "cron", "jobs.json")

	cronService := cron.NewCronService(cronStorePath, nil)

	// Cron tool — always registered. Policy controls whether an agent can invoke it.
	cronTool, err := tools.NewCronTool(cronService, agentLoop, msgBus, workspace, restrict, execTimeout, cfg)
	if err != nil {
		return nil, fmt.Errorf("critical error during CronTool initialization: %w", err)
	}
	agentLoop.RegisterTool(cronTool)

	if cronTool != nil {
		cronService.SetOnJob(func(job *cron.CronJob) (string, error) {
			result := cronTool.ExecuteJob(context.Background(), job)
			return result, nil
		})
	}

	return cronService, nil
}

func createHeartbeatHandler(agentLoop *agent.AgentLoop) func(prompt, channel, chatID string) *tools.ToolResult {
	return func(prompt, channel, chatID string) *tools.ToolResult {
		if channel == "" || chatID == "" {
			channel, chatID = "cli", "direct"
		}

		response, err := agentLoop.ProcessHeartbeat(context.Background(), prompt, channel, chatID)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("Heartbeat error: %v", err))
		}
		if response == "HEARTBEAT_OK" {
			return tools.SilentResult("Heartbeat OK")
		}
		return tools.SilentResult(response)
	}
}
