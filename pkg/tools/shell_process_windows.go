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

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run(); err != nil {
		slog.Warn("terminateProcessTree: taskkill failed", "pid", pid, "error", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		slog.Warn("terminateProcessTree: Process.Kill failed", "pid", pid, "error", err)
	}
	return nil
}
