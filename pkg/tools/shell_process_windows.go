//go:build windows

package tools

import (
	"log/slog"
	"os/exec"
	"strconv"
)

func prepareCommandForTermination(cmd *exec.Cmd) {
	// no-op on Windows
}

// terminateProcessTree terminates the process tree rooted at cmd on Windows.
// onKillFailure is called (if non-nil) when taskkill or Process.Kill fails.
// This hook enables callers to emit audit entries for kill failures without
// changing the error-return semantics of this function. See the Unix
// implementation for the full rationale (B1.4-f).
func terminateProcessTree(cmd *exec.Cmd, onKillFailure func(pid int, err error, caller string)) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run(); err != nil {
		slog.Warn("terminateProcessTree: taskkill failed", "pid", pid, "error", err)
		if onKillFailure != nil {
			onKillFailure(pid, err, "taskkill")
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		slog.Warn("terminateProcessTree: Process.Kill failed", "pid", pid, "error", err)
		if onKillFailure != nil {
			onKillFailure(pid, err, "Kill_proc")
		}
	}
	return nil
}
