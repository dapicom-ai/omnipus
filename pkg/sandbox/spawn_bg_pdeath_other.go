//go:build !linux

package sandbox

import "os/exec"

// clearPdeathsigForBackground is a no-op on non-Linux platforms because
// PR_SET_PDEATHSIG is a Linux-only feature. Foreground / background
// distinction is handled by the platform's own hardening primitives.
func clearPdeathsigForBackground(cmd *exec.Cmd) {
	_ = cmd
}
