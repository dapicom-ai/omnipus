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

func terminateProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	// Send SIGTERM to the process group first to allow graceful cleanup.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = cmd.Process.Signal(syscall.SIGTERM)

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
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	// Fallback kill on the shell process itself via the safe OS handle.
	_ = cmd.Process.Kill()
	return nil
}
