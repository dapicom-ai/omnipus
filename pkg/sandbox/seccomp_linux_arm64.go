//go:build linux && arm64

package sandbox

import "golang.org/x/sys/unix"

func init() {
	// kexec_file_load is available on arm64; create_module is not.
	syscallNrByName["kexec_file_load"] = unix.SYS_KEXEC_FILE_LOAD
}
