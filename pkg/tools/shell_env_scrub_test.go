//go:build !cgo

// T2.5: RunSync_SandboxOff_ScrubsSensitiveEnv.
//
// Verifies that OMNIPUS_MASTER_KEY, OMNIPUS_BEARER_TOKEN, and OMNIPUS_KEY_FILE
// are stripped from the child process environment on the sandbox-off path
// (cmd.Env = sandbox.ScrubGatewayEnv()), AND that PATH is present in the
// child's environment (sanity differentiation — scrubbing must not zero the env).

package tools

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/sandbox"
)

// TestRunSync_SandboxOff_ScrubsSensitiveEnv (T2.5) runs `env` via the
// sandbox-off ExecTool path, parses stdout, and asserts:
//   - OMNIPUS_MASTER_KEY is absent
//   - OMNIPUS_KEY_FILE is absent
//   - OMNIPUS_BEARER_TOKEN is absent
//   - PATH is present (proving the env is not empty after scrubbing)
func TestRunSync_SandboxOff_ScrubsSensitiveEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell / env command")
	}

	const (
		fakeKey    = "FAKE_MASTER_KEY_SCRUB_TEST_99999"
		fakeFile   = "/tmp/should-not-leak.key"
		fakeBearer = "fake-bearer-scrub-test-token"
	)

	// Inject secrets into the test process environment.
	// t.Setenv restores the original (or unsets) at cleanup.
	t.Setenv("OMNIPUS_MASTER_KEY", fakeKey)
	t.Setenv("OMNIPUS_KEY_FILE", fakeFile)
	t.Setenv("OMNIPUS_BEARER_TOKEN", fakeBearer)

	// Build a sandbox-off ExecTool (SandboxMode="" defaults to off per
	// TestExecTool_EmptyMode_DefaultsToOff contract).
	workspace := t.TempDir()
	tool, err := NewExecToolWithDeps(workspace, false, nil, ExecToolDeps{
		SandboxMode:        string(sandbox.ModeOff),
		ExecTimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("NewExecToolWithDeps: %v", err)
	}
	if tool.sandboxOn() {
		t.Fatalf("sandboxOn() = true for ModeOff; want false — check ExecTool wiring")
	}

	ctx, cancel := context.WithTimeout(WithToolContext(context.Background(), "cli", ""), 15*time.Second)
	defer cancel()

	// Run `env` to capture the full child environment.
	res := tool.Execute(ctx, map[string]any{
		"action":  "run",
		"command": "env",
	})
	if res.IsError {
		t.Fatalf("Execute(env) failed: %s", res.ForLLM)
	}

	output := res.ForLLM

	// --- Sensitive keys must be absent ---
	if strings.Contains(output, "OMNIPUS_MASTER_KEY") {
		t.Error("OMNIPUS_MASTER_KEY was present in child env (sandbox-off scrub failure)")
	}
	if strings.Contains(output, fakeKey) {
		t.Errorf("OMNIPUS_MASTER_KEY value %q leaked to child env", fakeKey)
	}
	if strings.Contains(output, "OMNIPUS_KEY_FILE") {
		t.Error("OMNIPUS_KEY_FILE was present in child env (sandbox-off scrub failure)")
	}
	if strings.Contains(output, "OMNIPUS_BEARER_TOKEN") {
		t.Error("OMNIPUS_BEARER_TOKEN was present in child env (sandbox-off scrub failure)")
	}
	if strings.Contains(output, fakeBearer) {
		t.Errorf("OMNIPUS_BEARER_TOKEN value %q leaked to child env", fakeBearer)
	}

	// --- PATH must be present (sanity: env is not empty after scrub) ---
	if !strings.Contains(output, "PATH=") {
		t.Error("PATH is absent from child env — scrub is over-removing (sanity check failed)")
	}
}
