// Package tools — Tier 3 run_in_workspace tool.
//
// This tool starts a long-lived dev server (Next.js, Vite, Astro, SvelteKit, or
// any operator-extended command) inside the agent's workspace directory and
// registers it with the gateway's DevServerRegistry. The registration produces
// a path token used by the /dev/<agent>/<token>/ reverse-proxy route, which
// the tool returns to the agent as a fully-qualified URL.
//
// Linux only in v4. On macOS/Windows the tool returns IsError=true with the
// spec-mandated unsupported message; the gateway's /dev/<agent>/ handler
// returns 503 with the same wording so tool-layer and HTTP-layer errors are
// consistent.
//
// On Linux the tool:
//   - Validates the command binary+subcommand against the baseline allow-list
//     (next dev | vite dev | astro dev | sveltekit dev) unioned with the
//     operator-extended cfg.Sandbox.Tier3Commands set.
//   - Validates expose_port falls inside cfg.Sandbox.DevServerPortRange
//     (default [18000, 18999]).
//   - Enforces a per-agent cap (1 active server) and a per-gateway cap
//     (cfg.Sandbox.MaxConcurrentDevServers, default 2). Cap-hit errors include
//     the EarliestExpiry timestamp from DevServerRegistry so the caller knows
//     when a slot becomes free.
//   - Emits an audit entry before spawning the child (AuditFailClosed=true
//     by default — the audit trail is the only post-hoc record of what ran).
//   - Spawns the child under hardened_exec (Setpgid + Pdeathsig + prlimit +
//     egress-proxy env injection) and registers the process.
//
// Threat acknowledgement: dev servers run with the gateway's full filesystem
// reach and unblocked raw TCP egress. run_in_workspace is a
// TRUSTED-PROMPT FEATURE — invoke only when the user input that drives the
// command has been verified trustworthy.

package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/audit"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// Tier3UnsupportedMessage is the literal IsError content returned on
// non-Linux platforms. It MUST match the gateway's /dev/ 503 body so
// users see the same wording at the tool layer and the HTTP layer.
//
// Spec v4 US-6 acceptance scenario 5 /.
const Tier3UnsupportedMessage = "Tier 3 dev servers are Linux only"

// baselineTier3Commands enumerates the leading-binary+subcommand pairs
// allowed by default. Operators may extend with cfg.Sandbox.Tier3Commands.
var baselineTier3Commands = []string{
	"next dev",
	"vite dev",
	"astro dev",
	"sveltekit dev",
}

// devServerStartupGrace is how long we wait between spawning the child
// and registering it. Long enough for the child to bind its port (the
// /dev/<agent>/<token>/ reverse proxy will otherwise see ECONNREFUSED on
// the first request). 3 s is enough for typical dev servers; the proxy
// itself retries connect for a short window.
const devServerStartupGrace = 3 * time.Second

// RunInWorkspaceConfig is the runtime config snapshot needed by the tool.
type RunInWorkspaceConfig struct {
	// Tier3Commands extends baselineTier3Commands. Each entry is a full
	// "binary subcommand" string (e.g. "remix dev"). Subcommand match is
	// byte-exact and must be either the entire command string or end at a
	// whitespace boundary — e.g. allowing "npm run" admits "npm run build"
	// but rejects "npm runX". Comparison is case-sensitive on the binary.
	Tier3Commands []string

	// PortRange is [min, max] inclusive. Spec default [18000, 18999].
	PortRange [2]int32

	// MaxConcurrent is the per-gateway active-server cap.
	MaxConcurrent int32

	// EgressAllowList is the operator's allow-list (audit hint only —
	// the proxy enforces the live list).
	EgressAllowList []string

	// AuditFailClosed (HIGH-6, silent-failure-hunter) controls behaviour
	// when audit-write fails on a Tier 3 invocation. When true (default
	// via ResolveBool(cfg.Sandbox.PathGuardAuditFailClosed, true)) the
	// tool refuses to start the dev server without a guaranteed compliance
	// trail — run_in_workspace is a TRUSTED-PROMPT FEATURE and the only
	// after-the-fact record of what was run is the audit log. When false,
	// the audit failure is logged at Error and the dev server proceeds
	// (operator opt-out).
	AuditFailClosed bool
}

// RunInWorkspaceTool is the agent-callable Tier 3 tool. One instance per
// gateway; the registry pointer is shared so /dev/<agent>/<token>/ can
// look up registrations.
type RunInWorkspaceTool struct {
	BaseTool
	cfg         RunInWorkspaceConfig
	workspace   string
	registry    *sandbox.DevServerRegistry
	proxy       *sandbox.EgressProxy
	auditLogger *audit.Logger

	// gatewayHost is the operator-configured PREVIEW listener host
	// (host[:port] or full URL); used to construct the absolute URL
	// returned to the agent in the tool result. Empty means we omit the
	// scheme/host and return only the path; the SPA reconstructs against
	// its current preview_origin at render time.
	//
	// In the two-port topology this MUST be the preview origin — NOT the
	// main gateway origin — so the iframe loads from a different browser
	// origin than the SPA (Threat Model T-01: served JS cannot read the
	// SPA's localStorage). Wired in pkg/agent/instance.go from
	// Tier13Deps.GatewayPreviewBaseURL.
	//
	// Scheme coercion: if gatewayHost does not contain "://" it is treated
	// as a bare host[:port] and "https://" is prepended automatically (see
	// buildDevURL). Operators running a plain-HTTP preview listener must
	// supply the full URL form (e.g. "http://192.168.1.10:5001") so that
	// the https:// coercion does not produce a mixed-content URL in the
	// tool result.
	gatewayHost string

	// runMu guards the slow validate-then-register path so two
	// concurrent invocations on the same agent don't both pass the
	// per-agent check before either registers. registry.Register is
	// also locked, but we want a single error path here.
	runMu sync.Mutex
}

// NewRunInWorkspaceTool constructs the tool. registry, proxy, and
// auditLogger may be nil in tests but production wiring passes all three.
func NewRunInWorkspaceTool(
	cfg RunInWorkspaceConfig,
	workspace string,
	registry *sandbox.DevServerRegistry,
	proxy *sandbox.EgressProxy,
	auditLogger *audit.Logger,
	gatewayHost string,
) *RunInWorkspaceTool {
	if cfg.PortRange == [2]int32{} {
		cfg.PortRange = [2]int32{18000, 18999}
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	return &RunInWorkspaceTool{
		cfg:         cfg,
		workspace:   workspace,
		registry:    registry,
		proxy:       proxy,
		auditLogger: auditLogger,
		gatewayHost: gatewayHost,
	}
}

// SetAuditLogger satisfies auditLoggerAware.
func (t *RunInWorkspaceTool) SetAuditLogger(logger *audit.Logger) {
	t.auditLogger = logger
}

func (t *RunInWorkspaceTool) Name() string { return "run_in_workspace" }

func (t *RunInWorkspaceTool) Description() string {
	return "Run a dev server (next/vite/astro/sveltekit dev) in the agent's workspace. Tier 3 — Linux only. TRUSTED-PROMPT FEATURE: dev servers run with the gateway's full filesystem access including $OMNIPUS_HOME/credentials.json. After running untrusted dev servers, rotate the master key. Returns a /dev/<agent>/<token>/ URL for the user's browser."
}

func (t *RunInWorkspaceTool) Scope() ToolScope { return ScopeCore }

func (t *RunInWorkspaceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Dev server command. Must start with one of: 'next dev', 'vite dev', 'astro dev', 'sveltekit dev', or an operator-configured Tier 3 command.",
			},
			"env": map[string]any{
				"type":                 "object",
				"description":          "Optional extra environment variables for the dev server child.",
				"additionalProperties": map[string]any{"type": "string"},
			},
			"expose_port": map[string]any{
				"type":        "integer",
				"description": "TCP port the dev server will bind to. Must lie within the operator-configured DevServerPortRange (default [18000, 18999]).",
			},
		},
		"required": []string{"command", "expose_port"},
	}
}

// Execute validates and spawns the dev server.
func (t *RunInWorkspaceTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	// Linux gate. Tier 3 is Linux-only in v4.
	if runtime.GOOS != "linux" {
		return ErrorResult(Tier3UnsupportedMessage)
	}

	if t.registry == nil {
		// Defence-in-depth: if the gateway forgot to wire the registry,
		// fail closed rather than spawning an unregistered child.
		return ErrorResult("run_in_workspace: dev-server registry not configured")
	}

	// Args.
	command, _ := args["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return ErrorResult("command is required")
	}
	exposePortRaw, ok := args["expose_port"]
	if !ok {
		return ErrorResult("expose_port is required")
	}
	exposePort, err := normalisePort(exposePortRaw)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid expose_port: %v", err))
	}

	// Port range check. Inclusive on both ends.
	if exposePort < t.cfg.PortRange[0] || exposePort > t.cfg.PortRange[1] {
		return ErrorResult(fmt.Sprintf(
			"port out of allowed range [%d, %d]",
			t.cfg.PortRange[0], t.cfg.PortRange[1],
		))
	}

	// Command allow-list.
	if !t.commandAllowed(command) {
		return ErrorResult(fmt.Sprintf(
			"command %q is not in the Tier 3 baseline (next dev | vite dev | astro dev | sveltekit dev) or operator-extended Tier3Commands",
			command,
		))
	}

	// Per-agent cap pre-check. (Registry also enforces this atomically
	// inside Register; we check here for a cleaner error path before
	// doing real work.) The lock is scoped tight: it covers only the
	// pre-check, NOT the subsequent spawn + 3 s startup grace. Holding
	// runMu across the grace would serialize all agent invocations
	// behind the slowest one.
	agentID := ToolAgentID(ctx)
	if agentID == "" {
		return ErrorResult("run_in_workspace: missing agent id in context")
	}
	t.runMu.Lock()
	existing := t.registry.LookupByAgent(agentID)
	t.runMu.Unlock()
	if existing != nil {
		return ErrorResult(fmt.Sprintf(
			"server already running on this agent; previous registration expires at %s",
			existing.CreatedAt.Add(sandbox.HardTimeout).UTC().Format(time.RFC3339),
		))
	}

	// Build envSlice from optional env arg, prefixed with PORT=<exposePort>
	// so dev servers that honour PORT (next, vite, astro, sveltekit all do)
	// bind to the port the agent requested.
	envSlice := []string{
		fmt.Sprintf("PORT=%d", exposePort),
		// HOST=127.0.0.1 forces dev servers that default to 0.0.0.0
		// to bind loopback only. The reverse proxy is the only path
		// for external access.
		"HOST=127.0.0.1",
	}
	if rawEnv, ok := args["env"].(map[string]any); ok {
		for k, v := range rawEnv {
			if vs, ok := v.(string); ok {
				envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, vs))
			}
		}
	}

	// HIGH-6 (silent-failure-hunter): emit the audit BEFORE spawning the
	// child. run_in_workspace is a TRUSTED-PROMPT FEATURE; if the audit
	// pipeline is degraded AND AuditFailClosed=true, we refuse to start the
	// dev server — the compliance trail is the only after-the-fact record.
	// Emitting before start keeps the fail-closed branch from leaking a
	// running orphan child when the audit fails.
	if auditErr := t.auditStart(ctx, agentID, command, exposePort); auditErr != nil {
		return auditErr
	}

	// Spawn the child. We use exec.CommandContext directly here rather
	// than sandbox.Run because run_in_workspace returns the URL
	// immediately and lets the child run in the background — Run
	// blocks until the child exits, which is the wrong contract for a
	// long-lived dev server.
	cmd, err := t.startBackgroundChild(ctx, command, envSlice, exposePort)
	if err != nil {
		return ErrorResult(fmt.Sprintf("run_in_workspace: failed to start child: %v", err))
	}

	// Register. If Register fails (gateway-cap hit), we must SIGTERM
	// the orphaned child or it lingers without an entry.
	reg, regErr := t.registry.Register(agentID, exposePort, cmd.Process.Pid, command, int(t.cfg.MaxConcurrent))
	if regErr != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		// HIGH-3 (silent-failure-hunter): capture the orphaned-child's exit
		// outcome so operators can correlate registration failures with
		// child behaviour (e.g. SIGTERM-ignored builds, segfault on startup).
		// Without this log a SIGTERM-ignored child becomes invisible.
		orphanPid := cmd.Process.Pid
		go func() {
			waitErr := cmd.Wait()
			exitCode := -1
			if waitErr == nil {
				exitCode = 0
			} else if ee, ok := waitErr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			}
			slog.Info("run_in_workspace: orphaned child exited (registration failed)",
				"agent_id", agentID, "pid", orphanPid, "exit_code", exitCode, "error", waitErr)
		}()
		// Translate the typed errors into the spec wording.
		var capErr sandbox.ErrGatewayCap
		if errors.As(regErr, &capErr) {
			// Match the wording in.
			return ErrorResult(fmt.Sprintf(
				"too many concurrent dev servers (%d/%d); previous registration expires at %s",
				capErr.Current, capErr.Max, capErr.EarliestExpiry.UTC().Format(time.RFC3339),
			))
		}
		if errors.Is(regErr, sandbox.ErrPerAgentCap) {
			return ErrorResult("server already running on this agent")
		}
		return ErrorResult(fmt.Sprintf("run_in_workspace: registration failed: %v", regErr))
	}

	// Reap the child in a goroutine so its zombie clears when it exits
	// naturally; the registry's Unregister will SIGTERM if it expires
	// first.
	//
	// HIGH-3 (silent-failure-hunter): capture and log the exit outcome so
	// crash-loop dev servers and unexpected exits are visible to operators.
	// The Unregister call still runs unconditionally — it is idempotent —
	// after we have logged the exit details.
	bgPid := cmd.Process.Pid
	go func() {
		waitErr := cmd.Wait()
		exitCode := -1
		if waitErr == nil {
			exitCode = 0
		} else if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		slog.Info("run_in_workspace: dev server exited",
			"agent_id", agentID, "pid", bgPid, "token", reg.Token,
			"exit_code", exitCode, "error", waitErr)
		// Defensive cleanup if the child exits on its own (crash,
		// natural completion). Idempotent.
		t.registry.Unregister(reg.Token)
	}()

	// Brief grace period so the dev server has time to bind before the
	// agent (or user) hits the URL. Skip if context is cancelled — the
	// caller has lost interest anyway.
	select {
	case <-ctx.Done():
	case <-time.After(devServerStartupGrace):
	}

	// Audit emission already happened pre-start (HIGH-6). No second
	// emission here — the start log captures what was authorised; the
	// reaper goroutine logs the exit.

	// FR-008a / CR-03: emit a structured tool result carrying BOTH a JSON
	// payload (consumed by the SPA's RunInWorkspaceUI to mount the iframe)
	// AND a human-readable English sentence (the LLM's view of what
	// happened). The ToolResult primitive does not have separate "result"
	// and "summary" fields, so we embed the summary as a "_summary" key
	// inside the JSON. The LLM sees the entire JSON including _summary in
	// its message history; the SPA parses the JSON and ignores fields it
	// does not recognise.
	//
	// Path is relative — the SPA reconstructs the absolute URL against its
	// current preview_origin at render time. URL is the absolute string
	// used today, preserved for replay safety on transcripts.
	//
	// expires_at carries the HardTimeout deadline only (reg.CreatedAt +
	// sandbox.HardTimeout). The idle-timeout path — where the registry
	// evicts a session that has had no requests for 30 min — is NOT
	// represented in this field. Operators or tools monitoring expires_at
	// should be aware that a session may be terminated before this
	// timestamp if the dev server goes idle.
	path := fmt.Sprintf("/dev/%s/%s/", agentID, reg.Token)
	url := t.buildDevURL(agentID, reg.Token)
	deadline := reg.CreatedAt.Add(sandbox.HardTimeout).UTC().Format(time.RFC3339)
	summary := fmt.Sprintf(
		"run_in_workspace: dev server started. URL: %s. Command: %s. Port: %d. Token expires after 30 min idle / 4 h hard cap.",
		url, command, exposePort,
	)
	jsonBody := fmt.Sprintf(
		`{"path":%q,"url":%q,"expires_at":%q,"command":%q,"port":%d,"_summary":%q}`,
		path, url, deadline, command, exposePort, summary,
	)
	return NewToolResult(jsonBody)
}

// startBackgroundChild spawns the dev-server child using the same
// hardened SysProcAttr as sandbox.Run — Setpgid + Pdeathsig — but does
// not wait for completion. The parent gets back a started *exec.Cmd; the
// caller MUST eventually Wait on it (we do this in a goroutine above).
func (t *RunInWorkspaceTool) startBackgroundChild(
	ctx context.Context,
	command string,
	env []string,
	port int32,
) (*exec.Cmd, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	limits := sandbox.Limits{
		// No timeout — dev servers run until expiry.
		TimeoutSeconds:   0,
		MemoryLimitBytes: 0, // dev servers manage their own memory
		WorkspaceDir:     t.workspace,
		EgressProxyAddr:  t.proxyAddr(),
	}

	// Build cmd.Env from the supplied env plus proxy + npm-cache vars.
	// We re-use sandbox.mergeEnv via a small shim so the same merge
	// semantics apply as for build_static. We can't call mergeEnv
	// directly (unexported); emulate it.
	merged := append([]string{}, env...)
	if t.proxy != nil {
		proxyURL := "http://" + t.proxy.Addr()
		merged = append(merged,
			"HTTP_PROXY="+proxyURL,
			"HTTPS_PROXY="+proxyURL,
			"http_proxy="+proxyURL,
			"https_proxy="+proxyURL,
			"NO_PROXY=127.0.0.1,localhost,::1",
			"no_proxy=127.0.0.1,localhost,::1",
		)
	}
	if t.workspace != "" {
		merged = append(merged, "npm_config_cache="+t.workspace+"/.npm-cache")
	}
	// Force PORT to win over operator-supplied env.
	merged = append(merged, fmt.Sprintf("PORT=%d", port))

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = t.workspace
	cmd.Env = merged

	// Apply Setpgid + Pdeathsig via the same path sandbox.Run uses.
	if err := sandbox.ApplyChildHardening(cmd, limits); err != nil {
		return nil, fmt.Errorf("apply hardening: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if err := sandbox.ApplyChildPostStartHardening(cmd, limits); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("post-start hardening: %w", err)
	}
	return cmd, nil
}

// commandAllowed returns true when command's leading binary+subcommand
// matches an entry in the baseline OR cfg.Tier3Commands.
//
// Comparison is exact-prefix on the trimmed command. e.g. baseline entry
// "next dev" matches the command "next dev --port 18000" but NOT
// "next-mock dev".
func (t *RunInWorkspaceTool) commandAllowed(command string) bool {
	allow := append([]string{}, baselineTier3Commands...)
	allow = append(allow, t.cfg.Tier3Commands...)
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if command == entry || strings.HasPrefix(command, entry+" ") {
			return true
		}
	}
	return false
}

// proxyAddr returns the egress proxy addr, or "" when proxy is nil.
func (t *RunInWorkspaceTool) proxyAddr() string {
	if t.proxy == nil {
		return ""
	}
	return t.proxy.Addr()
}

// buildDevURL returns the /dev/<agent>/<token>/ URL the agent surfaces to
// the user. When gatewayHost is empty (test wiring) we return only the
// path so callers can prepend their own host.
func (t *RunInWorkspaceTool) buildDevURL(agentID, token string) string {
	path := fmt.Sprintf("/dev/%s/%s/", agentID, token)
	if t.gatewayHost == "" {
		return path
	}
	host := strings.TrimSuffix(t.gatewayHost, "/")
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	return host + path
}

// auditStart emits an audit entry for the dev-server start so operators
// can correlate /dev/<agent>/<token>/ traffic with the agent that
// requested it.
//
// HIGH-6 (silent-failure-hunter): run_in_workspace is a TRUSTED-PROMPT
// FEATURE. The audit log is the ONLY after-the-fact
// compliance trail of what dev-server command ran with the gateway's full
// filesystem reach. When the audit write fails AND AuditFailClosed is true
// (default), we REFUSE to start the dev server — the safety contract
// requires a guaranteed trail. When AuditFailClosed is false the operator
// has explicitly opted out and we continue with an Error log.
//
// Returns a non-nil *ToolResult ONLY when the dev server must be aborted.
// Returns nil to mean "continue".
func (t *RunInWorkspaceTool) auditStart(ctx context.Context, agentID, command string, port int32) *ToolResult {
	if t.auditLogger == nil {
		// No logger wired (test path). Continue without auditing — if
		// production wiring forgets the logger that is a separate bug
		// caught by integration tests. We do NOT fail closed here because
		// production wiring is responsible for providing the logger.
		return nil
	}
	logErr := t.auditLogger.Log(&audit.Entry{
		Event:    audit.EventExec,
		Decision: audit.DecisionAllow,
		AgentID:  agentID,
		Tool:     t.Name(),
		Command:  command,
		Details: map[string]any{
			"expose_port":      port,
			"egress_allowlist": t.cfg.EgressAllowList,
			"workspace":        t.workspace,
		},
	})
	if logErr == nil {
		return nil
	}
	if t.cfg.AuditFailClosed {
		slog.Error("run_in_workspace: audit logger degraded; refusing to run trusted-prompt feature",
			"agent_id", agentID, "command", command, "error", logErr, "audit_fail_closed", true)
		return &ToolResult{
			IsError: true,
			ForLLM:  "audit logger degraded; refusing to run trusted-prompt feature without compliance trail",
			ForUser: "Tier 3 requires audit logging; aborting",
		}
	}
	slog.Error("run_in_workspace: audit write failed (continuing — audit_fail_closed=false)",
		"agent_id", agentID, "command", command, "error", logErr)
	return nil
}

// normalisePort accepts both float64 (the JSON unmarshalling default for
// numeric values) and int. Returns int32 on success, error on negative
// or out-of-int32-range.
func normalisePort(v any) (int32, error) {
	switch n := v.(type) {
	case float64:
		if n < 0 || n > 65535 {
			return 0, fmt.Errorf("port %v out of TCP range", n)
		}
		return int32(n), nil
	case int:
		if n < 0 || n > 65535 {
			return 0, fmt.Errorf("port %d out of TCP range", n)
		}
		return int32(n), nil
	case int32:
		return n, nil
	case int64:
		if n < 0 || n > 65535 {
			return 0, fmt.Errorf("port %d out of TCP range", n)
		}
		return int32(n), nil
	}
	return 0, fmt.Errorf("port must be a number, got %T", v)
}
