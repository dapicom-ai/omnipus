// T2.6: Run_SandboxOn_ScrubsSensitiveEnv_Subprocess.
//
// Verifies via a real subprocess (sandbox.Run) that OMNIPUS_MASTER_KEY,
// OMNIPUS_BEARER_TOKEN, and OMNIPUS_KEY_FILE are absent from the child's
// inherited environment when the hardened Run path is used, and that PATH
// is present (sanity differentiation).

package sandbox

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRun_SandboxOn_ScrubsSensitiveEnv_Subprocess (T2.6) runs `env` via
// sandbox.Run (the hardened path used when sandbox mode = enforce). It
// injects fake secret values into the test process env, then asserts the
// child's stdout does NOT contain those keys or values.
//
// This is the sandbox-ON counterpart to T2.5 (shell_env_scrub_test.go).
func TestRun_SandboxOn_ScrubsSensitiveEnv_Subprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sandbox.Run subprocess test uses POSIX `env` command")
	}

	const (
		fakeKey    = "FAKE_MASTER_KEY_SANDBOX_ON_99999"
		fakeFile   = "/tmp/should-not-leak-sandbox.key"
		fakeBearer = "fake-bearer-sandbox-on-test"
	)

	// Inject secrets into the current test process env.
	// t.Setenv restores the originals at cleanup.
	t.Setenv("OMNIPUS_MASTER_KEY", fakeKey)
	t.Setenv("OMNIPUS_KEY_FILE", fakeFile)
	t.Setenv("OMNIPUS_BEARER_TOKEN", fakeBearer)

	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// sandbox.Run calls mergeEnv which calls scrubGatewayEnv() — the env
	// scrub is unconditional on this path regardless of whether Landlock is
	// active. The child merely needs to start; we don't require kernel
	// sandbox enforcement for the env-scrub assertion.
	result, err := Run(ctx, []string{"env"}, nil, Limits{
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("sandbox.Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("env exited %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	output := string(result.Stdout)

	// --- Sensitive keys must be absent ---
	if strings.Contains(output, "OMNIPUS_MASTER_KEY") {
		t.Error("OMNIPUS_MASTER_KEY present in hardened child env (T2.6 env-scrub failure)")
	}
	if strings.Contains(output, fakeKey) {
		t.Errorf("OMNIPUS_MASTER_KEY value %q leaked to hardened child", fakeKey)
	}
	if strings.Contains(output, "OMNIPUS_KEY_FILE") {
		t.Error("OMNIPUS_KEY_FILE present in hardened child env (T2.6 env-scrub failure)")
	}
	if strings.Contains(output, "OMNIPUS_BEARER_TOKEN") {
		t.Error("OMNIPUS_BEARER_TOKEN present in hardened child env (T2.6 env-scrub failure)")
	}
	if strings.Contains(output, fakeBearer) {
		t.Errorf("OMNIPUS_BEARER_TOKEN value %q leaked to hardened child", fakeBearer)
	}

	// --- PATH must be present (sanity: env not empty after scrub) ---
	if !strings.Contains(output, "PATH=") {
		t.Error("PATH absent from hardened child env — mergeEnv over-scrubbing (sanity)")
	}
}
