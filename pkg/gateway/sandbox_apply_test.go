//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Unit tests for Sprint-J sandbox-apply wiring — mode resolution, legacy
// Enabled mapping, CLI > config precedence, and production-off nag banner.
// Kernel-level Apply/Install coverage lives in pkg/sandbox (subprocess
// test) and tests/security (E2E harness).

package gateway

import (
	"os"
	"strings"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// TestResolveMode_CLIBeatsConfig verifies FR-J-006: --sandbox=off with
// config.Sandbox.Mode="enforce" resolves to off. CLI always wins.
func TestResolveMode_CLIBeatsConfig(t *testing.T) {
	mode, src, err := resolveMode("off", "enforce", true)
	if err != nil {
		t.Fatalf("resolveMode: %v", err)
	}
	if mode != sandbox.ModeOff {
		t.Errorf("got mode=%q, want off", mode)
	}
	if src != "cli_flag" {
		t.Errorf("got src=%q, want cli_flag", src)
	}
}

// TestResolveMode_ConfigDefault verifies fallback to config value when
// CLI flag is not set.
func TestResolveMode_ConfigDefault(t *testing.T) {
	mode, src, err := resolveMode("", "off", true)
	if err != nil {
		t.Fatalf("resolveMode: %v", err)
	}
	if mode != sandbox.ModeOff {
		t.Errorf("got mode=%q, want off", mode)
	}
	if src != "config" {
		t.Errorf("got src=%q, want config", src)
	}
}

// TestResolveMode_FreshConfigDefaultsToEnforce verifies that a truly
// empty config (neither Mode nor Enabled ever written) defaults to
// enforce on capable kernels. cfgEnabledSet=false means neither field
// was touched in the file.
func TestResolveMode_FreshConfigDefaultsToEnforce(t *testing.T) {
	mode, src, err := resolveMode("", "", false)
	if err != nil {
		t.Fatalf("resolveMode: %v", err)
	}
	if mode != sandbox.ModeEnforce {
		t.Errorf("got mode=%q, want enforce", mode)
	}
	if src != "" {
		t.Errorf("got src=%q, want empty (default)", src)
	}
}

// TestResolveMode_InvalidCLIReturnsError verifies FR-J-006 second
// sentence: invalid --sandbox values surface as errors so cmd/omnipus
// can exit 2 (usage error) before any boot logic.
func TestResolveMode_InvalidCLIReturnsError(t *testing.T) {
	_, _, err := resolveMode("of", "", false)
	if err == nil {
		t.Fatal("resolveMode: expected error for --sandbox=of")
	}
	if !strings.Contains(err.Error(), "invalid sandbox mode") {
		t.Errorf("error message must identify the bad value; got %q", err.Error())
	}
}

// TestResolveMode_InvalidConfigReturnsError verifies that a malformed
// gateway.sandbox.mode in the config file is also rejected, not
// silently treated as default.
func TestResolveMode_InvalidConfigReturnsError(t *testing.T) {
	_, _, err := resolveMode("", "bogus", true)
	if err == nil {
		t.Fatal("resolveMode: expected error for config Mode=bogus")
	}
	if !strings.Contains(err.Error(), "gateway.sandbox.mode") {
		t.Errorf("error message must include the config path prefix; got %q", err.Error())
	}
}

// TestApplySandbox_OffModeDoesNotCallApply verifies that when mode is
// resolved to off, applySandbox does not invoke Apply/Install — just
// logs the decision and returns a no-enforcement result. This is the
// dev-override path (FR-J-006).
func TestApplySandbox_OffModeDoesNotCallApply(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.Mode = "off"

	tmp, err := os.CreateTemp(t.TempDir(), "stderr-*.log")
	if err != nil {
		t.Fatalf("create temp stderr: %v", err)
	}
	defer tmp.Close()

	result, err := applySandbox(SandboxApplyOptions{
		CLIMode:  "",
		Cfg:      cfg,
		HomePath: t.TempDir(),
		Backend:  sandbox.NewFallbackBackend(),
		GetEnv:   func(string) string { return "" },
		Stderr:   tmp,
	})
	if err != nil {
		t.Fatalf("applySandbox: %v", err)
	}
	if result.Mode != sandbox.ModeOff {
		t.Errorf("got mode=%q, want off", result.Mode)
	}
	if result.ApplyState.LandlockEnforced {
		t.Error("mode=off must not report LandlockEnforced=true")
	}
	if result.ApplyState.SeccompEnforced {
		t.Error("mode=off must not report SeccompEnforced=true")
	}
}

// TestApplySandbox_ProductionOffBanner verifies FR-J-011: mode=off +
// OMNIPUS_ENV=production fires the multi-line warning banner to stderr.
// Uses a temp file in lieu of stderr so the Fprint output can be read
// back after applySandbox returns.
func TestApplySandbox_ProductionOffBanner(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.Mode = "off"

	tmp, err := os.CreateTemp(t.TempDir(), "stderr-*.log")
	if err != nil {
		t.Fatalf("create temp stderr: %v", err)
	}
	defer tmp.Close()

	result, err := applySandbox(SandboxApplyOptions{
		Cfg:      cfg,
		HomePath: t.TempDir(),
		Backend:  sandbox.NewFallbackBackend(),
		GetEnv: func(k string) string {
			if k == "OMNIPUS_ENV" {
				return "production"
			}
			return ""
		},
		Stderr: tmp,
	})
	if err != nil {
		t.Fatalf("applySandbox: %v", err)
	}
	if result.NagReason != "production_off" {
		t.Errorf("NagReason: got %q, want production_off", result.NagReason)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		t.Fatalf("sync stderr temp: %v", syncErr)
	}
	body, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatalf("read stderr temp: %v", err)
	}
	if !strings.Contains(string(body), "SANDBOX DISABLED IN PRODUCTION") {
		t.Errorf("stderr must contain production banner; got %q", string(body))
	}
}

// TestApplySandbox_FallbackBackendNoSeccompInstall verifies FR-J-014:
// when the selected backend is not a LinuxBackend, seccomp Install is
// NOT invoked even with mode=enforce. Seccomp is strictly gated on
// LinuxBackend selection — both-or-neither, never seccomp-alone.
func TestApplySandbox_FallbackBackendNoSeccompInstall(t *testing.T) {
	cfg := &config.Config{}
	cfg.Sandbox.Mode = "enforce"

	result, err := applySandbox(SandboxApplyOptions{
		Cfg:      cfg,
		HomePath: t.TempDir(),
		Backend:  sandbox.NewFallbackBackend(), // explicit fallback injection
		GetEnv:   func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("applySandbox: %v", err)
	}
	// With fallback backend, applySandbox takes the degraded path.
	if result.ApplyState.LandlockEnforced {
		t.Error("FallbackBackend must not report LandlockEnforced=true")
	}
	if result.ApplyState.SeccompEnforced {
		t.Error("FallbackBackend must not report SeccompEnforced=true")
	}
	// The extra notes must mention graceful degradation so operators
	// know why the requested enforce mode didn't actually install.
	found := false
	for _, note := range result.ApplyState.ExtraNotes {
		if strings.Contains(note, "kernel does not support Landlock") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected degradation note in ExtraNotes; got %v", result.ApplyState.ExtraNotes)
	}
}
