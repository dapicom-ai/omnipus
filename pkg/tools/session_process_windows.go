//go:build windows

package tools

import (
	"log/slog"
	"os/exec"
	"strconv"
	"time"
)

func killProcessGroup(pid int) error {
	err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	if err != nil {
		slog.Warn("killProcessGroup: taskkill failed", "pid", pid, "error", err)
	}
	return err
}

// gracefulKillProcessGroup on Windows performs an immediate force-kill (no SIGTERM equivalent).
// The gracePeriod parameter is accepted for interface compatibility but not used.
func gracefulKillProcessGroup(pid int, _ time.Duration) {
	if err := killProcessGroup(pid); err != nil {
		slog.Warn("gracefulKillProcessGroup: kill failed", "pid", pid, "error", err)
	}
}
