// Package sandbox provides kernel-level and application-level process sandboxing.
//
// It implements SEC-01 (Landlock), SEC-02 (seccomp), and SEC-03 (child process
// inheritance) from the Omnipus BRD. Two backends are provided:
//
//   - LinuxBackend: Landlock + seccomp on Linux 5.13+ (sandbox_linux.go)
//   - FallbackBackend: application-level path checks on all platforms
//
// Backend selection follows a capability cascade: detect platform, detect kernel
// features, select the highest-capability backend, log the active enforcement level.
package sandbox

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Access permission flags for PathRule.
const (
	AccessRead    uint64 = 1 << 0
	AccessWrite   uint64 = 1 << 1
	AccessExecute uint64 = 1 << 2
)

// PathRule describes filesystem access for a single path.
type PathRule struct {
	Path   string
	Access uint64
}

// SandboxPolicy describes sandbox restrictions to apply.
type SandboxPolicy struct {
	FilesystemRules   []PathRule
	InheritToChildren bool
}

// SandboxBackend is the interface for platform-specific sandbox enforcement.
type SandboxBackend interface {
	Name() string
	Available() bool
	Apply(policy SandboxPolicy) error
	ApplyToCmd(cmd *exec.Cmd, policy SandboxPolicy) error
}

// ABIResult describes Landlock ABI detection results.
type ABIResult struct {
	Available bool
	Version   int
	Features  []string
}

// DetectLandlockABI returns ABI information based on a probed or mocked ABI version.
// Pass 0 for unavailable, 1-3 for specific ABI versions.
func DetectLandlockABI(abiVersion int) ABIResult {
	if abiVersion <= 0 {
		return ABIResult{Available: false, Version: 0}
	}

	features := []string{
		"EXECUTE", "WRITE_FILE", "READ_FILE", "READ_DIR",
		"REMOVE_DIR", "REMOVE_FILE", "MAKE_CHAR", "MAKE_DIR",
		"MAKE_REG", "MAKE_SOCK", "MAKE_FIFO", "MAKE_BLOCK",
		"MAKE_SYM", "REFER",
	}
	if abiVersion >= 2 {
		features = append(features, "TRUNCATE")
	}
	if abiVersion >= 3 {
		features = append(features, "IOCTL_DEV")
	}

	return ABIResult{
		Available: true,
		Version:   abiVersion,
		Features:  features,
	}
}

// BlockedSyscall pairs a syscall name with its Linux syscall number.
// This is the single source of truth for blocked syscalls — both name-based
// queries and numeric BPF filters derive from this list.
type BlockedSyscall struct {
	Name string
	Nr   uint32 // Linux amd64 syscall number; 0 on non-Linux (populated by seccomp_linux.go)
}

// blockedSyscallNames is the canonical list of blocked syscall names.
// Platform-specific code (seccomp_linux.go) populates Nr values.
var blockedSyscallNames = []string{
	"ptrace",
	"mount",
	"umount2",
	"init_module",
	"finit_module",
	"create_module",
	"delete_module",
	"reboot",
	"swapon",
	"swapoff",
	"pivot_root",
	"kexec_load",
	"kexec_file_load",
	"bpf",
	"perf_event_open",
}

// SeccompProgram represents an assembled seccomp BPF filter program.
type SeccompProgram struct {
	syscalls []BlockedSyscall
	useTSync bool
}

// BuildSeccompProgram assembles the seccomp BPF program with all blocked syscalls.
// The program blocks privilege-escalation syscalls with EPERM (not SIGKILL).
func BuildSeccompProgram() *SeccompProgram {
	syscalls := make([]BlockedSyscall, len(blockedSyscallNames))
	for i, name := range blockedSyscallNames {
		syscalls[i] = BlockedSyscall{Name: name}
	}
	return &SeccompProgram{syscalls: syscalls, useTSync: true}
}

// Blocks returns true if the given syscall name is blocked by this program.
func (sp *SeccompProgram) Blocks(syscall string) bool {
	for _, sc := range sp.syscalls {
		if sc.Name == syscall {
			return true
		}
	}
	return false
}

// BlockedSyscalls returns the list of blocked syscalls.
func (sp *SeccompProgram) BlockedSyscalls() []BlockedSyscall {
	return sp.syscalls
}

// UsesTSync returns true if the program uses SECCOMP_FILTER_FLAG_TSYNC (SEC-03).
func (sp *SeccompProgram) UsesTSync() bool {
	return sp.useTSync
}

// allowedEntry pairs a path with its permitted access flags.
type allowedEntry struct {
	path   string
	access uint64
}

// FallbackBackend provides application-level path checking when kernel
// sandboxing is unavailable.
type FallbackBackend struct {
	entries []allowedEntry
}

// NewFallbackBackend creates a FallbackBackend.
func NewFallbackBackend() *FallbackBackend {
	return &FallbackBackend{}
}

func (f *FallbackBackend) Name() string      { return "fallback" }
func (f *FallbackBackend) Available() bool    { return true }

// Apply records allowed paths and their access flags for application-level enforcement.
func (f *FallbackBackend) Apply(policy SandboxPolicy) error {
	f.entries = nil
	for _, rule := range policy.FilesystemRules {
		abs, err := filepath.Abs(rule.Path)
		if err != nil {
			return fmt.Errorf("sandbox fallback: cannot resolve path %q: %w", rule.Path, err)
		}
		f.entries = append(f.entries, allowedEntry{path: abs, access: rule.Access})
	}
	return nil
}

// ApplyToCmd is a no-op for the fallback backend.
func (f *FallbackBackend) ApplyToCmd(_ *exec.Cmd, _ SandboxPolicy) error {
	return nil
}

// CheckPath verifies that path is under one of the allowed paths (ignoring access flags).
func (f *FallbackBackend) CheckPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sandbox fallback: cannot resolve path %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("sandbox fallback: cannot resolve symlinks for %q: %w", path, err)
	}
	for _, entry := range f.entries {
		if pathIsUnder(resolved, entry.path) {
			return nil
		}
	}
	return fmt.Errorf("sandbox fallback: path %q is outside allowed paths", path)
}

// CheckPathAccess verifies that path is under an allowed path with the requested access flags.
func (f *FallbackBackend) CheckPathAccess(path string, access uint64) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sandbox fallback: cannot resolve path %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("sandbox fallback: cannot resolve symlinks for %q: %w", path, err)
	}
	for _, entry := range f.entries {
		if pathIsUnder(resolved, entry.path) && (entry.access&access) == access {
			return nil
		}
	}
	return fmt.Errorf("sandbox fallback: path %q is outside allowed paths or lacks required access", path)
}

// pathIsUnder checks if child is under or equal to parent directory.
func pathIsUnder(child, parent string) bool {
	parentDir := parent
	if !strings.HasSuffix(parentDir, string(filepath.Separator)) {
		parentDir += string(filepath.Separator)
	}
	return child == parent || strings.HasPrefix(child, parentDir)
}

// SelectBackend detects platform capabilities and returns the highest-capability
// sandbox backend available, along with its name.
func SelectBackend() (SandboxBackend, string) {
	return selectBackendPlatform()
}

// ProbeLandlockABI probes the actual kernel for Landlock ABI version.
// Returns 0 if unavailable. Platform-specific implementation in sandbox_linux.go.
func ProbeLandlockABI() int {
	return probeLandlockABIPlatform()
}
