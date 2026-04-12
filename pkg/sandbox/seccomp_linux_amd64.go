//go:build linux && amd64

package sandbox

import "golang.org/x/sys/unix"

func init() {
	// Syscall constants available on amd64 but not on all architectures.
	syscallNrByName["create_module"] = unix.SYS_CREATE_MODULE
	syscallNrByName["kexec_file_load"] = unix.SYS_KEXEC_FILE_LOAD
}
