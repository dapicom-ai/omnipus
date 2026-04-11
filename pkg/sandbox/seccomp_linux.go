//go:build linux

package sandbox

import (
	"fmt"
	"log/slog"
	"sort"
	"unsafe"

	"golang.org/x/sys/unix"
)

// syscallNrByName maps syscall names to Linux amd64 syscall numbers.
// Used to populate BlockedSyscall.Nr from the canonical name list.
var syscallNrByName = map[string]uint32{
	"ptrace":          unix.SYS_PTRACE,
	"mount":           unix.SYS_MOUNT,
	"umount2":         unix.SYS_UMOUNT2,
	"init_module":     unix.SYS_INIT_MODULE,
	"finit_module":    unix.SYS_FINIT_MODULE,
	"create_module":   unix.SYS_CREATE_MODULE,
	"delete_module":   unix.SYS_DELETE_MODULE,
	"reboot":          unix.SYS_REBOOT,
	"swapon":          unix.SYS_SWAPON,
	"swapoff":         unix.SYS_SWAPOFF,
	"pivot_root":      unix.SYS_PIVOT_ROOT,
	"kexec_load":      unix.SYS_KEXEC_LOAD,
	"kexec_file_load": unix.SYS_KEXEC_FILE_LOAD,
	"bpf":             unix.SYS_BPF,
	"perf_event_open": unix.SYS_PERF_EVENT_OPEN,
}

// blockedSyscallNrs derives numeric syscall IDs from the canonical blockedSyscallNames list.
func blockedSyscallNrs() []uint32 {
	nrs := make([]uint32, 0, len(blockedSyscallNames))
	for _, name := range blockedSyscallNames {
		nr, ok := syscallNrByName[name]
		if !ok {
			panic(fmt.Sprintf("BUG: no syscall number for %q", name))
		}
		nrs = append(nrs, nr)
	}
	return nrs
}

// Install loads the seccomp BPF filter into the kernel.
// Sets PR_SET_NO_NEW_PRIVS first, then installs the filter with
// SECCOMP_FILTER_FLAG_TSYNC for child-process inheritance (SEC-03).
// Blocked syscalls return EPERM (not SIGKILL) per spec.
func (sp *SeccompProgram) Install() error {
	nrs := blockedSyscallNrs()
	if len(nrs) == 0 {
		return nil
	}
	sort.Slice(nrs, func(i, j int) bool { return nrs[i] < nrs[j] })

	filter := assembleBPF(nrs)
	if len(filter) == 0 {
		return fmt.Errorf("seccomp: empty BPF program")
	}

	// PR_SET_NO_NEW_PRIVS is required before installing a seccomp filter
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("seccomp: prctl(PR_SET_NO_NEW_PRIVS): %w", err)
	}

	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}

	// Install filter with TSYNC so all threads get the filter (SEC-03)
	_, _, errno := unix.Syscall(
		unix.SYS_SECCOMP,
		uintptr(unix.SECCOMP_SET_MODE_FILTER),
		uintptr(unix.SECCOMP_FILTER_FLAG_TSYNC),
		uintptr(unsafe.Pointer(&prog)),
	)
	if errno != 0 {
		return fmt.Errorf("seccomp: SYS_SECCOMP install failed: %w", errno)
	}

	slog.Info("Seccomp BPF filter installed", "blocked_syscalls", len(nrs), "tsync", sp.useTSync)
	return nil
}

// assembleBPF builds a classic BPF program that:
//  1. Loads the syscall number from seccomp_data.nr (offset 0)
//  2. Compares against each blocked syscall number
//  3. Returns SECCOMP_RET_ERRNO(EPERM) for blocked syscalls
//  4. Returns SECCOMP_RET_ALLOW for everything else
func assembleBPF(blockedNrs []uint32) []unix.SockFilter {
	// seccomp_data layout: offset 0 = nr (uint32), offset 4 = arch (uint32)
	const offsetNr = 0

	// BPF constants from golang.org/x/sys/unix
	const (
		bpfLD  = unix.BPF_LD
		bpfW   = unix.BPF_W
		bpfABS = unix.BPF_ABS
		bpfJMP = unix.BPF_JMP
		bpfJEQ = unix.BPF_JEQ
		bpfK   = unix.BPF_K
		bpfRET = unix.BPF_RET

		seccompRetAllow = unix.SECCOMP_RET_ALLOW
		seccompRetErrno = unix.SECCOMP_RET_ERRNO
	)

	// SECCOMP_RET_ERRNO encodes errno in the low 16 bits
	retEPERM := uint32(seccompRetErrno) | uint32(unix.EPERM&0xFFFF)

	n := len(blockedNrs)
	// Program structure:
	// [0]         LD  [offsetNr]       — load syscall number
	// [1..n]      JEQ nr, goto_deny    — one JEQ per blocked syscall
	// [n+1]       RET ALLOW            — default: allow
	// [n+2]       RET ERRNO(EPERM)     — deny target

	prog := make([]unix.SockFilter, 0, n+3)

	// Instruction 0: Load syscall number
	prog = append(prog, unix.SockFilter{
		Code: uint16(bpfLD | bpfW | bpfABS),
		K:    offsetNr,
	})

	// Instructions 1..n: Jump to deny if syscall matches
	// Jump targets are relative: Jt/Jf are number of instructions to skip
	// deny is at index n+2, allow is at index n+1
	for i, nr := range blockedNrs {
		remaining := n - i // instructions left after this one before allow
		prog = append(prog, unix.SockFilter{
			Code: uint16(bpfJMP | bpfJEQ | bpfK),
			Jt:   uint8(remaining), // jump to deny (skip remaining JEQs + allow)
			Jf:   0,                // fall through to next JEQ
			K:    nr,
		})
	}

	// Instruction n+1: RET ALLOW (default)
	prog = append(prog, unix.SockFilter{
		Code: uint16(bpfRET | bpfK),
		K:    seccompRetAllow,
	})

	// Instruction n+2: RET ERRNO(EPERM) (deny)
	prog = append(prog, unix.SockFilter{
		Code: uint16(bpfRET | bpfK),
		K:    retEPERM,
	})

	return prog
}
