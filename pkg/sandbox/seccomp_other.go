//go:build !linux

package sandbox

import "log/slog"

// Install is a no-op on non-Linux platforms.
// Seccomp is a Linux-only kernel feature (SEC-02).
func (sp *SeccompProgram) Install() error {
	slog.Info("Seccomp not available on this platform. Skipping syscall filtering.")
	return nil
}
