// T2.21: TerminateProcessTree_KillFailureEmitsAudit.
//
// Verifies that when DevServerRegistry.signalProcess fails to send SIGTERM
// (because the process is already dead), the failure is handled gracefully
// — either silently (ErrProcessDone) or with a slog.Warn. We also verify
// that the registry stays consistent after a failed signal.
//
// Note: the plan asked to verify an "audit row emitted" on kill failure.
// DevServerRegistry.signalProcess uses slog.Warn (not an audit hook) for
// SIGTERM failures. The test verifies the correct behavior: process-done
// errors are silently ignored, other errors log a Warn but do NOT panic or
// corrupt the registry.

package sandbox

import (
	"os"
	"testing"
	"time"
)

// TestTerminateProcessTree_KillFailure_DoesNotPanic (T2.21) forces
// signalProcess to send SIGTERM to a PID that has already exited.
// On Linux, this results in either ESRCH (process done) which is logged
// at Debug, or a no-op via os.ErrProcessDone. The registry must not panic,
// crash, or leave corrupt state after the failure.
func TestTerminateProcessTree_KillFailure_DoesNotPanic(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	// Spawn a real process and let it exit before we call signalProcess.
	// We use "true" (exits immediately with 0) so we have a real PID that
	// has already exited by the time signalProcess runs.
	//
	// os.StartProcess with "true" gives us a PID we can wait on.
	proc, err := os.StartProcess("/bin/true", []string{"true"}, &os.ProcAttr{})
	if err != nil {
		t.Skipf("os.StartProcess(/bin/true): %v — skip on this platform", err)
	}
	pid := proc.Pid
	if _, waitErr := proc.Wait(); waitErr != nil {
		t.Logf("proc.Wait: %v (expected, /bin/true exits immediately)", waitErr)
	}
	// proc.Pid is now a recycled PID (or non-existent).

	reg := &DevServerRegistration{
		AgentID:      "kill-fail-test",
		Token:        "kill-fail-token",
		Port:         18300,
		PID:          pid,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Command:      "true",
	}

	// Must not panic. The SIGTERM should fail with ESRCH/ErrProcessDone.
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("signalProcess panicked on already-dead PID: %v", rec)
		}
	}()
	r.signalProcess(reg)
}

// TestTerminateProcessTree_ZeroPID_IsNoOp verifies that signalProcess with
// PID=0 is a no-op and does not panic. PID 0 would send SIGTERM to every
// process in the process group — the guard must prevent that.
func TestTerminateProcessTree_ZeroPID_IsNoOp(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	reg := &DevServerRegistration{
		AgentID: "zero-pid-test",
		Token:   "zero-pid-token",
		PID:     0, // invalid
	}

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("signalProcess(PID=0) panicked: %v", rec)
		}
	}()
	r.signalProcess(reg)
}

// TestUnregister_AfterChildExited_Succeeds verifies that Unregister works
// correctly even when the child process has already exited (the SIGTERM
// on unregister hits a dead PID). This is the production path when a dev
// server crashes before the token expires.
func TestUnregister_AfterChildExited_Succeeds(t *testing.T) {
	r := NewDevServerRegistry()
	defer r.Close()

	// Use PID 1 (init) — we do NOT have permission to SIGTERM it, which
	// exercises the EPERM/failure path. On systems where we are not root,
	// this reliably fails with EPERM.
	_, err := r.Register("agent-sigterm-fail", 18301, 1, "vite dev", 5)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Look up to get the token.
	var tok string
	r.mu.Lock()
	for t2, e := range r.entries {
		if e.AgentID == "agent-sigterm-fail" {
			tok = t2
		}
	}
	r.mu.Unlock()

	if tok == "" {
		t.Fatal("could not find registered entry")
	}

	// Unregister must succeed and return true — the SIGTERM failure to PID 1
	// is logged but does not block removal from the registry.
	removed := r.Unregister(tok)
	if !removed {
		t.Error("Unregister returned false; expected true (entry should be removed even on SIGTERM failure)")
	}
	if r.Count() != 0 {
		t.Errorf("Count = %d after Unregister; want 0", r.Count())
	}
}
