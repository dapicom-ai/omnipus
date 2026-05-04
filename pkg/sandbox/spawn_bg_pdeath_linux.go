//go:build linux

package sandbox

import "os/exec"

// clearPdeathsigForBackground clears the Linux PR_SET_PDEATHSIG attribute on
// cmd.SysProcAttr so the kernel does not signal the child when the Go OS
// thread that called fork retires. Foreground hardened_exec children DO want
// Pdeathsig (they are short-lived and must not outlive a crashed gateway),
// but background dev servers are managed by the DevServerRegistry which
// signals them explicitly via the process group.
func clearPdeathsigForBackground(cmd *exec.Cmd) {
	if cmd == nil || cmd.SysProcAttr == nil {
		return
	}
	cmd.SysProcAttr.Pdeathsig = 0
}
