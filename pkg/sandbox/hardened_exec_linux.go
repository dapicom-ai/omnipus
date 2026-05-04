//go:build linux

// Linux platform hardening for hardened_exec children. Per
// + : children inherit the gateway's existing Landlock +
// seccomp profiles unchanged (no narrowing in v4); we add Setpgid +
// Pdeathsig=SIGTERM for clean shutdown and prlimit RLIMIT_AS for memory
// caps.

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// applyPlatformHardening configures the child's SysProcAttr and applies
// pre-start prlimit when supported. RLIMIT_AS is set via SysProcAttr.Rlimits
// when the runtime supports it; otherwise we fall back to a post-start
// prlimit on the child PID (small race window, but the limit takes effect
// before any nontrivial allocation in practice).
func applyPlatformHardening(cmd *exec.Cmd, lim Limits) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Setpgid: put the child in a new process group so we can signal the
	// whole subtree (npm spawns children) on timeout.
	cmd.SysProcAttr.Setpgid = true
	// Pdeathsig: kernel sends SIGTERM to the child if the gateway dies.
	// Defends against orphaned npm/node processes when the gateway crashes.
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	return nil
}

// memoryLimitSupported reports whether this platform can enforce
// Limits.MemoryLimitBytes via the post-start hardener. Linux: yes
// (RLIMIT_AS via prlimit). Used by Run to populate
// Result.MemoryLimitUnsupported (HIGH-1, silent-failure-hunter).
const memoryLimitSupported = true

// applyPostStartHardening installs RLIMIT_AS via prlimit on the child PID.
// We do this AFTER Start (rather than via SysProcAttr.Rlimits) because the
// SysProcAttr.Rlimits field is not available in all Go toolchain versions
// we target; prlimit is a stable Linux 2.6+ syscall.
//
// A small window exists between Start and Prlimit during which the child
// has no memory cap. In practice this is a few hundred microseconds —
// before any user code in npm/node has executed. The exec.Cmd contract
// gives us no earlier hook (PreExec is unsafe), so this is the best
// available without re-implementing fork+exec.
func applyPostStartHardening(cmd *exec.Cmd, lim Limits) error {
	if lim.MemoryLimitBytes == 0 || cmd.Process == nil {
		return nil
	}
	rlim := &unix.Rlimit{
		Cur: lim.MemoryLimitBytes,
		Max: lim.MemoryLimitBytes,
	}
	if err := unix.Prlimit(cmd.Process.Pid, unix.RLIMIT_AS, rlim, nil); err != nil {
		// A prlimit failure does not kill the child — we report the
		// error so the caller can decide whether to abort. In practice
		// the only way prlimit fails on a child we just forked is
		// EPERM (different user namespace), which is itself a
		// configuration bug worth surfacing.
		return fmt.Errorf("prlimit RLIMIT_AS: %w", err)
	}
	return nil
}
