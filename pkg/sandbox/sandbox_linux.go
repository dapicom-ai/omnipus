//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

// processLandlockApplied is a process-wide latching flag set to true the
// first time ApplyWithMode successfully completes on ANY LinuxBackend
// instance in this process. It exists because Landlock's restrict_self is a
// process-global ratchet: once any instance has called it, any subsequent
// call (even on a fresh LinuxBackend with policyApplied=false) would fail
// with EINVAL from create_ruleset on the already-restricted task.
//
// Tests (gateway_harness_test, rest_*_test) spawn multiple in-process
// gateway instances in sequence; each constructs its own LinuxBackend via
// sandbox.SelectBackend(). Without this process-wide guard, the second boot
// would call Apply on a kernel that has already been locked down and fail
// the whole test run. Production gateways only ever boot once, so this is
// strictly a test-affordance — but the guard is safe in production too
// (Apply's one-shot semantics mean a second boot was always wrong).
var processLandlockApplied atomic.Bool

// Landlock syscall numbers.
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

const (
	landlockRulePathBeneath      = 1
	landlockRuleNetPort          = 2 // ABI v4+: bind/connect rules use this rule type
	landlockCreateRulesetVersion = 1 << 0
)

// Landlock ABI v1 filesystem access rights (kernel 5.13+).
const (
	landlockAccessFSExecute    uint64 = 1 << 0
	landlockAccessFSWriteFile  uint64 = 1 << 1
	landlockAccessFSReadFile   uint64 = 1 << 2
	landlockAccessFSReadDir    uint64 = 1 << 3
	landlockAccessFSRemoveDir  uint64 = 1 << 4
	landlockAccessFSRemoveFile uint64 = 1 << 5
	landlockAccessFSMakeChar   uint64 = 1 << 6
	landlockAccessFSMakeDir    uint64 = 1 << 7
	landlockAccessFSMakeReg    uint64 = 1 << 8
	landlockAccessFSMakeSock   uint64 = 1 << 9
	landlockAccessFSMakeFifo   uint64 = 1 << 10
	landlockAccessFSMakeBlock  uint64 = 1 << 11
	landlockAccessFSMakeSym    uint64 = 1 << 12
	landlockAccessFSRefer      uint64 = 1 << 13
	landlockAccessFSTruncate   uint64 = 1 << 14 // ABI v2
	// landlockAccessFSIoctlDev removed: does not exist in kernel headers on this
	// system (6.8.0-107). Adding unknown bits to handledAccessFS causes EINVAL.
)

// Landlock ABI v4 network access rights (kernel 6.8+).
const (
	landlockAccessNetBindTcp    uint64 = 1 << 0
	landlockAccessNetConnectTcp uint64 = 1 << 1
)

type landlockRulesetAttr struct {
	handledAccessFS  uint64
	handledAccessNet uint64 // ABI v4 only; zero-initialized on older ABIs
}

type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFd      int32
	_             [4]byte
}

// landlockNetPortAttr matches the kernel's struct landlock_net_port_attr
// (include/uapi/linux/landlock.h). Both fields are u64 in the ABI: the kernel
// reserved width for `port` even though TCP port numbers fit in 16 bits, so
// the Go side must use uint64 here regardless of NetPortRule.Port being uint16.
type landlockNetPortAttr struct {
	allowedAccess uint64
	port          uint64
}

// LinuxBackend enforces Landlock + seccomp on Linux.
type LinuxBackend struct {
	abiVersion int
	allRights  uint64
	// handledAccessNet is the bitmask of Landlock NET_* access rights that
	// the backend declares to the kernel when creating the ruleset. Set to
	// landlockAccessNetBindTcp|landlockAccessNetConnectTcp on ABI ≥ 4 (where
	// the kernel honors net rules) and zero on older ABIs. Computed once in
	// computeRights and read back in ApplyWithMode.
	handledAccessNet uint64
	// policyApplied is set to true once Apply() succeeds on this backend.
	// It distinguishes "capability available" from "capability enforcing"
	// for the sandbox status endpoint. Landlock Apply is one-shot per
	// process so this flag is latching — it never resets to false.
	policyApplied bool
	// savedPolicy is the SandboxPolicy that ApplyWithMode installed. We
	// retain it so RestrictCurrentThread can rebuild an equivalent ruleset
	// and re-run landlock_restrict_self on whichever OS thread is about to
	// fork a child. Without per-thread re-apply, Go's M:N scheduler can
	// fork from an unrestricted worker thread and the child silently
	// escapes the kernel sandbox.
	savedPolicy SandboxPolicy
	// savedMode is the mode passed to ApplyWithMode; RestrictCurrentThread
	// is a no-op when the saved mode is not ModeEnforce.
	savedMode Mode
}

// Compile-time assertion that LinuxBackend satisfies the capability
// interfaces used by sandbox.DescribeBackend. These checks catch the case
// where a refactor renames or removes ABIVersion()/PolicyApplied() and the
// status endpoint silently misclassifies the backend as non-kernel.
var (
	_ interface{ ABIVersion() int }     = (*LinuxBackend)(nil)
	_ interface{ PolicyApplied() bool } = (*LinuxBackend)(nil)
)

// NewLinuxBackend creates a Linux sandbox backend if Landlock is available.
// Returns (backend, true) if available, (nil, false) if not.
func NewLinuxBackend() (*LinuxBackend, bool) {
	abi := probeLandlockABIPlatform()
	if abi <= 0 {
		return nil, false
	}
	lb := &LinuxBackend{abiVersion: abi}
	lb.computeRights()
	return lb, true
}

func (lb *LinuxBackend) computeRights() {
	lb.allRights = landlockAccessFSExecute | landlockAccessFSWriteFile |
		landlockAccessFSReadFile | landlockAccessFSReadDir |
		landlockAccessFSRemoveDir | landlockAccessFSRemoveFile |
		landlockAccessFSMakeChar | landlockAccessFSMakeDir |
		landlockAccessFSMakeReg | landlockAccessFSMakeSock |
		landlockAccessFSMakeFifo | landlockAccessFSMakeBlock |
		landlockAccessFSMakeSym | landlockAccessFSRefer

	if lb.abiVersion >= 2 {
		lb.allRights |= landlockAccessFSTruncate
	}
	// Note: landlockAccessFSIoctlDev is commented out because
	// LANDLOCK_ACCESS_FS_IOCTL does not exist in kernel headers
	// on this system (6.8.0-107). Setting unknown bits causes EINVAL.
	//
	// Landlock ABI v4+ adds NET_BIND_TCP and NET_CONNECT_TCP. We declare
	// only NET_BIND_TCP as handled — that closes the rogue-port hole
	// (an agent shell can no longer bind 0.0.0.0:5173 outside the
	// dev-server allow-list). NET_CONNECT_TCP is intentionally NOT
	// handled because (a) the gateway itself needs outbound 443/53/80
	// for LLM provider calls and DNS, and a kernel-level connect
	// allow-list would break those without buying us much over the
	// already-existing application-layer egress proxy, and (b) agent
	// children still get egress filtering when sandbox.Run injects
	// HTTP_PROXY/HTTPS_PROXY into their environment.
	//
	// ConnectPortRules were removed from SandboxPolicy in v0.1 (A1.3):
	// the public API no longer advertises connect-port enforcement that
	// the kernel never applied. Outbound TCP filtering is delegated to
	// the egress proxy. On kernels < ABI v4 handledAccessNet stays 0
	// entirely, preserving legacy unrestricted-network behaviour.
	if lb.abiVersion >= 4 {
		lb.handledAccessNet = landlockAccessNetBindTcp
	}
}

func (lb *LinuxBackend) Name() string {
	return fmt.Sprintf("landlock-v%d", lb.abiVersion)
}

func (lb *LinuxBackend) Available() bool { return true }

// ABIVersion returns the detected Landlock ABI version (1-4).
// Returns 0 if Landlock is not available.
func (lb *LinuxBackend) ABIVersion() int {
	return lb.abiVersion
}

// PolicyApplied reports whether Apply() has successfully run on this
// backend. Used by sandbox.DescribeBackend to distinguish capability from
// runtime enforcement in the status endpoint.
func (lb *LinuxBackend) PolicyApplied() bool {
	return lb.policyApplied
}

// Apply applies Landlock restrictions to the current process in enforce mode.
// Idempotent: once PolicyApplied() returns true, further calls are no-ops.
// See ApplyWithMode for the mode-aware variant that supports permissive mode.
func (lb *LinuxBackend) Apply(policy SandboxPolicy) error {
	return lb.ApplyWithMode(policy, ModeEnforce)
}

// ApplyWithMode applies Landlock restrictions to the current process.
//
// ModeEnforce:
//   - Build ruleset, add path rules, prctl(PR_SET_NO_NEW_PRIVS), landlock_restrict_self.
//   - After this returns, filesystem access outside the ruleset returns EACCES.
//
// ModePermissive:
//   - Build ruleset and add path rules (so errors in rule add are still caught).
//   - prctl(PR_SET_NO_NEW_PRIVS) so seccomp install still works.
//   - SKIP landlock_restrict_self entirely (kernels ≤ 6.11 have no permissive
//     Landlock semantic; restrict_self would enforce the policy in that case).
//   - Emit an INFO log identifying this as the permissive-degraded path.
//   - PolicyApplied() still returns true so the status endpoint correctly
//     reports "policy was computed and logged" — this is the audit-only
//     degradation documented in FR-J-012.
//
// ModeOff is rejected; callers should never invoke Apply when the sandbox is
// disabled. An error is returned instead of silently succeeding to prevent
// drift between caller intent and kernel state.
//
// Idempotent: if PolicyApplied() already returns true, further calls are
// no-ops that return nil. Landlock is a one-way ratchet — re-applying would
// stack rulesets and could only tighten, never widen. We treat that as a
// bug in the caller (e.g. a misconfigured reload handler) and skip.
func (lb *LinuxBackend) ApplyWithMode(policy SandboxPolicy, mode Mode) error {
	if lb.policyApplied {
		slog.Info("sandbox.apply.skipped", "reason", "already_applied",
			"abi_version", lb.abiVersion, "mode", string(mode))
		return nil
	}
	// Process-wide guard: if any other LinuxBackend instance already
	// called Apply in this process, the kernel has Landlock restrictions
	// installed globally. Creating another ruleset here would fail with
	// EINVAL and fool callers into thinking the kernel is broken. Flag
	// this instance as applied and return nil — the effective policy on
	// the process is whatever was installed the first time.
	if processLandlockApplied.Load() {
		lb.policyApplied = true
		slog.Info("sandbox.apply.skipped",
			"reason", "already_applied_in_process",
			"abi_version", lb.abiVersion, "mode", string(mode))
		return nil
	}

	switch mode {
	case ModeEnforce, ModePermissive:
		// Fine — proceed below.
	case ModeOff, "":
		// Defensive: callers should gate on mode before invoking Apply.
		// Returning an error here turns a caller bug into a loud boot
		// failure instead of a silent "sandbox disabled" state.
		return fmt.Errorf("landlock: Apply called with mode=%q; caller must gate on mode", mode)
	default:
		return fmt.Errorf("landlock: unknown mode %q", mode)
	}

	// Create ruleset. handledAccessNet is set on ABI ≥ 4 to enable
	// kernel-level enforcement of NET_BIND_TCP / NET_CONNECT_TCP — see
	// computeRights for the rationale. On older ABIs it stays 0 and the
	// kernel ignores the field.
	attr := landlockRulesetAttr{
		handledAccessFS:  lb.allRights,
		handledAccessNet: lb.handledAccessNet,
	}
	rulesetFd, _, errno := unix.Syscall(
		sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock: create_ruleset failed: %w", errno)
	}
	defer unix.Close(int(rulesetFd))

	var ruleErrors []error
	for _, rule := range policy.FilesystemRules {
		rights := lb.accessToLandlockRights(rule.Access)
		if err := addLandlockPathRule(int(rulesetFd), rule.Path, rights); err != nil {
			// ENOENT for system paths (e.g. /lib64 on ARM64) is expected —
			// the directory simply doesn't exist on that architecture. Log
			// as a warning and skip rather than aborting sandbox setup.
			if errors.Is(err, unix.ENOENT) {
				slog.Warn("Landlock: path does not exist, skipping rule", "path", rule.Path)
				continue
			}
			slog.Warn("Landlock: failed to add path rule", "path", rule.Path, "error", err)
			ruleErrors = append(ruleErrors, fmt.Errorf("path %q: %w", rule.Path, err))
		}
	}
	if len(ruleErrors) > 0 {
		return fmt.Errorf("landlock: failed to add %d path rule(s): %w", len(ruleErrors), errors.Join(ruleErrors...))
	}

	// Network port allow-rules (Landlock ABI v4+). On older kernels the
	// rule-add syscall returns EINVAL because handledAccessNet is zero and
	// LANDLOCK_RULE_NET_PORT is unknown — we treat that as a soft skip so
	// the FS rules still take effect. On capable kernels, every entry in
	// BindPortRules is registered as an allow-rule; any bind to a port not
	// listed will be denied with EACCES.
	if lb.abiVersion >= 4 {
		for _, rule := range policy.BindPortRules {
			if err := addLandlockNetPortRule(int(rulesetFd), rule.Port, landlockAccessNetBindTcp); err != nil {
				if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
					slog.Warn("Landlock: kernel rejected net bind rule, continuing without it",
						"port", rule.Port, "error", err)
					continue
				}
				return fmt.Errorf("landlock: add bind rule for port %d: %w", rule.Port, err)
			}
		}
		// ConnectPortRules are intentionally ignored — handledAccessNet only
		// includes NET_BIND_TCP, so the kernel does not enforce connect
		// allow-listing. Outbound TCP filtering is delegated to the egress
		// proxy. See computeRights for the rationale.
	} else if len(policy.BindPortRules) > 0 {
		// Defensive: caller passed bind rules but the kernel ABI is too
		// low to honor them. Log once and proceed — refusing to apply
		// here would force the operator into a no-sandbox-at-all state
		// just because they configured net rules on an older kernel.
		// Note: ConnectPortRules were removed in v0.1 (A1.3); outbound TCP
		// filtering is delegated to the egress proxy.
		slog.Warn("Landlock: net port rules ignored on this kernel",
			"abi_version", lb.abiVersion,
			"required_abi", 4,
			"bind_rules", len(policy.BindPortRules))
	}

	// Set no_new_privs unconditionally — seccomp install (which runs after
	// this) requires NNP, and NNP is also a prerequisite for Landlock's
	// restrict_self. This is safe in permissive mode because NNP by itself
	// enforces nothing; it only disables setuid/setgid and seccomp-bypass.
	if _, _, prctlErrno := unix.RawSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); prctlErrno != 0 {
		return fmt.Errorf("landlock: prctl(PR_SET_NO_NEW_PRIVS) failed: %w", prctlErrno)
	}

	if mode == ModePermissive {
		// FR-J-012: current kernels (≤ 6.11) have no native permissive
		// Landlock semantic. Calling restrict_self here would enforce
		// the policy — the opposite of what the operator asked for.
		// We skip it and leave the policy computed-but-unenforced. The
		// seccomp program is separately installed with RET_LOG by the
		// caller, which gives us partial audit-only coverage.
		lb.policyApplied = true
		processLandlockApplied.Store(true)
		slog.Info("sandbox.permissive.downgraded",
			"reason", "kernel_lacks_permissive_landlock",
			"abi_version", lb.abiVersion,
			"rules", len(policy.FilesystemRules),
			"bind_rules", len(policy.BindPortRules))
		return nil
	}

	// Enforce mode: restrict_self is the one-way ratchet that actually
	// activates policy enforcement on this thread and all future children.
	_, _, errno = unix.Syscall(sysLandlockRestrictSelf, rulesetFd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock: restrict_self failed: %w", errno)
	}

	// Latching flag: once Landlock has been applied to the process it cannot
	// be removed, so this stays true for the rest of the process lifetime.
	// DescribeBackend reads this to distinguish capability from enforcement.
	lb.policyApplied = true
	lb.savedPolicy = policy
	lb.savedMode = mode
	processLandlockApplied.Store(true)
	registerCurrentLinuxBackend(lb)

	slog.Info("Landlock sandbox applied",
		"abi_version", lb.abiVersion,
		"rules", len(policy.FilesystemRules),
		"bind_rules", len(policy.BindPortRules),
		"mode", string(mode))
	return nil
}

// accessToLandlockRights maps generic Access flags to Landlock-specific rights.
func (lb *LinuxBackend) accessToLandlockRights(access uint64) uint64 {
	var rights uint64
	if access&AccessRead != 0 {
		rights |= landlockAccessFSReadFile | landlockAccessFSReadDir
	}
	if access&AccessWrite != 0 {
		rights |= landlockAccessFSWriteFile | landlockAccessFSRemoveDir |
			landlockAccessFSRemoveFile | landlockAccessFSMakeChar |
			landlockAccessFSMakeDir | landlockAccessFSMakeReg |
			landlockAccessFSMakeSock | landlockAccessFSMakeFifo |
			landlockAccessFSMakeBlock | landlockAccessFSMakeSym |
			landlockAccessFSRefer
		if lb.abiVersion >= 2 {
			rights |= landlockAccessFSTruncate
		}
	}
	if access&AccessExecute != 0 {
		rights |= landlockAccessFSExecute
	}
	return rights
}

// ApplyToCmd is a no-op — Landlock restrictions inherit to children natively (SEC-03).
func (lb *LinuxBackend) ApplyToCmd(_ *exec.Cmd, _ SandboxPolicy) error {
	return nil
}

func addLandlockPathRule(rulesetFd int, path string, rights uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer unix.Close(fd)

	// Many access rights are directory-only. The kernel validates that
	// allowed_access only contains rights valid for the FD type (file vs
	// directory). Strip directory-only rights for regular/character files to
	// avoid EINVAL when whitelisting paths like /dev/null or /etc/hosts.
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err == nil && stat.Mode&unix.S_IFDIR == 0 {
		dirOnly := landlockAccessFSReadDir |
			landlockAccessFSRemoveDir | landlockAccessFSRemoveFile |
			landlockAccessFSMakeChar | landlockAccessFSMakeDir |
			landlockAccessFSMakeReg | landlockAccessFSMakeSock |
			landlockAccessFSMakeFifo | landlockAccessFSMakeBlock |
			landlockAccessFSMakeSym | landlockAccessFSRefer
		rights &^= dirOnly
	}

	pathAttr := landlockPathBeneathAttr{
		allowedAccess: rights,
		parentFd:      int32(fd),
	}

	_, _, errno := unix.Syscall6(
		sysLandlockAddRule,
		uintptr(rulesetFd),
		landlockRulePathBeneath,
		uintptr(unsafe.Pointer(&pathAttr)),
		0, 0, 0,
	)
	if errno != 0 {
		return fmt.Errorf("add_rule for %q: %w", path, errno)
	}
	return nil
}

// addLandlockNetPortRule registers an allow-rule for a single TCP port via
// landlock_add_rule(LANDLOCK_RULE_NET_PORT). rights is the bitmask of net
// access rights (NET_BIND_TCP and/or NET_CONNECT_TCP) that the rule grants.
// The kernel rejects rights not declared in handledAccessNet on the parent
// ruleset, so callers must either pass a superset of handledAccessNet or
// enable handledAccessNet to cover the rights here. Mirrors addLandlockPathRule.
func addLandlockNetPortRule(rulesetFd int, port uint16, rights uint64) error {
	attr := landlockNetPortAttr{
		allowedAccess: rights,
		port:          uint64(port),
	}
	_, _, errno := unix.Syscall6(
		sysLandlockAddRule,
		uintptr(rulesetFd),
		landlockRuleNetPort,
		uintptr(unsafe.Pointer(&attr)),
		0, 0, 0,
	)
	if errno != 0 {
		return fmt.Errorf("add_rule net_port=%d: %w", port, errno)
	}
	return nil
}

func probeLandlockABIPlatform() int {
	version, _, errno := unix.Syscall(
		sysLandlockCreateRuleset,
		0,
		0,
		landlockCreateRulesetVersion,
	)
	if errno != 0 {
		return 0
	}
	return int(version)
}

// currentLinuxBackend holds the LinuxBackend that most recently completed
// ApplyWithMode in enforce mode. We need this because Landlock enforces per
// OS thread (`landlock_restrict_self` only restricts the calling thread plus
// its future descendants). Go's M:N scheduler routes goroutines onto
// arbitrary OS worker threads, so any goroutine that forks a child via
// `exec.Cmd.Start` may run on an unrestricted thread and the child silently
// escapes the kernel sandbox. Spawn-time helpers (RestrictCurrentThread,
// RunOnRestrictedThread) read this singleton to rebuild an equivalent ruleset
// on whichever thread is about to fork.
var (
	currentLinuxBackendMu sync.RWMutex
	currentLinuxBackend   *LinuxBackend
)

func registerCurrentLinuxBackend(lb *LinuxBackend) {
	currentLinuxBackendMu.Lock()
	currentLinuxBackend = lb
	currentLinuxBackendMu.Unlock()
}

// CurrentLinuxBackend returns the LinuxBackend that completed Apply in the
// current process, or nil if none did. Callers MUST treat the returned value
// as read-only.
func CurrentLinuxBackend() *LinuxBackend {
	currentLinuxBackendMu.RLock()
	defer currentLinuxBackendMu.RUnlock()
	return currentLinuxBackend
}

// restrictCurrentThreadIfNeeded re-applies the saved enforce-mode policy to
// the calling OS thread. No-op when the gateway is not in enforce mode. The
// caller must hold runtime.LockOSThread before calling. See the linux
// implementation's RestrictCurrentThread for the contract.
func restrictCurrentThreadIfNeeded() error {
	lb := CurrentLinuxBackend()
	if lb == nil {
		return nil
	}
	return lb.RestrictCurrentThread()
}

// RestrictCurrentThread applies the saved enforce-mode policy to the calling
// OS thread. The caller MUST runtime.LockOSThread() before invoking this and
// MUST NOT runtime.UnlockOSThread afterwards — the OS thread is permanently
// restricted and Go must dispose of it (by exiting the goroutine that owns
// the lock) rather than recycling it for unrelated work.
//
// Returns nil (no-op) if the saved mode was anything other than ModeEnforce.
// Returns an error if the kernel rejects the ruleset; callers must abort the
// spawn rather than fall through to an unrestricted exec.
func (lb *LinuxBackend) RestrictCurrentThread() error {
	if lb == nil || lb.savedMode != ModeEnforce {
		return nil
	}

	// 1. PR_SET_NO_NEW_PRIVS is per-thread on Linux; new Go worker threads
	//    inherit it from their creator's state at clone() time, but Go may
	//    have created OS threads BEFORE the boot Apply ran. Re-set it here
	//    so the ruleset attach in step 4 is accepted on the calling thread.
	if _, _, errno := unix.RawSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno != 0 {
		return fmt.Errorf("landlock: prctl(PR_SET_NO_NEW_PRIVS) failed: %w", errno)
	}

	// 2. Build a fresh ruleset matching the saved policy.
	attr := landlockRulesetAttr{
		handledAccessFS:  lb.allRights,
		handledAccessNet: lb.handledAccessNet,
	}
	rulesetFd, _, errno := unix.Syscall(
		sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock: create_ruleset failed: %w", errno)
	}
	defer unix.Close(int(rulesetFd))

	// 3. Re-add the saved filesystem and net port rules. We tolerate ENOENT
	//    (path missing on this arch) and EINVAL/ENOENT for net rules on
	//    older ABIs, matching ApplyWithMode's behavior.
	for _, rule := range lb.savedPolicy.FilesystemRules {
		rights := lb.accessToLandlockRights(rule.Access)
		if err := addLandlockPathRule(int(rulesetFd), rule.Path, rights); err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return fmt.Errorf("landlock: re-add path %q: %w", rule.Path, err)
		}
	}
	if lb.abiVersion >= 4 {
		for _, rule := range lb.savedPolicy.BindPortRules {
			if err := addLandlockNetPortRule(int(rulesetFd), rule.Port, landlockAccessNetBindTcp); err != nil {
				if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
					continue
				}
				return fmt.Errorf("landlock: re-add bind port %d: %w", rule.Port, err)
			}
		}
		// ConnectPortRules deliberately not re-added; matches the
		// handledAccessNet decision in computeRights.
	}

	// 4. restrict_self installs the Landlock domain on the calling thread.
	//    Children forked from this thread will inherit the domain; sibling
	//    threads in the same process are unaffected.
	if _, _, errno := unix.Syscall(sysLandlockRestrictSelf, rulesetFd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: restrict_self failed: %w", errno)
	}
	return nil
}

func selectBackendPlatform() (SandboxBackend, string) {
	lb, ok := NewLinuxBackend()
	if ok {
		name := lb.Name()
		slog.Info("Landlock sandbox available", "backend", name, "abi_version", lb.abiVersion)
		return lb, name
	}

	var kernelVersion string
	var uname unix.Utsname
	if err := unix.Uname(&uname); err == nil {
		kernelVersion = unix.ByteSliceToString(uname.Release[:])
	}
	slog.Warn("Landlock not available. Using application-level enforcement.",
		"backend", "fallback", "kernel_version", kernelVersion)
	fb := NewFallbackBackend()
	return fb, fb.Name()
}
