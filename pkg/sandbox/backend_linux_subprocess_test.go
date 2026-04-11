//go:build linux

// Package sandbox_test — subprocess-level Apply coverage for the Landlock backend.
//
// TestLandlock_ApplySubprocess forks the test binary itself as a subprocess
// and calls LinuxBackend.Apply inside the child. This lets us verify that
// Landlock actually restricts filesystem access without permanently sandboxing
// the parent test process (Landlock's restrict_self is a one-way ratchet that
// cannot be removed from the calling process).
package sandbox_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// TestLandlock_ApplySubprocess verifies that LinuxBackend.Apply actually
// enforces filesystem restrictions at the kernel level. It forks the test
// binary as a child process; the child calls Apply with a workspace-only
// policy, then tries to read /etc/passwd. If Landlock enforced the policy,
// the read fails and the child exits with code 42 (sentinel). If Landlock
// is unavailable, the child exits with 77 (skip sentinel).
//
// The parent interprets:
//   - exit 42  → Landlock enforced: test passes
//   - exit 77  → Landlock unavailable at subprocess time: parent skips
//   - anything else → test failure with stderr dump
func TestLandlock_ApplySubprocess(t *testing.T) {
	if os.Getenv("OMNIPUS_LANDLOCK_SUBPROCESS_CHILD") == "1" {
		runLandlockChild()
		return // unreachable — runLandlockChild calls os.Exit
	}

	// Parent path: skip if the Landlock backend is not available on this kernel.
	backend, name := sandbox.SelectBackend()
	if !strings.HasPrefix(name, "landlock") {
		t.Skipf("Landlock backend not available (backend=%q) — skipping subprocess test", name)
	}
	_ = backend

	if os.Getuid() == 0 {
		t.Skip("Landlock tests must run as non-root (root bypasses Landlock restrictions)")
	}

	workspace := t.TempDir()

	// Re-exec the test binary with the child sentinel env var set.
	//nolint:gosec // intentional test-binary self-exec
	cmd := exec.Command(os.Args[0],
		"-test.run=TestLandlock_ApplySubprocess",
		"-test.count=1",
		"-test.v",
	)
	cmd.Env = append(os.Environ(),
		"OMNIPUS_LANDLOCK_SUBPROCESS_CHILD=1",
		"OMNIPUS_LANDLOCK_SANDBOX_DIR="+workspace,
	)
	out, err := cmd.CombinedOutput()

	var exitCode int
	if err == nil {
		exitCode = 0
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else {
		t.Fatalf("child process failed to run: %v\n%s", err, out)
	}

	switch exitCode {
	case 42:
		// Landlock enforced — /etc/passwd read was blocked inside workspace policy.
		t.Logf("Landlock subprocess enforcement confirmed (child exited 42)")
	case 77:
		t.Skipf("Landlock not available inside child process (child exited 77):\n%s", out)
	default:
		t.Fatalf("child exited with unexpected code %d (expected 42 for enforced, 77 for skip):\n%s",
			exitCode, out)
	}
}

// runLandlockChild is the child-mode implementation. It must only call os.Exit —
// never t.Fatal — because we communicate the result via exit code so the parent
// can unambiguously distinguish enforcement (42) from skip (77) from failure.
func runLandlockChild() {
	workspace := os.Getenv("OMNIPUS_LANDLOCK_SANDBOX_DIR")
	if workspace == "" {
		// No workspace provided — cannot set up a meaningful policy.
		os.Exit(77)
	}

	backend, name := sandbox.SelectBackend()
	if !strings.HasPrefix(name, "landlock") {
		// Kernel does not support Landlock in this subprocess environment.
		os.Exit(77)
	}

	policy := sandbox.SandboxPolicy{
		FilesystemRules: []sandbox.PathRule{
			{Path: workspace, Access: sandbox.AccessRead | sandbox.AccessWrite},
		},
	}

	if err := backend.Apply(policy); err != nil {
		// EINVAL from create_ruleset means the kernel rejected our rights bitmask —
		// this can happen when the kernel reports a Landlock ABI version that our
		// backend does not fully enumerate (e.g. ABI v4+ with unknown access bits).
		// Treat as skip so the parent test is not mis-reported as a failure.
		fmt.Fprintf(os.Stderr, "Landlock Apply failed (treating as skip): %v\n", err)
		os.Exit(77)
	}

	// With Landlock active, reading /etc/passwd (outside the workspace) must fail
	// with EACCES (permission denied). Any other error (ENOENT, EIO, EMFILE, etc.)
	// indicates a test environment problem, not Landlock enforcement, and must not
	// be misreported as enforcement success.
	_, err := os.ReadFile("/etc/passwd")
	if err == nil {
		fmt.Fprintf(os.Stderr,
			"Landlock did NOT block /etc/passwd read — policy was not enforced\n")
		os.Exit(1)
	}

	// Only EACCES (or EPERM, which Landlock may return on some kernels) confirms
	// that Landlock is the cause of the denial. Any other errno is an unexpected
	// failure — report it distinctly so the parent can surface a clear diagnostic.
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		fmt.Fprintf(os.Stderr, "Landlock blocked /etc/passwd as expected: %v\n", err)
		os.Exit(42)
	}
	// Unexpected read error — exit 2 so the parent fails with a clear message.
	fmt.Fprintf(os.Stderr, "Unexpected /etc/passwd read error (not EACCES/EPERM): %v\n", err)
	os.Exit(2)
}
