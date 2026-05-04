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

// childNProcCap caps the number of child processes a hardened-exec subtree
// can spawn at the kernel layer (RLIMIT_NPROC, v0.2 #155 item 5). The cap is
// inherited by every fork() the child performs, so a fork-bomb that slips
// past the shell-guard regex (e.g. via `sh fork.sh` indirection) hits the
// kernel limit before saturating the host's process table.
//
// Sizing rationale: 32 is generous enough for a realistic build pipeline
// (npm install commonly spawns 8-16 concurrent worker subprocesses; nx /
// turborepo can spawn slightly more) but tight enough that an exponential
// fork-bomb saturates within microseconds. Operators with a workload
// requiring more concurrent helpers can tune this — it is intentionally
// internal-const, not config, because higher values defeat the protection.
//
// Threat note: RLIMIT_NPROC is per-UID, not per-process tree. If two
// hardened-exec children run concurrently under the same UID, they share
// the same NPROC budget. That's an acceptable property: the limit caps
// the BLAST RADIUS, not throughput. A hostile bomb in one child consumes
// the budget and starves a sibling, but the host stays alive.
const childNProcCap uint64 = 32

// applyPostStartHardening installs RLIMIT_AS and RLIMIT_NPROC via prlimit
// on the child PID. We do this AFTER Start (rather than via
// SysProcAttr.Rlimits) because the SysProcAttr.Rlimits field is not
// available in all Go toolchain versions we target; prlimit is a stable
// Linux 2.6+ syscall.
//
// A small window exists between Start and Prlimit during which the child
// has no caps. In practice this is a few hundred microseconds — before any
// user code in npm/node has executed. The exec.Cmd contract gives us no
// earlier hook (PreExec is unsafe), so this is the best available without
// re-implementing fork+exec.
//
// RLIMIT_NPROC is set unconditionally (v0.2 #155 item 5). RLIMIT_AS is
// gated on a non-zero Limits.MemoryLimitBytes per the existing contract.
func applyPostStartHardening(cmd *exec.Cmd, lim Limits) error {
	if cmd.Process == nil {
		return nil
	}

	// RLIMIT_NPROC — fork-bomb defense. Applied unconditionally so even
	// callers that don't bother to set MemoryLimitBytes still get fork-
	// bomb containment. The cap is per-UID and inherited by every fork()
	// the child performs.
	nprocLim := &unix.Rlimit{Cur: childNProcCap, Max: childNProcCap}
	if err := unix.Prlimit(cmd.Process.Pid, unix.RLIMIT_NPROC, nprocLim, nil); err != nil {
		// EPERM here means the calling process lacks CAP_SYS_RESOURCE to
		// raise (or even SET) the limit. On a non-root gateway that would
		// only fire if the OS-level user nproc soft limit is below 32 —
		// in which case the OS's own limit is already containing the bomb.
		// We log via the returned error and let the caller decide; we do
		// NOT abort the spawn purely on RLIMIT_NPROC failure because the
		// other layers (regex guard + OS user limit) still apply.
		return fmt.Errorf("prlimit RLIMIT_NPROC: %w", err)
	}

	// RLIMIT_AS — memory cap (existing behavior, v0.1).
	if lim.MemoryLimitBytes == 0 {
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
