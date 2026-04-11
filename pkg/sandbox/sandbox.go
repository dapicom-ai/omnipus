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
	"os"
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

func (f *FallbackBackend) Name() string    { return "fallback" }
func (f *FallbackBackend) Available() bool { return true }

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

// ApplyToCmd injects sandbox policy as environment variables on the child
// process. This is the application-level counterpart to Landlock/seccomp: when
// kernel sandboxing is unavailable (non-Linux platforms, Linux < 5.13, Termux,
// etc.), cooperative child processes (such as Omnipus helper scripts) can read
// OMNIPUS_SANDBOX_PATHS and self-enforce path restrictions.
//
// Threat model: this mechanism ONLY covers cooperative processes. Arbitrary
// uncooperative binaries are NOT contained — that requires kernel enforcement
// (SEC-01 Landlock). This fulfills ADR W-1 by replacing the previous no-op
// with a real mechanism, while documenting the gap that Landlock closes on
// supported kernels.
//
// The filesystem rules are resolved to absolute paths before being passed to
// the child so that relative workspace paths remain meaningful across cwd
// changes inside the child process.
func (f *FallbackBackend) ApplyToCmd(cmd *exec.Cmd, pol SandboxPolicy) error {
	if cmd == nil {
		return fmt.Errorf("sandbox fallback: nil cmd")
	}
	if len(pol.FilesystemRules) == 0 {
		return nil
	}
	paths := make([]string, 0, len(pol.FilesystemRules))
	for _, rule := range pol.FilesystemRules {
		abs, err := filepath.Abs(rule.Path)
		if err != nil {
			return fmt.Errorf("sandbox fallback: cannot resolve path %q: %w", rule.Path, err)
		}
		paths = append(paths, abs)
	}
	// Inherit existing env if the caller has not populated it yet. This matches
	// the stdlib convention where a nil Env means "use os.Environ()".
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		"OMNIPUS_SANDBOX_MODE=fallback",
		"OMNIPUS_SANDBOX_PATHS="+strings.Join(paths, string(os.PathListSeparator)),
	)
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

// Status describes the active sandbox configuration for operator reporting.
//
// Important distinction between "capability" and "enforcement":
//   - Capable means the backend CAN provide kernel-level enforcement if
//     Apply() is called and seccomp is installed.
//   - Enforcing means Apply() has actually run successfully on the current
//     process and seccomp has been installed.
//
// The fields split the state so the UI cannot mistake capability for runtime
// enforcement. This matters: an earlier version of this type conflated the
// two and would report "Seccomp enabled" whenever Landlock was capable, even
// though neither had ever been installed on the Omnipus process.
type Status struct {
	Backend          string   `json:"backend"`
	Available        bool     `json:"available"`
	KernelLevel      bool     `json:"kernel_level"`
	ABIVersion       int      `json:"abi_version,omitempty"`
	BlockedSyscalls  []string `json:"blocked_syscalls,omitempty"`
	SeccompEnabled   bool     `json:"seccomp_enabled"`
	LandlockFeatures []string `json:"landlock_features,omitempty"`
	// PolicyApplied reports whether Apply() has successfully run on the
	// current process. When false, the Landlock/seccomp capability is
	// available but not actively enforcing — see the package comment for
	// the wiring status.
	PolicyApplied bool `json:"policy_applied"`
	// Notes carries operator-facing explanations of the current state, such
	// as "sandbox not applied at startup" or "kernel older than 5.13". It
	// is empty when everything is healthy.
	Notes []string `json:"notes,omitempty"`
}

// abiReporter is implemented by backends that expose a Landlock ABI version
// (i.e. LinuxBackend). Declared at package scope so DescribeBackend and tests
// can share the interface.
type abiReporter interface {
	ABIVersion() int
}

// policyApplyReporter is implemented by backends that track whether their
// Apply() method has been called successfully. Used by DescribeBackend to
// distinguish capability from runtime enforcement.
type policyApplyReporter interface {
	PolicyApplied() bool
}

// DescribeBackend returns the operator-facing status of the given backend.
// It uses type assertions against narrow interfaces (abiReporter,
// policyApplyReporter) so this function stays build-tag free and forward-
// compatible with future kernel backends (Windows Job Objects, BSD pledge).
//
// The returned Status distinguishes capability from enforcement:
//   - KernelLevel=true means the backend CAN apply kernel policy.
//   - PolicyApplied=true means Apply() has actually run on this process.
//
// When the backend is capable but Apply has not been called, PolicyApplied
// is false and a note is added to Notes to surface the gap to operators.
func DescribeBackend(backend SandboxBackend) Status {
	if backend == nil {
		return Status{Backend: "none", Available: false}
	}
	status := Status{
		Backend:   backend.Name(),
		Available: backend.Available(),
	}
	rep, ok := backend.(abiReporter)
	if !ok {
		// Non-kernel backend (e.g. FallbackBackend). KernelLevel stays false.
		return status
	}
	// Capable of kernel-level enforcement.
	status.KernelLevel = true
	status.ABIVersion = rep.ABIVersion()
	status.LandlockFeatures = DetectLandlockABI(status.ABIVersion).Features
	status.BlockedSyscalls = append([]string(nil), blockedSyscallNames...)

	// Distinguish capability from enforcement. A backend that tracks its
	// own applied state wins; otherwise conservatively assume not applied
	// and surface a note so operators can investigate.
	if applied, ok := backend.(policyApplyReporter); ok && applied.PolicyApplied() {
		status.PolicyApplied = true
		status.SeccompEnabled = true
	} else {
		status.PolicyApplied = false
		status.SeccompEnabled = false
		status.Notes = append(
			status.Notes,
			"sandbox backend is capable of kernel-level enforcement but Apply() has not been called on the Omnipus process; child processes are not currently restricted by Landlock or seccomp",
		)
	}
	return status
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
