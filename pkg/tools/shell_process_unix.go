//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
	"time"
)

func prepareCommandForTermination(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessTree sends SIGTERM then SIGKILL to the process group of cmd.
// onKillFailure is called (if non-nil) when any Kill attempt returns an error
// other than "process already done". This gives callers (e.g. ExecTool.runSync)
// a hook to emit audit entries for kill failures without changing the
// error-return semantics of this function (it always returns nil).
//
// B1.4-f: onKillFailure enables audit logging of kill failures per CLAUDE.md
// "audit-everything". The hook is called with pid, the failing error, and a
// caller label so the audit entry contains enough context for forensics.
func terminateProcessTree(cmd *exec.Cmd, onKillFailure func(pid int, err error, caller string)) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	// Send SIGTERM to the process group first to allow graceful cleanup.
	// SIGTERM errors are informational: ESRCH means it is already gone (fine).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		if onKillFailure != nil {
			onKillFailure(pid, err, "SIGTERM_pgrp")
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		if onKillFailure != nil {
			onKillFailure(pid, err, "SIGTERM_proc")
		}
	}

	// Wait up to 2 seconds for the process to exit before escalating to SIGKILL.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Poll every 100ms to avoid blocking here — cmd.Wait() is called by the
		// session goroutine, so we only need to check liveness.
		for i := 0; i < 20; i++ {
			if err := syscall.Kill(pid, 0); err != nil {
				// Process no longer exists.
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	<-done

	// Kill the entire process group unconditionally to handle any survivors.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		if onKillFailure != nil {
			onKillFailure(pid, err, "SIGKILL_pgrp")
		}
	}
	// Fallback kill on the shell process itself via the safe OS handle.
	if err := cmd.Process.Kill(); err != nil {
		// os.ErrProcessDone / ESRCH means the process already exited; not a fault.
		if err != syscall.ESRCH && err.Error() != "os: process already finished" {
			if onKillFailure != nil {
				onKillFailure(pid, err, "Kill_proc")
			}
		}
	}
	return nil
}
