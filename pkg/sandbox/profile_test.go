package sandbox_test

import (
	"strings"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// containsPath is a helper to check that a path string contains the given
// substring (case-sensitive, OS path separator agnostic).
func containsPath(path, substr string) bool {
	return strings.Contains(path, substr)
}

// TestLimitsForProfile verifies that each SandboxProfile constant produces a
// Limits value with the expected field shape.
func TestLimitsForProfile(t *testing.T) {
	t.Parallel()

	// We cannot start a real EgressProxy in a unit test without a network
	// listener, so we use a nil proxy and verify the nil-safety of the
	// function. For workspace+net and host profiles we verify EgressProxyAddr
	// is the proxy's Addr() when a real proxy is provided.
	const timeout = int32(30)

	t.Run("empty string treated as workspace", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile("", dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.WorkspaceDir == "" {
			t.Errorf("expected WorkspaceDir to be set for empty profile")
		}
		if lim.EgressProxyAddr != "" {
			t.Errorf("expected no egress proxy for empty (workspace) profile, got %q", lim.EgressProxyAddr)
		}
		if lim.TimeoutSeconds != timeout {
			t.Errorf("expected TimeoutSeconds=%d, got %d", timeout, lim.TimeoutSeconds)
		}
	})

	t.Run("empty string treated as workspace", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile("", dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.WorkspaceDir == "" {
			t.Errorf("expected WorkspaceDir to be set for empty profile (defaults to workspace)")
		}
		if lim.EgressProxyAddr != "" {
			t.Errorf("expected no egress proxy for empty profile, got %q", lim.EgressProxyAddr)
		}
	})

	t.Run("workspace profile: workspace set, no egress proxy", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileWorkspace, dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.WorkspaceDir == "" {
			t.Errorf("expected WorkspaceDir to be set")
		}
		if lim.EgressProxyAddr != "" {
			t.Errorf("expected empty EgressProxyAddr for workspace profile, got %q", lim.EgressProxyAddr)
		}
	})

	t.Run("workspace+net profile: workspace set, nil proxy yields empty addr", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileWorkspaceNet, dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.WorkspaceDir == "" {
			t.Errorf("expected WorkspaceDir to be set for workspace+net profile")
		}
		// nil proxy → empty addr; the actual proxy addr is tested in the
		// integration path when a live proxy is available.
		if lim.EgressProxyAddr != "" {
			t.Errorf("expected empty EgressProxyAddr when proxy is nil, got %q", lim.EgressProxyAddr)
		}
	})

	t.Run("workspace+net profile: proxy addr injected when proxy non-nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		proxy, err := sandbox.NewEgressProxy([]string{"registry.npmjs.org"}, nil)
		if err != nil {
			t.Fatalf("NewEgressProxy: %v", err)
		}
		defer proxy.Close()

		lim, err := sandbox.LimitsForProfile(config.SandboxProfileWorkspaceNet, dir, proxy, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.EgressProxyAddr == "" {
			t.Errorf("expected non-empty EgressProxyAddr when proxy is non-nil")
		}
		if lim.EgressProxyAddr != proxy.Addr() {
			t.Errorf("EgressProxyAddr = %q; want %q", lim.EgressProxyAddr, proxy.Addr())
		}
		if lim.WorkspaceDir == "" {
			t.Errorf("expected WorkspaceDir to be set")
		}
	})

	t.Run("host profile: proxy addr injected when proxy non-nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		proxy, err := sandbox.NewEgressProxy([]string{"registry.npmjs.org", "github.com"}, nil)
		if err != nil {
			t.Fatalf("NewEgressProxy: %v", err)
		}
		defer proxy.Close()

		lim, err := sandbox.LimitsForProfile(config.SandboxProfileHost, dir, proxy, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.EgressProxyAddr != proxy.Addr() {
			t.Errorf("EgressProxyAddr = %q; want %q", lim.EgressProxyAddr, proxy.Addr())
		}
	})

	t.Run("host profile: nil proxy yields empty addr", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileHost, dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lim.EgressProxyAddr != "" {
			t.Errorf("expected empty EgressProxyAddr for nil proxy, got %q", lim.EgressProxyAddr)
		}
	})

	// Test-6: host profile edge cases — empty and non-existent workspaceDir.
	// The host profile uses the workspace dir for npm cache injection only; it
	// does not require the dir to exist. Both cases must succeed without error.
	// Traces to: quizzical-marinating-frog.md pr-test-analyzer Test-6.
	t.Run("host/empty_workspace", func(t *testing.T) {
		t.Parallel()
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileHost, "", nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error for host profile with empty workspaceDir: %v", err)
		}
		// For host profile with empty workspaceDir, WorkspaceDir should be "".
		if lim.WorkspaceDir != "" {
			t.Errorf("expected WorkspaceDir=\"\" for host profile with empty input, got %q", lim.WorkspaceDir)
		}
	})

	t.Run("host/nonexistent_workspace", func(t *testing.T) {
		t.Parallel()
		nonExistent := "/tmp/does-not-exist-xyz-omnipus-test-12345"
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileHost, nonExistent, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error for host profile with non-existent workspaceDir: %v", err)
		}
		// For host profile, the non-existent path is accepted as-is (abs-resolved).
		if lim.WorkspaceDir == "" {
			t.Errorf("expected non-empty WorkspaceDir for non-existent path, got empty string")
		}
		// Must contain the original path component (abs-path resolution).
		if !containsPath(lim.WorkspaceDir, "does-not-exist-xyz-omnipus-test") {
			t.Errorf("WorkspaceDir %q does not appear to be the abs-pathed input", lim.WorkspaceDir)
		}
	})

	t.Run("off profile: zero Limits returned", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lim, err := sandbox.LimitsForProfile(config.SandboxProfileOff, dir, nil, timeout)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		zero := sandbox.Limits{}
		if lim != zero {
			t.Errorf("expected zero Limits for 'off' profile, got %+v", lim)
		}
	})

	t.Run("off profile: IsGodMode returns true", func(t *testing.T) {
		t.Parallel()
		if !sandbox.IsGodMode(config.SandboxProfileOff) {
			t.Errorf("IsGodMode(off) should return true")
		}
	})

	t.Run("non-off profiles: IsGodMode returns false", func(t *testing.T) {
		t.Parallel()
		profiles := []config.SandboxProfile{
			config.SandboxProfileWorkspace,
			config.SandboxProfileWorkspaceNet,
			config.SandboxProfileHost,
			"",
		}
		for _, p := range profiles {
			if sandbox.IsGodMode(p) {
				t.Errorf("IsGodMode(%q) should return false", p)
			}
		}
	})

	t.Run("workspace profile: empty workspaceDir returns error", func(t *testing.T) {
		t.Parallel()
		_, err := sandbox.LimitsForProfile(config.SandboxProfileWorkspace, "", nil, timeout)
		if err == nil {
			t.Errorf("expected error for empty workspaceDir")
		}
	})

	t.Run("unknown profile: returns error", func(t *testing.T) {
		t.Parallel()
		_, err := sandbox.LimitsForProfile("bogus-profile", t.TempDir(), nil, timeout)
		if err == nil {
			t.Errorf("expected error for unknown profile")
		}
	})
}
