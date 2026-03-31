//go:build !windows

package tools

import (
	"errors"
	"log/slog"
	"syscall"
	"time"
)

func killProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err != nil {
		// ESRCH means the process group no longer exists — treat as success.
		if errors.Is(err, syscall.ESRCH) {
			// Attempt the individual PID as a fallback in case the process is not a
			// group leader (intentional: some shells don't create a new process group).
			_ = syscall.Kill(pid, syscall.SIGKILL)
			return nil
		}
		// Attempt individual PID fallback regardless of the group-kill error.
		// This covers EPERM on the group signal but success on the direct signal.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return err
	}
	// Group signal succeeded; also signal the individual PID as a fallback for
	// processes that are not group leaders (intentional belt-and-suspenders).
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}

// gracefulKillProcessGroup sends SIGTERM to the process group, waits up to
// gracePeriod, then sends SIGKILL if the process is still running.
func gracefulKillProcessGroup(pid int, gracePeriod time.Duration) {
	// SIGTERM the whole process group.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		slog.Warn("gracefulKillProcessGroup: SIGTERM to process group failed",
			"pid", pid, "error", err)
	}
	// Also signal the individual PID as a fallback for non-group-leader processes.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		slog.Warn("gracefulKillProcessGroup: SIGTERM to pid failed",
			"pid", pid, "error", err)
	}

	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		// Check if the process is still alive by sending signal 0.
		if err := syscall.Kill(pid, 0); err != nil {
			// Process no longer exists.
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Grace period expired: send SIGKILL.
	groupErr := syscall.Kill(-pid, syscall.SIGKILL)
	pidErr := syscall.Kill(pid, syscall.SIGKILL)

	if groupErr != nil && !errors.Is(groupErr, syscall.ESRCH) {
		slog.Warn("gracefulKillProcessGroup: SIGKILL to process group failed",
			"pid", pid, "error", groupErr)
	}
	if pidErr != nil && !errors.Is(pidErr, syscall.ESRCH) {
		slog.Warn("gracefulKillProcessGroup: SIGKILL to pid failed",
			"pid", pid, "error", pidErr)
	}
}
