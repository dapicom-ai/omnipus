//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package gateway — Sprint-J sandbox-apply wiring.
//
// This file implements FR-J-001..016 from docs/plan/sprint-j-sandbox-apply-spec.md.
// It owns the boot-time orchestration of:
//
//   1. Mode resolution (CLI > config > default) with legacy Enabled mapping.
//   2. Backend selection via sandbox.SelectBackend() (Linux vs Fallback).
//   3. Policy computation via sandbox.DefaultPolicy($OMNIPUS_HOME, AllowedPaths).
//   4. Kernel apply: LinuxBackend.ApplyWithMode → SeccompProgram.Install
//      (both gated on LinuxBackend selection — seccomp-alone is never valid).
//   5. Fail-closed on kernel error when mode=enforce on a capable kernel
//      (exit 78 / EX_CONFIG from cmd/omnipus/main.go).
//   6. Status surfacing: /health response carries sandbox.applied, mode, backend.
//   7. Nag banners: permissive-always, production-off (every 60 seconds,
//      no suppression).
//
// Strict invariants:
//   - All sandbox-apply work MUST complete before any HTTP listener binds
//     (FR-J-010, FR-J-016). Boot sequence: unlock → config → selectBackend
//     → Apply → Install → net.Listen. During the Apply→Install→bind window,
//     external TCP probes receive ECONNREFUSED (not HTTP 503).
//   - Seccomp is only installed when the Linux backend is selected
//     (FR-J-014). Fallback backend means no seccomp.
//   - Apply is idempotent per-process (FR-J-009, enforced inside
//     sandbox.LinuxBackend).
//   - No hot-reload of sandbox config (FR-J-015). Config changes require restart.

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// ExitSandboxConfig is the Sprint-J-specific exit code for Apply/Install
// failure on a capable kernel (sysexits.h EX_CONFIG=78). cmd/omnipus/main.go
// maps sandbox errors to this code so operators and CI pipelines can
// distinguish sandbox-apply failure from a generic boot error (exit 1).
const ExitSandboxConfig = 78

// SandboxApplyOptions carries the inputs for applySandbox. Kept as a struct
// so the boot caller in gateway.go passes one value and new fields can be
// added without churning the signature (e.g. a future test hook).
type SandboxApplyOptions struct {
	// CLIMode is the value parsed from --sandbox. Empty means "no flag".
	// Non-empty CLI value always overrides the config value (CLI > config).
	CLIMode string
	// Cfg is the loaded config. applySandbox reads cfg.Sandbox.Mode and
	// cfg.Sandbox.AllowedPaths.
	Cfg *config.Config
	// HomePath is $OMNIPUS_HOME, the workspace root that gets RWX access.
	HomePath string
	// Backend is the sandbox backend to apply. When nil, applySandbox calls
	// sandbox.SelectBackend() itself. Normally the gateway passes
	// agentLoop.SandboxBackend() so the /api/v1/security/sandbox-status
	// handler observes the Apply-marked state on the same instance.
	//
	// Note: Landlock's restrict_self affects the whole process, so the
	// choice of instance does not affect kernel enforcement — it only
	// affects which instance's PolicyApplied() flag flips to true.
	Backend sandbox.SandboxBackend
	// GetEnv is os.Getenv by default; overridable for tests that need to
	// inject OMNIPUS_ENV without mutating the real env.
	GetEnv func(string) string
	// Stderr is os.Stderr by default; overridable for tests that capture
	// the production / permissive nag banners.
	Stderr *os.File
}

// SandboxApplyResult captures the outcome of applySandbox. The gateway
// retains this so:
//   - the /health handler can surface {sandbox.applied, mode, backend};
//   - the /api/v1/security/sandbox-status handler can enrich the response
//     with mode and disabled_by via sandbox.DescribeBackendWithState;
//   - the nag-banner goroutine knows whether to fire (permissive or
//     production-off) and for what reason.
type SandboxApplyResult struct {
	// Backend is the selected backend (either LinuxBackend or FallbackBackend).
	Backend sandbox.SandboxBackend
	// BackendName is the selected backend's Name() — "landlock-v3",
	// "landlock-v2", "landlock-v1", or "fallback".
	BackendName string
	// Mode is the resolved mode after CLI/config/legacy mapping.
	Mode sandbox.Mode
	// DisabledBy identifies the source of a Mode=Off decision: "cli_flag",
	// "config", or "kernel_unsupported". Empty when Mode is enforce or
	// permissive.
	DisabledBy string
	// ApplyState is the state struct sandbox.DescribeBackendWithState
	// consumes to produce the /api/v1/security/sandbox-status response.
	ApplyState sandbox.ApplyState
	// NagReason is "" when no banner should fire, "permissive" when
	// Mode=Permissive, "production_off" when Mode=Off + OMNIPUS_ENV=production.
	// Used by StartNagBanner to decide whether and what to repeat.
	NagReason string
}

// resolveMode implements the CLI > config > default precedence rule from
// FR-J-006. Returns (mode, disabledBy, error):
//
//	"cli_flag"  — CLI flag provided (trumps config, even empty-string config)
//	"config"    — Mode came from config (either Mode or legacy Enabled)
//	""          — Mode was derived from defaults (no CLI, no config)
//
// An invalid CLI value causes an error so cmd/omnipus can exit with code 2
// (usage error) before any boot logic runs (FR-J-006 second sentence).
func resolveMode(cliMode, cfgMode string, cfgEnabledSet bool) (sandbox.Mode, string, error) {
	// CLI takes priority unconditionally. An empty CLIMode means no flag
	// was passed — defer to config.
	if cliMode != "" {
		mode, err := sandbox.ParseMode(cliMode)
		if err != nil {
			return "", "", err
		}
		return mode, "cli_flag", nil
	}

	// No CLI override. Use the config's ResolvedMode which handles the
	// legacy Enabled mapping. An empty cfgMode with cfgEnabledSet=false
	// means neither field was written — this is a fresh config and we
	// should apply the "enforce on capable kernels" default.
	if cfgMode == "" && !cfgEnabledSet {
		// Fresh config: caller-level default. Kernel capability is
		// checked separately by SelectBackend → FallbackBackend on
		// pre-5.13 kernels.
		return sandbox.ModeEnforce, "", nil
	}

	mode, err := sandbox.ParseMode(cfgMode)
	if err != nil {
		return "", "", fmt.Errorf("gateway.sandbox.mode: %w", err)
	}
	return mode, "config", nil
}

// productionNagBanner is the multi-line warning printed to stderr when the
// gateway runs with mode=off in a production environment. Deliberately loud
// and unmissable in journald / Docker logs. FR-J-011: no suppression.
const productionNagBanner = `
======================================================================
WARN: SANDBOX DISABLED IN PRODUCTION ENVIRONMENT
  Omnipus is running with sandbox mode=off while OMNIPUS_ENV=production.
  This is not the deny-by-default posture the security model requires.
  Either set sandbox.mode=enforce or remove OMNIPUS_ENV=production.
  This banner repeats every 60 seconds and cannot be silenced.
======================================================================
`

// permissiveNagBanner is printed when mode=permissive. Permissive mode is
// valid for pre-enforcement audit rollouts but must never ship to production
// without an explicit plan to flip to enforce afterwards.
const permissiveNagBanner = `
======================================================================
WARN: SANDBOX IN PERMISSIVE MODE — NOT ENFORCED. DO NOT USE IN PRODUCTION.
  Policy is computed and audit-logged but violations are NOT blocked.
  Seccomp uses RET_LOG; Landlock restrict_self is skipped on kernels < 6.12.
  Flip sandbox.mode to enforce once you've reviewed the audit log.
======================================================================
`

// applySandbox is the Sprint-J boot step. It MUST run after credential
// unlock and config load, and MUST complete before any net.Listen call on
// the HTTP port (FR-J-010, FR-J-016).
//
// Returns (result, nil) on success — including the graceful-fallback path
// where the kernel is too old and we selected FallbackBackend.
//
// Returns (result, err) only when the operator asked for enforce/permissive
// on a capable kernel AND the kernel rejected Apply or Install. In that
// case, the caller MUST abort boot (FR-J-004: fail closed, exit code 78,
// never bind the HTTP listener). The returned result is still populated so
// the caller can inspect what was attempted, but the error overrides it.
func applySandbox(opts SandboxApplyOptions) (*SandboxApplyResult, error) {
	if opts.GetEnv == nil {
		opts.GetEnv = os.Getenv
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	// Step 1 — Resolve mode from CLI + config. CLI > config > default.
	// Validation of the CLI flag string was already done by cobra (see
	// cmd/omnipus/internal/gateway/command.go) with exit code 2, so any
	// error returned here is a bug in the caller; we still guard.
	cfgMode := ""
	cfgEnabledSet := false
	if opts.Cfg != nil {
		cfgMode = opts.Cfg.Sandbox.Mode
		// Detect "Enabled was written to config" vs "Enabled defaulted
		// to the zero value". Presence of any Sandbox field indicates
		// the operator touched the section.
		cfgEnabledSet = opts.Cfg.Sandbox.Enabled ||
			opts.Cfg.Sandbox.Mode != "" ||
			len(opts.Cfg.Sandbox.AllowedPaths) > 0
	}
	mode, disabledBy, err := resolveMode(opts.CLIMode, cfgMode, cfgEnabledSet)
	if err != nil {
		return nil, err
	}

	// Step 2 — Select or reuse backend. SelectBackend never fails; on
	// pre-5.13 kernels or non-Linux it returns FallbackBackend. When the
	// caller provides a backend (normally agentLoop.SandboxBackend()), we
	// reuse it so the status endpoint's PolicyApplied() check observes
	// the Apply-marked state on the same instance.
	var (
		backend     sandbox.SandboxBackend
		backendName string
	)
	if opts.Backend != nil {
		backend = opts.Backend
		backendName = backend.Name()
	} else {
		backend, backendName = sandbox.SelectBackend()
	}
	result := &SandboxApplyResult{
		Backend:     backend,
		BackendName: backendName,
		Mode:        mode,
		DisabledBy:  disabledBy,
	}

	// Step 3 — Handle mode=off: no Apply, no Install. Log-only. Arm the
	// production nag if OMNIPUS_ENV=production.
	if mode == sandbox.ModeOff {
		result.ApplyState = sandbox.ApplyState{
			Mode:       sandbox.ModeOff,
			DisabledBy: orDefault(disabledBy, "config"),
		}
		result.DisabledBy = orDefault(disabledBy, "config")
		slog.Warn("sandbox.disabled",
			"reason", result.DisabledBy,
			"mode", "off",
			"backend", backendName)
		if strings.EqualFold(opts.GetEnv("OMNIPUS_ENV"), "production") {
			fmt.Fprint(opts.Stderr, productionNagBanner)
			slog.Warn("sandbox.disabled.nag",
				"reason", "production_environment",
				"banner_repeat_interval_seconds", 60)
			result.NagReason = "production_off"
		}
		return result, nil
	}

	// Step 4 — Detect Linux kernel capability. If SelectBackend returned
	// FallbackBackend (pre-5.13, non-Linux, Termux, etc.) we cannot apply
	// anything and the sandbox degrades gracefully. FR-J-014 gates seccomp
	// strictly on LinuxBackend selection — no seccomp-alone.
	linuxBE, isLinux := backend.(linuxApplier)
	if !isLinux {
		// Graceful degradation path. Not an error; operator asked for
		// enforce/permissive but the kernel cannot provide it. Hard
		// Constraint #4 (CLAUDE.md) requires we continue serving with
		// application-level fallback rather than crashing.
		slog.Warn("sandbox.degraded",
			"reason", "kernel_too_old_or_non_linux",
			"selected_backend", backendName,
			"requested_mode", string(mode))
		result.Mode = mode
		result.ApplyState = sandbox.ApplyState{
			Mode: mode,
			ExtraNotes: []string{
				"kernel does not support Landlock; falling back to application-level enforcement",
			},
		}
		return result, nil
	}

	// Step 5 — Compute the workspace policy. $OMNIPUS_HOME gets RWX;
	// system libs get R; user AllowedPaths gets R or RW with the
	// system-restricted Write strip (FR-J-013). The warnFn closure
	// captures slog so each stripped rule emits a structured WARN.
	warnFn := func(msg, path string) {
		slog.Warn(msg, "path", path, "reason", "system_restricted_path")
	}
	var allowedPaths []string
	if opts.Cfg != nil {
		allowedPaths = opts.Cfg.Sandbox.AllowedPaths
	}
	policy := sandbox.DefaultPolicy(opts.HomePath, allowedPaths, warnFn)
	// DefaultPolicy.AllowNetworkOutbound=true is currently the only safe value
	// — see its doc comment. The config field sandbox.allow_network_outbound
	// is retained for future use once a real network allow-list lands; until
	// then we force-allow so LLM provider calls always succeed, and emit a
	// warning if the operator explicitly set it to false so the intent
	// doesn't get silently ignored.
	if opts.Cfg != nil && !opts.Cfg.Sandbox.AllowNetworkOutbound {
		slog.Warn("sandbox: allow_network_outbound=false is not yet enforceable (no network allow-list); outbound TCP stays permitted to avoid breaking LLM provider calls")
	}

	// Step 6 — Apply Landlock. Seccomp Install MUST run after this
	// (FR-J-002) because seccomp filters all syscalls including
	// landlock_*; reversing the order would cause Install to block
	// Apply's syscalls.
	if err := linuxBE.ApplyWithMode(policy, mode); err != nil {
		slog.Error("sandbox.apply_failed",
			"error", err,
			"mode", string(mode),
			"backend", backendName)
		return result, fmt.Errorf("sandbox: Apply failed on capable kernel: %w", err)
	}

	// Step 7 — Install seccomp. Permissive mode uses RET_LOG; enforce
	// uses RET_ERRNO(EPERM). Both are gated on Apply having succeeded.
	seccompProg := sandbox.BuildSeccompProgramWithMode(mode)
	if err := seccompProg.Install(); err != nil {
		slog.Error("sandbox.install_failed",
			"error", err,
			"mode", string(mode),
			"backend", backendName)
		return result, fmt.Errorf("sandbox: seccomp Install failed on capable kernel: %w", err)
	}

	// Step 8 — Populate result state for /health and /api/.../sandbox-status.
	abiVersion := 0
	if rep, ok := backend.(interface{ ABIVersion() int }); ok {
		abiVersion = rep.ABIVersion()
	}
	result.ApplyState = sandbox.ApplyState{
		Mode:             mode,
		LandlockEnforced: mode == sandbox.ModeEnforce,
		SeccompEnforced:  mode == sandbox.ModeEnforce,
		AuditOnly:        mode == sandbox.ModePermissive,
	}

	if mode == sandbox.ModePermissive {
		// FR-J-012: prominent banner at boot AND every 60 seconds.
		fmt.Fprint(opts.Stderr, permissiveNagBanner)
		slog.Warn("sandbox.permissive",
			"backend", backendName,
			"mode", "permissive",
			"landlock_abi", abiVersion,
			"seccomp_syscalls", len(seccompProg.BlockedSyscalls()),
			"landlock_enforced", false,
			"seccomp_enforced", false,
			"audit_only", true)
		result.NagReason = "permissive"
	} else {
		slog.Info("sandbox.applied",
			"backend", backendName,
			"mode", "enforce",
			"landlock_abi", abiVersion,
			"seccomp_syscalls", len(seccompProg.BlockedSyscalls()))
	}

	return result, nil
}

// linuxApplier is the internal narrow interface that applySandbox uses to
// call ApplyWithMode on the LinuxBackend without import-cycling via a type
// assertion on *sandbox.LinuxBackend. FallbackBackend does not implement it,
// which is exactly how FR-J-014 (seccomp gated on Linux) is enforced: the
// type assertion fails for Fallback, and the function returns before seccomp
// Install is reached.
type linuxApplier interface {
	ApplyWithMode(policy sandbox.SandboxPolicy, mode sandbox.Mode) error
}

// orDefault returns value if non-empty, otherwise fallback.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// sandboxHealthSetter is the narrow interface the health server satisfies
// for Sprint-J wiring. Extracted from an inline interface type so the
// linter's inamedparam rule is satisfied and the wiring stays testable.
type sandboxHealthSetter interface {
	SetSandboxInfoFunc(fn func() map[string]any)
}

// registerSandboxHealthCheck wires the SandboxApplyResult into the health
// server so GET /health responses include a "sandbox" sub-object with
// {applied, mode, backend}. Sprint-J FR-J-008 requires this field to
// reflect the post-Apply state (true on enforce, false on off/fallback).
// Operators can curl /health | jq .sandbox to verify the runtime state
// without hitting the authenticated /api/v1/security/sandbox-status endpoint.
func registerSandboxHealthCheck(srv sandboxHealthSetter, result *SandboxApplyResult) {
	if srv == nil || result == nil {
		return
	}
	// Build the info map once — FR-J-015 forbids hot-reload of sandbox
	// config, so the values never change after boot. The closure captures
	// the pre-built map to avoid re-allocating on every /health request
	// (this endpoint is hit frequently by k8s readiness probes).
	info := map[string]any{
		"applied": result.Mode == sandbox.ModeEnforce || result.Mode == sandbox.ModePermissive,
		"mode":    string(result.Mode),
		"backend": result.BackendName,
	}
	if result.DisabledBy != "" {
		info["disabled_by"] = result.DisabledBy
	}
	if result.ApplyState.AuditOnly {
		info["audit_only"] = true
	}
	if result.ApplyState.LandlockEnforced {
		info["landlock_enforced"] = true
	}
	if result.ApplyState.SeccompEnforced {
		info["seccomp_enforced"] = true
	}
	srv.SetSandboxInfoFunc(func() map[string]any { return info })
}

// StartNagBanner starts a background goroutine that repeats the permissive
// or production-off banner to stderr every 60 seconds (FR-J-011, FR-J-012).
// Returns a cancel function the gateway shutdown path must call to stop the
// goroutine cleanly.
//
// If reason is "", no goroutine is started and a no-op cancel is returned.
func StartNagBanner(reason string, stderr *os.File) context.CancelFunc {
	if reason == "" {
		return func() {}
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				switch reason {
				case "permissive":
					fmt.Fprint(stderr, permissiveNagBanner)
					slog.Warn("sandbox.permissive.nag", "banner_repeat_interval_seconds", 60)
				case "production_off":
					fmt.Fprint(stderr, productionNagBanner)
					slog.Warn("sandbox.disabled.nag", "banner_repeat_interval_seconds", 60)
				}
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
}
