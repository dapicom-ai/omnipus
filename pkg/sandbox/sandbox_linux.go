//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Landlock syscall numbers.
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

const (
	landlockRulePathBeneath      = 1
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
	landlockAccessFSIoctlDev   uint64 = 1 << 15 // ABI v3
)

type landlockRulesetAttr struct {
	handledAccessFS uint64
}

type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFd      int32
	_             [4]byte
}

// LinuxBackend enforces Landlock + seccomp on Linux.
type LinuxBackend struct {
	abiVersion int
	allRights  uint64
	// policyApplied is set to true once Apply() succeeds on this backend.
	// It distinguishes "capability available" from "capability enforcing"
	// for the sandbox status endpoint. Landlock Apply is one-shot per
	// process so this flag is latching — it never resets to false.
	policyApplied bool
}

// Compile-time assertion that LinuxBackend satisfies the capability
// interfaces used by sandbox.DescribeBackend. These checks catch the case
// where a refactor renames or removes ABIVersion()/PolicyApplied() and the
// status endpoint silently misclassifies the backend as non-kernel.
var (
	_ interface{ ABIVersion() int }  = (*LinuxBackend)(nil)
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
	if lb.abiVersion >= 3 {
		lb.allRights |= landlockAccessFSIoctlDev
	}
}

func (lb *LinuxBackend) Name() string {
	return fmt.Sprintf("landlock-v%d", lb.abiVersion)
}

func (lb *LinuxBackend) Available() bool { return true }

// ABIVersion returns the detected Landlock ABI version (1-3).
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

// Apply applies Landlock restrictions to the current process.
func (lb *LinuxBackend) Apply(policy SandboxPolicy) error {
	// Create ruleset
	attr := landlockRulesetAttr{handledAccessFS: lb.allRights}
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
			slog.Warn("Landlock: failed to add path rule", "path", rule.Path, "error", err)
			ruleErrors = append(ruleErrors, fmt.Errorf("path %q: %w", rule.Path, err))
		}
	}
	if len(ruleErrors) > 0 {
		return fmt.Errorf("landlock: failed to add %d path rule(s): %w", len(ruleErrors), errors.Join(ruleErrors...))
	}

	// Set no_new_privs then restrict self
	if _, _, errno := unix.RawSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno != 0 {
		return fmt.Errorf("landlock: prctl(PR_SET_NO_NEW_PRIVS) failed: %w", errno)
	}

	_, _, errno = unix.Syscall(sysLandlockRestrictSelf, rulesetFd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock: restrict_self failed: %w", errno)
	}

	// Latching flag: once Landlock has been applied to the process it cannot
	// be removed, so this stays true for the rest of the process lifetime.
	// DescribeBackend reads this to distinguish capability from enforcement.
	lb.policyApplied = true

	slog.Info("Landlock sandbox applied", "abi_version", lb.abiVersion, "rules", len(policy.FilesystemRules))
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
